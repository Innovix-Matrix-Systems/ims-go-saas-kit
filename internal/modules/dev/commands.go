package dev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
)

// Command is one allowlisted maintenance command the dashboard can run —
// the same work as the matching `ims` CLI command, executed in-process
// (like running an artisan command from Telescope/Horizon-style tooling).
type Command struct {
	Name        string          `json:"name"` // e.g. "permission:sync"
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Group       string          `json:"group"`
	Confirm     bool            `json:"confirm"`    // ask before running (changes data)
	Background  bool            `json:"background"` // returns at once; watch the run
	Options     []CommandOption `json:"options"`
	Timeout     time.Duration   `json:"-"`
	// Run does the work and writes human-readable output.
	Run func(ctx context.Context, options map[string]string, out io.Writer) error `json:"-"`
}

// CommandOption is one input a command accepts.
type CommandOption struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // bool | number | text | select
	Default     string   `json:"default"`
	Choices     []string `json:"choices,omitempty"`
	Description string   `json:"description,omitempty"`
}

// CommandRun is one execution, stored in dev_actions (action command.run).
type CommandRun struct {
	ID          int64             `json:"id"`
	Command     string            `json:"command"`
	ActorUserID int64             `json:"actor_user_id"`
	Status      string            `json:"status"` // running | ok | error
	Options     map[string]string `json:"options"`
	Output      string            `json:"output"`
	Error       *string           `json:"error"`
	DurationMS  int32             `json:"duration_ms"`
	CreatedAt   time.Time         `json:"created_at"`
}

// CommandSummary is a command with its last run.
type CommandSummary struct {
	Command
	Runs       int64      `json:"runs"`
	LastStatus *string    `json:"last_status"`
	LastRunAt  *time.Time `json:"last_run_at"`
}

const (
	commandAction    = "command.run"
	maxCommandOutput = 64 << 10
	runStatusRunning = "running"
)

// CommandService runs dashboard commands and keeps their history.
type CommandService struct {
	queries  *dbgen.Queries
	commands map[string]Command
	order    []string
	logger   *slog.Logger
	wg       sync.WaitGroup
}

// NewCommandService registers the given commands.
func NewCommandService(queries *dbgen.Queries, logger *slog.Logger, commands ...Command) *CommandService {
	s := &CommandService{queries: queries, commands: map[string]Command{}, logger: logger}
	for _, command := range commands {
		if command.Options == nil {
			command.Options = []CommandOption{}
		}
		if command.Timeout == 0 {
			command.Timeout = 2 * time.Minute
		}
		s.commands[command.Name] = command
		s.order = append(s.order, command.Name)
	}
	return s
}

// List returns every command with its last run.
func (s *CommandService) List(ctx context.Context) ([]CommandSummary, error) {
	rows, err := s.queries.DevCommandRunSummary(ctx)
	if err != nil {
		return nil, database.MapError("dev.command_summary", err)
	}
	last := map[string]dbgen.DevCommandRunSummaryRow{}
	for _, row := range rows {
		if row.Target != nil {
			last[*row.Target] = row
		}
	}
	out := make([]CommandSummary, 0, len(s.order))
	for _, name := range s.order {
		summary := CommandSummary{Command: s.commands[name]}
		if row, ok := last[name]; ok {
			status := row.Status
			at := row.CreatedAt.Time
			summary.Runs, summary.LastStatus, summary.LastRunAt = row.Runs, &status, &at
		}
		out = append(out, summary)
	}
	return out, nil
}

// Runs lists recent runs, optionally of one command.
func (s *CommandService) Runs(ctx context.Context, command string, beforeID int64, limit int32) ([]CommandRun, error) {
	rows, err := s.queries.ListDevCommandRuns(ctx, dbgen.ListDevCommandRunsParams{Target: command, BeforeID: beforeID, RowLimit: limit})
	if err != nil {
		return nil, database.MapError("dev.command_runs", err)
	}
	out := make([]CommandRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, toCommandRun(row))
	}
	return out, nil
}

// Get returns one run.
func (s *CommandService) Get(ctx context.Context, id int64) (*CommandRun, error) {
	row, err := s.queries.GetDevAction(ctx, id)
	if err != nil || row.Action != commandAction {
		return nil, apperr.New(apperr.ResourceNotFound, "The command run was not found.")
	}
	run := toCommandRun(row)
	return &run, nil
}

// Run executes a command for actorID. Background commands return the
// running record at once; the rest return the finished run.
func (s *CommandService) Run(ctx context.Context, actorID int64, name string, options map[string]string) (*CommandRun, error) {
	command, ok := s.commands[name]
	if !ok {
		return nil, apperr.Newf(apperr.ResourceNotFound, "Command %q is not available.", name)
	}
	resolved, err := resolveOptions(command, options)
	if err != nil {
		return nil, err
	}
	params, _ := json.Marshal(map[string]any{"options": resolved})
	target := name
	row, err := s.queries.InsertDevAction(ctx, dbgen.InsertDevActionParams{
		ActorUserID: actorID, Action: commandAction, Target: &target, Params: params, Status: runStatusRunning,
	})
	if err != nil {
		return nil, database.MapError("dev.command_start", err)
	}

	// Commands outlive a cancelled request; each has its own deadline.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), command.Timeout)
	execute := func() *CommandRun {
		defer cancel()
		started := time.Now()
		var out bytes.Buffer
		runErr := safeRun(runCtx, command, resolved, &out)
		return s.finish(context.WithoutCancel(ctx), row.ID, resolved, out.String(), runErr, time.Since(started))
	}
	if command.Background {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			execute()
		}()
		run := toCommandRun(row)
		return &run, nil
	}
	return execute(), nil
}

// Wait blocks until background runs finish (tests, shutdown).
func (s *CommandService) Wait() { s.wg.Wait() }

func safeRun(ctx context.Context, command Command, options map[string]string, out io.Writer) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("command panicked: %v", recovered)
		}
	}()
	return command.Run(ctx, options, out)
}

func (s *CommandService) finish(ctx context.Context, id int64, options map[string]string, output string, runErr error, took time.Duration) *CommandRun {
	if len(output) > maxCommandOutput {
		output = output[:maxCommandOutput] + "\n… output truncated"
	}
	status := actionStatusOK
	var message *string
	if runErr != nil {
		status = "error"
		text := runErr.Error()
		message = &text
	}
	params, _ := json.Marshal(map[string]any{"options": options, "output": output})
	if err := s.queries.FinishDevAction(ctx, dbgen.FinishDevActionParams{
		ID: id, Status: status, ErrorMessage: message, DurationMs: int32(took.Milliseconds()), Params: params,
	}); err != nil && s.logger != nil {
		s.logger.WarnContext(ctx, "command run not saved", "id", id, "error", err)
	}
	row, err := s.queries.GetDevAction(ctx, id)
	if err != nil {
		return &CommandRun{ID: id, Status: status, Options: options, Output: output, Error: message}
	}
	run := toCommandRun(row)
	return &run
}

func resolveOptions(command Command, given map[string]string) (map[string]string, error) {
	resolved := map[string]string{}
	known := map[string]CommandOption{}
	for _, option := range command.Options {
		known[option.Name] = option
		resolved[option.Name] = option.Default
	}
	for name, value := range given {
		option, ok := known[name]
		if !ok {
			return nil, apperr.Validation(map[string][]string{"options." + name: {"Unknown option."}})
		}
		value = strings.TrimSpace(value)
		switch option.Type {
		case "bool":
			if value != "true" && value != "false" {
				return nil, apperr.Validation(map[string][]string{"options." + name: {"Must be true or false."}})
			}
		case "number":
			var number int
			if _, err := fmt.Sscan(value, &number); err != nil {
				return nil, apperr.Validation(map[string][]string{"options." + name: {"Must be a number."}})
			}
		case "select":
			if !contains(option.Choices, value) {
				return nil, apperr.Validation(map[string][]string{"options." + name: {"Must be one of " + strings.Join(option.Choices, ", ") + "."}})
			}
		}
		resolved[name] = value
	}
	return resolved, nil
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func toCommandRun(row dbgen.DevAction) CommandRun {
	var params struct {
		Options map[string]string `json:"options"`
		Output  string            `json:"output"`
	}
	_ = json.Unmarshal(row.Params, &params)
	if params.Options == nil {
		params.Options = map[string]string{}
	}
	name := ""
	if row.Target != nil {
		name = *row.Target
	}
	return CommandRun{
		ID: row.ID, Command: name, ActorUserID: row.ActorUserID, Status: row.Status, Options: params.Options,
		Output: params.Output, Error: row.ErrorMessage, DurationMS: row.DurationMs, CreatedAt: row.CreatedAt.Time,
	}
}

// Names lists the registered command names (sorted).
func (s *CommandService) Names() []string {
	names := append([]string(nil), s.order...)
	sort.Strings(names)
	return names
}
