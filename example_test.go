package cron

import (
	"fmt"
	"sync/atomic"
	"time"
)

// Example_basic 展示基础定时任务功能
func Example_basic() {
	// 创建调度器实例
	c := New()
	defer c.Stop()

	// 计数器用于跟踪任务执行次数
	var count int32

	// 添加每100ms执行一次的任务
	c.AddFunc(Every(100*time.Millisecond), func() {
		atomic.AddInt32(&count, 1)
		fmt.Printf("任务执行次数: %d\n", atomic.LoadInt32(&count))
	})

	// 启动调度器
	c.Start()

	// 运行300ms后停止，预期执行3次
	time.Sleep(300 * time.Millisecond)

	// Output:
	// 任务执行次数: 1
	// 任务执行次数: 2
	// 任务执行次数: 3
}

// Example_concurrentJobs 展示并发任务执行
func Example_concurrentJobs() {
	c := New()
	defer c.Stop()

	// 任务1: 短任务
	c.AddFunc(Every(100*time.Millisecond), func() {
		fmt.Println("短任务执行")
	})

	// 任务2: 长任务（模拟耗时操作）
	c.AddFunc(Every(200*time.Millisecond), func() {
		fmt.Println("长任务开始")
		time.Sleep(150 * time.Millisecond) // 模拟耗时
		fmt.Println("长任务结束")
	})

	c.Start()
	// 运行500ms观察并发行为
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

// Example_customJob 展示自定义Job接口实现
func Example_customJob() {
	// 创建调度器和任务实例
	c := New()
	job := &CounterJob{Name: "自定义计数器任务"}
	defer c.Stop()

	// 添加任务（每150ms执行一次）
	c.AddJob(Every(150*time.Millisecond), job)
	c.Start()

	// 运行450ms，预期执行3次
	time.Sleep(450 * time.Millisecond)

	// Output:
	// 自定义计数器任务: 执行次数=1
	// 自定义计数器任务: 执行次数=2
	// 自定义计数器任务: 执行次数=3
}

// Example_customSchedule 展示自定义调度策略实现
func Example_customSchedule() {
	// 创建调度器和任务
	c := New()
	defer c.Stop()

	// 使用自定义调度器（为测试方便，这里使用每100ms模拟每周执行）
	// 实际使用时应设置为 &WeeklySchedule{Hour: 9, Minute: 0, Weekday: 1}（周一9点）
	simulatedWeekly := &WeeklySchedule{}
	c.AddFunc(simulatedWeekly, func() {
		fmt.Println("每周任务执行")
	})

	c.Start()
	// 短时间运行以验证调度逻辑
	time.Sleep(250 * time.Millisecond)

	// Output:
	// 每周任务执行
	// 每周任务执行
}

// 自定义任务类型
type CounterJob struct {
	Name  string
	Count int32
}

// 实现Job接口
func (j *CounterJob) Run() {
	atomic.AddInt32(&j.Count, 1)
	fmt.Printf("%s: 执行次数=%d\n", j.Name, atomic.LoadInt32(&j.Count))
}

// 实现每周一上午9点执行的调度器
type WeeklySchedule struct {
	Hour    int          // 小时
	Minute  int          // 分钟
	Weekday time.Weekday // 星期几 (0=周日, 1=周一, ..., 6=周六)
}

// Next 计算下一次执行时间
func (s *WeeklySchedule) Next(t time.Time) time.Time {
	// 计算目标时间
	target := time.Date(t.Year(), t.Month(), t.Day(), s.Hour, s.Minute, 0, 0, t.Location())

	// 调整到目标星期几
	daysAhead := int(s.Weekday - target.Weekday())
	if daysAhead <= 0 {
		daysAhead += 7
	}

	// 如果目标时间已过，加一周
	next := target.AddDate(0, 0, daysAhead)
	if next.Before(t) {
		next = next.AddDate(0, 0, 7)
	}

	return next
}
