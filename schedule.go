package cron

import "time"

// DelaySchedule 按固定间隔调度：Next(t) = t.Add(Delay)。
//
// t 是调度器传入的基准时刻（触发时的 now）。因此：
//   - 间隔相对“应触发时间”，一般不受 Job 执行时长影响；
//   - 若事件循环曾被阻塞，t 为醒来后的时间，可能出现追赶式连触发。
type DelaySchedule struct {
	Delay time.Duration
}

// Next 返回 t + Delay。
func (s DelaySchedule) Next(t time.Time) time.Time { return t.Add(s.Delay) }

// Every 构造 DelaySchedule，表示每隔 delay 触发一次。
//
//	cron.Every(5 * time.Minute)
//	cron.Every(30 * time.Second)
func Every(delay time.Duration) DelaySchedule {
	return DelaySchedule{Delay: delay}
}
