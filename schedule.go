package cron

import "time"

// DelaySchedule 是一个简单的延迟调度器
// 基于固定的时间间隔进行调度
type DelaySchedule struct {
	Delay time.Duration // 任务执行间隔
}

// Next 计算下一次执行时间
// 参数t是当前时间，返回t加上延迟时间后的时间
func (s DelaySchedule) Next(t time.Time) time.Time {
	return t.Add(s.Delay)
}

// Every 创建一个固定间隔的调度器
// 参数delay是任务执行的间隔时间
// 例如: Every(5*time.Minute)创建一个每5分钟执行一次的调度器
func Every(delay time.Duration) DelaySchedule {
	return DelaySchedule{
		Delay: delay,
	}
}
