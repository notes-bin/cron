package cron

import (
	"sync"
	"testing"
	"time"
)

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

func TestJobExecution(t *testing.T) {
	c := New()
	done := make(chan struct{})
	var once sync.Once
	job := FuncJob(func() {
		once.Do(func() { close(done) })
	})

	c.AddJob(&ImmediateSchedule{}, job)
	c.Start()
	defer c.Stop()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("job was not executed")
	}
}

// TestSchedule 用于测试，每小时触发一次。
type TestSchedule struct{}

func (s *TestSchedule) Next(t time.Time) time.Time { return t.Add(1 * time.Hour) }

// ImmediateSchedule 用于测试，立即触发。
type ImmediateSchedule struct{}

func (s *ImmediateSchedule) Next(t time.Time) time.Time { return t }
