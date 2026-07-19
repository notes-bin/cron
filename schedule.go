package cron

import "time"

// DelaySchedule 是最常用的调度器实现：固定时间间隔触发。
//
// 实现原理: Next(t) = t + Delay
// 即每次触发后，以下次触发时间（而非当前时间）为基准增加 Delay。
// 这意味着:
//   - 如果 Job 执行耗时 10s，Delay 是 1m，
//     实际间隔始终是 1m（从上次触发到下次触发），不受 Job 执行时间影响。
//   - 但如果系统在触发时被阻塞（如高负载），
//     调度器使用 now 基准，可能导致快速连续触发追赶进度。
type DelaySchedule struct {
	Delay time.Duration
}

// Next 返回 t + Delay。
func (s DelaySchedule) Next(t time.Time) time.Time { return t.Add(s.Delay) }

// Every 创建固定间隔的 DelaySchedule。
//
// 使用示例:
//
//	cron.Every(5 * time.Minute)   // 每 5 分钟
//	cron.Every(1 * time.Hour)     // 每小时
//	cron.Every(30 * time.Second)  // 每 30 秒
func Every(delay time.Duration) DelaySchedule {
	return DelaySchedule{Delay: delay}
}
