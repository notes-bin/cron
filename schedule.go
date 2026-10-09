package cron

import "time"

// DelaySchedule 按固定间隔调度：Next(t) = t.Add(Delay)。
//
// t 是调度器传入的基准时刻（触发时的 now）。因此：
//   - 间隔相对“应触发时间”，一般不受 Job 执行时长影响；
//   - 若事件循环曾被阻塞，t 为醒来后的时间，可能出现追赶式连触发。
//
// Delay 为零值时 Next 返回零时间（不再调度），使零值 DelaySchedule 可安全使用。
type DelaySchedule struct {
	Delay time.Duration
}

// Next 返回 t + Delay。
// Delay <= 0 时返回零值（有用的零值语义：永不触发，并防止热循环）。
func (s DelaySchedule) Next(t time.Time) time.Time {
	if s.Delay <= 0 {
		return time.Time{}
	}
	return t.Add(s.Delay)
}

// Every 构造 DelaySchedule，表示每隔 delay 触发一次。
// delay <= 0 时 panic：构造入口显式拒绝无效间隔；字面量 DelaySchedule{} 则走零值“不调度”。
//
//	cron.Every(5 * time.Minute)
//	cron.Every(30 * time.Second)
func Every(delay time.Duration) DelaySchedule {
	if delay <= 0 {
		panic("cron: non-positive delay")
	}
	return DelaySchedule{Delay: delay}
}
