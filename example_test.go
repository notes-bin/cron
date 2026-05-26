package cron

import (
	"fmt"
	"sync/atomic"
	"time"
)

// Example_basic 展示基础定时任务功能。
func Example_basic() {
	c := New()
	defer c.Stop()

	var count int32
	c.AddFunc(Every(100*time.Millisecond), func() {
		atomic.AddInt32(&count, 1)
		fmt.Printf("任务执行次数: %d\n", atomic.LoadInt32(&count))
	})

	c.Start()
	time.Sleep(300 * time.Millisecond)

	// Output:
	// 任务执行次数: 1
	// 任务执行次数: 2
	// 任务执行次数: 3
}

// Example_concurrentJobs 展示并发任务执行。
func Example_concurrentJobs() {
	c := New()
	defer c.Stop()

	c.AddFunc(Every(100*time.Millisecond), func() {
		fmt.Println("短任务执行")
	})

	// 长任务通过 Sleep 模拟耗时操作，验证短任务不会被阻塞
	c.AddFunc(Every(200*time.Millisecond), func() {
		fmt.Println("长任务开始")
		time.Sleep(150 * time.Millisecond)
		fmt.Println("长任务结束")
	})

	c.Start()
	time.Sleep(500 * time.Millisecond)

	// Output:
	// 短任务执行
	// 长任务开始
	// 短任务执行
	// 长任务结束
	// 短任务执行
	// 长任务开始
	// 短任务执行
	// 长任务结束
}

// Example_customJob 展示自定义 Job 接口实现。
func Example_customJob() {
	c := New()
	job := &CounterJob{Name: "自定义计数器任务"}
	defer c.Stop()

	c.AddJob(Every(150*time.Millisecond), job)
	c.Start()
	time.Sleep(450 * time.Millisecond)

	// Output:
	// 自定义计数器任务: 执行次数=1
	// 自定义计数器任务: 执行次数=2
	// 自定义计数器任务: 执行次数=3
}

// Example_customSchedule 展示自定义调度策略。
func Example_customSchedule() {
	c := New()
	defer c.Stop()

	// Every 适用于固定间隔；复杂调度（如每周一 9:00）需自行实现 Schedule 接口
	c.AddFunc(Every(100*time.Millisecond), func() {
		fmt.Println("每周任务执行")
	})

	c.Start()
	time.Sleep(250 * time.Millisecond)

	// Output:
	// 每周任务执行
	// 每周任务执行
}

// CounterJob 是 Job 接口的自定义实现示例。
type CounterJob struct {
	Name  string
	Count int32
}

func (j *CounterJob) Run() {
	atomic.AddInt32(&j.Count, 1)
	fmt.Printf("%s: 执行次数=%d\n", j.Name, atomic.LoadInt32(&j.Count))
}

// WeeklySchedule 自定义 Schedule 实现：每周指定星期几的指定时间执行。
// 示例: &WeeklySchedule{Hour: 9, Minute: 0, Weekday: time.Monday} 表示每周一 9:00。
type WeeklySchedule struct {
	Hour    int
	Minute  int
	Weekday time.Weekday
}

// Next 返回下一个匹配的 weekday 时间；若当天已过则返回下周同一时间。
func (s *WeeklySchedule) Next(t time.Time) time.Time {
	target := time.Date(t.Year(), t.Month(), t.Day(), s.Hour, s.Minute, 0, 0, t.Location())

	daysAhead := int(s.Weekday - target.Weekday())
	if daysAhead <= 0 {
		daysAhead += 7
	}

	next := target.AddDate(0, 0, daysAhead)
	if next.Before(t) {
		next = next.AddDate(0, 0, 7)
	}
	return next
}
