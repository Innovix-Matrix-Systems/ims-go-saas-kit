package dev

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database/dbtest"
)

func TestCommandService(t *testing.T) {
	queries, pool := dbtest.Queries(t)
	dbtest.Clean(t, pool, "dev_actions")
	ctx := context.Background()

	release := make(chan struct{})
	service := NewCommandService(queries, nil,
		Command{Name: "greet", Label: "Greet", Group: "Test",
			Options: []CommandOption{
				{Name: "name", Type: "text", Default: "world"},
				{Name: "loud", Type: "bool", Default: "false"},
				{Name: "times", Type: "number", Default: "1"},
				{Name: "mood", Type: "select", Default: "happy", Choices: []string{"happy", "sad"}},
			},
			Run: func(_ context.Context, options map[string]string, out io.Writer) error {
				fmt.Fprintf(out, "hello %s (%s, %s)", options["name"], options["loud"], options["mood"])
				return nil
			}},
		Command{Name: "fail", Run: func(context.Context, map[string]string, io.Writer) error {
			return errors.New("boom")
		}},
		Command{Name: "panic", Run: func(context.Context, map[string]string, io.Writer) error {
			panic("kaput")
		}},
		Command{Name: "slow", Background: true, Run: func(_ context.Context, _ map[string]string, out io.Writer) error {
			<-release
			fmt.Fprint(out, "done")
			return nil
		}},
	)

	run, err := service.Run(ctx, 7, "greet", map[string]string{"name": "Dhaka", "mood": "sad"})
	if err != nil || run.Status != "ok" || run.Output != "hello Dhaka (false, sad)" || run.ActorUserID != 7 {
		t.Fatalf("greet = %+v, %v", run, err)
	}
	for name, options := range map[string]map[string]string{
		"unknown option": {"colour": "red"},
		"bad bool":       {"loud": "yes"},
		"bad number":     {"times": "many"},
		"bad choice":     {"mood": "angry"},
	} {
		if _, err := service.Run(ctx, 7, "greet", options); !apperr.Is(err, apperr.ValidationFailed) {
			t.Errorf("%s: error = %v, want validation", name, err)
		}
	}
	if _, err := service.Run(ctx, 7, "rm -rf", nil); !apperr.Is(err, apperr.ResourceNotFound) {
		t.Fatalf("unknown command error = %v", err)
	}

	failed, err := service.Run(ctx, 7, "fail", nil)
	if err != nil || failed.Status != "error" || failed.Error == nil || *failed.Error != "boom" {
		t.Fatalf("fail = %+v, %v", failed, err)
	}
	panicked, err := service.Run(ctx, 7, "panic", nil)
	if err != nil || panicked.Status != "error" {
		t.Fatalf("panic = %+v, %v", panicked, err)
	}

	// Background commands return a running record, then finish.
	started, err := service.Run(ctx, 7, "slow", nil)
	if err != nil || started.Status != runStatusRunning {
		t.Fatalf("slow start = %+v, %v", started, err)
	}
	close(release)
	service.Wait()
	finished, err := service.Get(ctx, started.ID)
	if err != nil || finished.Status != "ok" || finished.Output != "done" {
		t.Fatalf("slow finish = %+v, %v", finished, err)
	}

	summaries, err := service.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, summary := range summaries {
		if summary.Name == "greet" && (summary.Runs != 1 || summary.LastStatus == nil || *summary.LastStatus != "ok") {
			t.Fatalf("greet summary = %+v", summary)
		}
	}
	runs, err := service.Runs(ctx, "fail", 0, 10)
	if err != nil || len(runs) != 1 || runs[0].Command != "fail" {
		t.Fatalf("Runs(fail) = %+v, %v", runs, err)
	}
}
