package cron

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestCronInitialization verifies that a Cron instance initializes correctly
func TestCronInitialization(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.location != time.Local {
		t.Errorf("expected location %v, got %v", time.Local, c.location)
	}
	if c.running {
		t.Error("new Cron should not be running")
	}
}

// TestAddJob verifies that jobs can be added to the cron scheduler
func TestAddJob(t *testing.T) {
	c := New()
	job := FuncJob(func() {})
	id := c.AddJob(&TestSchedule{}, job)

	if id == 0 {
		t.Error("expected non-zero EntryID")
	}

	c.entriesMu.RLock()
	defer c.entriesMu.RUnlock()

	found := false
	for _, entry := range c.entries {
		if entry.ID == id {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("entry with ID %d not found", id)
	}
}

// TestRemoveJob verifies that jobs can be removed from the cron scheduler
func TestRemoveJob(t *testing.T) {
	c := New()
	job := FuncJob(func() {})
	id := c.AddJob(&TestSchedule{}, job)

	c.Remove(id)

	c.entriesMu.RLock()
	defer c.entriesMu.RUnlock()

	for _, entry := range c.entries {
		if entry.ID == id {
			t.Errorf("entry with ID %d was not removed", id)
		}
	}
}

// TestStartStop verifies that the cron scheduler starts and stops correctly
func TestStartStop(t *testing.T) {
	c := New()
	c.Start()

	c.runningMu.Lock()
	running := c.running
	c.runningMu.Unlock()

	if !running {
		t.Error("Cron should be running after Start()")
	}

	ctx := c.Stop()
	<-ctx.Done()

	c.runningMu.Lock()
	running = c.running
	c.runningMu.Unlock()

	if running {
		t.Error("Cron should not be running after Stop()")
	}
}

// TestJobExecution verifies that jobs are executed according to schedule
func TestJobExecution(t *testing.T) {
	// Setup test logging
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))

	c := New()
	var executed bool
	job := FuncJob(func() {
		executed = true
	})

	// Schedule to run immediately
	c.AddJob(&ImmediateSchedule{}, job)
	c.Start()
	defer c.Stop()

	// Wait for job to execute
	timer := time.NewTimer(1 * time.Second)
	<-timer.C
	if !executed {
		t.Error("job was not executed")
	}
}

// TestSchedule implements the Schedule interface for testing
type TestSchedule struct{}

func (s *TestSchedule) Next(t time.Time) time.Time {
	return t.Add(1 * time.Hour)
}

// ImmediateSchedule implements the Schedule interface to run immediately
type ImmediateSchedule struct{}

func (s *ImmediateSchedule) Next(t time.Time) time.Time {
	return t
}
