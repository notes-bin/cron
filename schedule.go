package cron

import "time"

type DelaySchedule struct {
	Delay time.Duration
}

func (s DelaySchedule) Next(t time.Time) time.Time {
	return t.Add(s.Delay)
}

func Every(delay time.Duration) DelaySchedule {
	return DelaySchedule{
		Delay: delay,
	}
}
