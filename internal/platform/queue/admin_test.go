package queue

import (
	"fmt"
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
)

func TestMapAdminJobDecodesApplicationKind(t *testing.T) {
	now := time.Now()
	row := &rivertype.JobRow{
		ID:          7,
		Kind:        riverArgsKind,
		Queue:       "default",
		State:       rivertype.JobStateAvailable,
		Priority:    2,
		Attempt:     1,
		MaxAttempts: 3,
		EncodedArgs: []byte(`{"kind":"mail.send","payload":{"to":"a@b.c"},"tenant_id":1}`),
		Errors:      []rivertype.AttemptError{{At: now, Attempt: 1, Error: "boom"}},
		CreatedAt:   now,
		ScheduledAt: now,
		AttemptedAt: &now,
		FinalizedAt: &now,
	}
	job := mapAdminJob(row)
	if job.Kind != "mail.send" {
		t.Errorf("kind = %q, want mail.send", job.Kind)
	}
	if job.RiverKind != riverArgsKind {
		t.Errorf("river kind = %q, want %q", job.RiverKind, riverArgsKind)
	}
	if job.State != "available" || job.Queue != "default" || job.Priority != 2 {
		t.Errorf("job = %+v", job)
	}
	if len(job.Errors) != 1 || job.Errors[0].Error != "boom" {
		t.Errorf("errors = %+v", job.Errors)
	}
	if len(job.Args) == 0 {
		t.Error("args are empty")
	}
}

func TestMapAdminJobFallsBackToRiverKind(t *testing.T) {
	row := &rivertype.JobRow{
		ID:          1,
		Kind:        riverArgsKind,
		EncodedArgs: []byte(`not-json`),
		State:       rivertype.JobStateRunning,
	}
	job := mapAdminJob(row)
	if job.Kind != riverArgsKind {
		t.Errorf("kind = %q, want fallback %q", job.Kind, riverArgsKind)
	}
	if string(job.Args) != "not-json" {
		t.Errorf("args = %s, want raw passthrough", job.Args)
	}
}

func TestMapAdminJobTruncatesErrorHistory(t *testing.T) {
	errors := make([]rivertype.AttemptError, 0, 15)
	for i := 0; i < 15; i++ {
		errors = append(errors, rivertype.AttemptError{
			At:      time.Now(),
			Attempt: i,
			Error:   fmt.Sprintf("attempt %d", i),
		})
	}
	row := &rivertype.JobRow{
		ID:          1,
		Kind:        riverArgsKind,
		EncodedArgs: []byte(`{"kind":"user.import"}`),
		Errors:      errors,
	}
	job := mapAdminJob(row)
	if len(job.Errors) != maxAdminJobErrors {
		t.Fatalf("errors = %d, want %d", len(job.Errors), maxAdminJobErrors)
	}
	if job.Errors[len(job.Errors)-1].Error != "attempt 14" {
		t.Errorf("last error = %q, want the newest attempt", job.Errors[len(job.Errors)-1].Error)
	}
}
