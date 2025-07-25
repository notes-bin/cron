package cron

import (
	"testing"
	"time"
)

// TestScheduleInterface verifies that schedule implementations work correctly
func TestScheduleInterface(t *testing.T) {
	// Test hourly schedule
	hourly := &TestSchedule{}
	now := time.Now()
	next := hourly.Next(now)

	if next.Sub(now) != 1*time.Hour {
		t.Errorf("expected 1 hour difference, got %v", next.Sub(now))
	}

	// Test immediate schedule
	immediate := &ImmediateSchedule{}
	next = immediate.Next(now)

	if !next.Equal(now) {
		t.Errorf("expected immediate time, got %v", next)
	}

	// Test custom daily schedule
	daily := &DailySchedule{Hour: 9, Minute: 0}
	next = daily.Next(now)

	// Verify the scheduled time is today or tomorrow at 9:00
	expectedHour := 9
	expectedMinute := 0
	if next.Hour() != expectedHour || next.Minute() != expectedMinute {
		t.Errorf("expected %d:%02d, got %d:%02d", expectedHour, expectedMinute, next.Hour(), next.Minute())
	}
}

// DailySchedule is a test implementation of the Schedule interface
type DailySchedule struct {
	Hour, Minute int
}

func (s *DailySchedule) Next(t time.Time) time.Time {
	next := time.Date(t.Year(), t.Month(), t.Day(), s.Hour, s.Minute, 0, 0, t.Location())
	if next.Before(t) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
