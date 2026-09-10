package cron

import (
	"testing"
	"time"
)

// TestScheduleInterface 校验若干 Schedule 实现的 Next 行为：
// 小时间隔、立即触发、每日定点。
func TestScheduleInterface(t *testing.T) {
	hourly := &TestSchedule{}
	now := time.Now()
	next := hourly.Next(now)

	if next.Sub(now) != 1*time.Hour {
		t.Errorf("expected 1 hour difference, got %v", next.Sub(now))
	}

	immediate := &ImmediateSchedule{}
	next = immediate.Next(now)

	if !next.Equal(now) {
		t.Errorf("expected immediate time, got %v", next)
	}

	daily := &DailySchedule{Hour: 9, Minute: 0}
	next = daily.Next(now)

	if next.Hour() != 9 || next.Minute() != 0 {
		t.Errorf("expected 9:00, got %d:%02d", next.Hour(), next.Minute())
	}
}

// DailySchedule 测试/示例用：每天 Hour:Minute；若已过则推到次日。
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
