package cron

import "time"

// DelaySchedule 基于固定间隔的调度器。
type DelaySchedule struct {
	Delay time.Duration
}

// Next 返回 t + Delay。
func (s DelaySchedule) Next(t time.Time) time.Time { return t.Add(s.Delay) }

// Every 创建固定间隔的调度器。例如 Every(5*time.Minute) 每 5 分钟执行一次。
func Every(delay time.Duration) DelaySchedule {
	return DelaySchedule{Delay: delay}
}
