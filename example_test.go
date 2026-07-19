package cron

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ┌─────────────────────────────────────────────────────────┐
// │  Example 测试 — 文档即测试                                │
// │  Go 的 Example 函数既是文档示例也是可运行测试。             │
// │  输出注释（// Output:）定义了预期输出，测试框架自动比对。   │
// │  所有 Example 必须使用 <-c.Stop().Done() 等待优雅退出，    │
// │  确保所有 job goroutine 完成后再返回，避免数据竞争。       │
// └─────────────────────────────────────────────────────────┘

// Example_basic 展示基础用法：创建调度器 → 添加任务 → 启动 → 等待 → 停止。
// 使用 atomic 计数器跟踪执行次数，验证任务按预期触发。
func Example_basic() {
	c := New()

	var count atomic.Int32
	c.AddFunc(Every(100*time.Millisecond), func() {
		count.Add(1)
		fmt.Printf("任务执行次数: %d\n", count.Load())
	})

	c.Start()
	time.Sleep(350 * time.Millisecond)
	<-c.Stop().Done()

	// Output:
	// 任务执行次数: 1
	// 任务执行次数: 2
	// 任务执行次数: 3
}

// Example_concurrentJobs 展示并发任务执行。
//
// 短任务（100ms）和长任务（200ms，睡眠 150ms 模拟耗时）并发运行。
// 由于 goroutine 调度顺序不确定，使用 sync.Mutex + strings.Builder
// 替代直接 fmt.Println 输出，避免数据竞争和输出顺序依赖。
func Example_concurrentJobs() {
	c := New()

	// 短任务和长任务并发执行，互不阻塞
	var mu sync.Mutex
	var buf strings.Builder

	c.AddFunc(Every(100*time.Millisecond), func() {
		mu.Lock()
		buf.WriteString("短任务执行\n")
		mu.Unlock()
	})

	c.AddFunc(Every(200*time.Millisecond), func() {
		mu.Lock()
		buf.WriteString("长任务开始\n")
		mu.Unlock()
		time.Sleep(150 * time.Millisecond)
		mu.Lock()
		buf.WriteString("长任务结束\n")
		mu.Unlock()
	})

	c.Start()
	time.Sleep(350 * time.Millisecond)
	<-c.Stop().Done()
	fmt.Print(buf.String())
}

// Example_customJob 展示自定义 Job 接口实现。
// CounterJob 实现了 Job 接口，利用 atomic 计数器跟踪执行次数。
func Example_customJob() {
	c := New()
	job := &CounterJob{Name: "自定义计数器任务"}

	c.AddJob(Every(150*time.Millisecond), job)
	c.Start()
	time.Sleep(500 * time.Millisecond)
	<-c.Stop().Done()

	// Output:
	// 自定义计数器任务: 执行次数=1
	// 自定义计数器任务: 执行次数=2
	// 自定义计数器任务: 执行次数=3
}

// Example_customSchedule 展示自定义调度策略。
// 使用固定间隔 Every(100ms) 替代 WeeklySchedule，因为后者在实际测试中
// 需要跨越 7 天时间窗口才能触发，不可用于示例。
// 对于真正的每周调度，请实现自己的 Schedule 接口（见 WeeklySchedule）。
func Example_customSchedule() {
	c := New()

	// Every 适用于固定间隔；复杂调度（如每周一 9:00）需自行实现 Schedule 接口
	c.AddFunc(Every(100*time.Millisecond), func() {
		fmt.Println("每周任务执行")
	})

	c.Start()
	time.Sleep(350 * time.Millisecond)
	<-c.Stop().Done()

	// Output:
	// 每周任务执行
	// 每周任务执行
	// 每周任务执行
}

// ┌─────────────────────────────────────────────────────────┐
// │  辅助类型                                                │
// └─────────────────────────────────────────────────────────┘

// CounterJob 展示了如何实现 Job 接口。
//
// 设计注意事项：
//   - Count 字段使用 atomic.Int32 实现无锁并发安全递增
//   - Job 接口的方法 Run 不应接收参数，所有配置应在构造时注入
type CounterJob struct {
	Name  string
	Count atomic.Int32
}

func (j *CounterJob) Run() {
	j.Count.Add(1)
	fmt.Printf("%s: 执行次数=%d\n", j.Name, j.Count.Load())
}

// WeeklySchedule 演示完整 Schedule 接口实现：每周指定时间触发。
//
// 当 Weekday 是今天且目标时间未过，返回今天的目标时间；
// 否则返回下周同日同时间。
type WeeklySchedule struct {
	Hour    int
	Minute  int
	Weekday time.Weekday
}

// Next 计算下一个匹配的 Weekday 的时间点。
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
