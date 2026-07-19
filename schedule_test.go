package cron

import (
	"testing"
	"time"
)

// TestScheduleInterface 验证各种 Schedule 实现的 Next 计算是否正确。
// 测试场景：
//   - TestSchedule: 每小时触发，验证间隔为 1h
//   - ImmediateSchedule: 立即触发，验证返回当前时间
//   - DailySchedule: 每天 9:00 触发，验证返回的小时和分钟正确
func TestScheduleInterface(t *testing.T) {
	// 每小时触发
	hourly := &TestSchedule{}
	now := time.Now()
	next := hourly.Next(now)

	if next.Sub(now) != 1*time.Hour {
		t.Errorf("expected 1 hour difference, got %v", next.Sub(now))
	}

	// 立即触发
	immediate := &ImmediateSchedule{}
	next = immediate.Next(now)

	if !next.Equal(now) {
		t.Errorf("expected immediate time, got %v", next)
	}

	// 每天 9:00 触发
	daily := &DailySchedule{Hour: 9, Minute: 0}
	next = daily.Next(now)

	expectedHour := 9
	expectedMinute := 0
	if next.Hour() != expectedHour || next.Minute() != expectedMinute {
		t.Errorf("expected %d:%02d, got %d:%02d", expectedHour, expectedMinute, next.Hour(), next.Minute())
	}
}

// DailySchedule 用于测试，每天指定时间触发；当天已过则推到次日。
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
