package cron

import (
	"testing"
	"time"
)

func TestEvery(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		delay    time.Duration
		expected time.Duration
	}{
		{"second", time.Second, time.Second},
		{"minute", time.Minute, time.Minute},
		{"hour", time.Hour, time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			next := Every(tt.delay).Next(now)
			if got := next.Sub(now); got != tt.expected {
				t.Fatalf("Next delta=%v; expected %v", got, tt.expected)
			}
		})
	}
}

func TestEveryNonPositivePanics(t *testing.T) {
	t.Parallel()
	for _, delay := range []time.Duration{0, -time.Second} {
		t.Run(delay.String(), func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			Every(delay)
		})
	}
}

func TestDelayScheduleNonPositiveNextIsZero(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	for _, delay := range []time.Duration{0, -time.Minute} {
		next := DelaySchedule{Delay: delay}.Next(now)
		if !next.IsZero() {
			t.Fatalf("Delay=%v: Next=%v; expected zero", delay, next)
		}
	}
}

func TestScheduleImplementations(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 1, 10, 30, 0, 0, time.UTC)

	t.Run("hourly schedule", func(t *testing.T) {
		t.Parallel()
		next := (&hourlySchedule{}).Next(now)
		if next.Sub(now) != time.Hour {
			t.Fatalf("got %v; expected 1h", next.Sub(now))
		}
	})

	t.Run("immediate schedule", func(t *testing.T) {
		t.Parallel()
		next := (&immediateSchedule{}).Next(now)
		if !next.Equal(now) {
			t.Fatalf("got %v; expected %v", next, now)
		}
	})

	t.Run("daily schedule future today", func(t *testing.T) {
		t.Parallel()
		// now=10:30，目标 15:00 → 今天
		next := (&dailySchedule{Hour: 15, Minute: 0}).Next(now)
		if next.Hour() != 15 || next.Minute() != 0 || next.Day() != now.Day() {
			t.Fatalf("got %v", next)
		}
	})

	t.Run("daily schedule past pushes tomorrow", func(t *testing.T) {
		t.Parallel()
		// now=10:30，目标 09:00 → 明天
		next := (&dailySchedule{Hour: 9, Minute: 0}).Next(now)
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

	t.Run("weekly schedule same day future", func(t *testing.T) {
		t.Parallel()
		// 2026-03-02 为周一；09:00 → 目标周一 15:00 应为今天
		monday := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
		next := (&WeeklySchedule{Hour: 15, Minute: 0, Weekday: time.Monday}).Next(monday)
		if next.Weekday() != time.Monday || next.Day() != monday.Day() || next.Hour() != 15 {
			t.Fatalf("got %v; expected same monday 15:00", next)
		}
	})

	t.Run("weekly schedule same day past pushes next week", func(t *testing.T) {
		t.Parallel()
		monday := time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC)
		next := (&WeeklySchedule{Hour: 15, Minute: 0, Weekday: time.Monday}).Next(monday)
		if !next.After(monday) || next.Weekday() != time.Monday {
			t.Fatalf("got %v; expected next monday after %v", next, monday)
		}
		if next.Sub(monday) < 6*24*time.Hour {
			t.Fatalf("got %v; expected about one week later", next)
		}
	})
}

// dailySchedule 测试用：每天 Hour:Minute；未严格晚于 t 则推到次日。
type dailySchedule struct {
	Hour, Minute int
}

func (s *dailySchedule) Next(t time.Time) time.Time {
	next := time.Date(t.Year(), t.Month(), t.Day(), s.Hour, s.Minute, 0, 0, t.Location())
	if !next.After(t) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
