package cron

import (
	"testing"
	"time"
)

func TestEvery(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		delay time.Duration
		want  time.Duration
	}{
		{"second", time.Second, time.Second},
		{"minute", time.Minute, time.Minute},
		{"hour", time.Hour, time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			next := Every(tt.delay).Next(now)
			if got := next.Sub(now); got != tt.want {
				t.Fatalf("Next delta=%v; want %v", got, tt.want)
			}
		})
	}
}

func TestScheduleImplementations(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 1, 10, 30, 0, 0, time.UTC)

	t.Run("TestSchedule hourly", func(t *testing.T) {
		t.Parallel()
		next := (&TestSchedule{}).Next(now)
		if next.Sub(now) != time.Hour {
			t.Fatalf("got %v; want 1h", next.Sub(now))
		}
	})

	t.Run("ImmediateSchedule", func(t *testing.T) {
		t.Parallel()
		next := (&ImmediateSchedule{}).Next(now)
		if !next.Equal(now) {
			t.Fatalf("got %v; want %v", next, now)
		}
	})

	t.Run("DailySchedule future today", func(t *testing.T) {
		t.Parallel()
		// now=10:30，目标 15:00 → 今天
		next := (&DailySchedule{Hour: 15, Minute: 0}).Next(now)
		if next.Hour() != 15 || next.Minute() != 0 || next.Day() != now.Day() {
			t.Fatalf("got %v", next)
		}
	})

	t.Run("DailySchedule past pushes tomorrow", func(t *testing.T) {
		t.Parallel()
		// now=10:30，目标 09:00 → 明天
		next := (&DailySchedule{Hour: 9, Minute: 0}).Next(now)
		if next.Hour() != 9 || next.Minute() != 0 {
			t.Fatalf("got %v", next)
		}
		if !next.After(now) {
			t.Fatalf("expected after now, got %v", next)
		}
		if next.Day() == now.Day() {
			t.Fatalf("expected next day, got same day %v", next)
		}
	})
}

// DailySchedule 测试用：每天 Hour:Minute；若已过则推到次日。
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
