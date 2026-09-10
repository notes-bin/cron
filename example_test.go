package cron

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Example 既是 godoc 示例也是可运行测试。
// 必须使用 <-c.Stop().Done() 等待 Job 结束，避免返回后仍有 goroutine 写共享状态。

// Example_basic 演示：创建 → 添加 → Start → 等待 → Stop。
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

// Example_slog 演示用薄适配器将 Logger 接到 log/slog。
// 无 Output 断言（slog 文本含时间戳）；编译即可验证用法。
func Example_slog() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	c := New(WithLogger(SlogLogger{L: log}))

	c.AddFunc(Every(100*time.Millisecond), func() {})
	c.Start()
	time.Sleep(150 * time.Millisecond)
	<-c.Stop().Done()
}

// SlogLogger 将 cron.Logger 转发到 *slog.Logger（kv 与 slog 属性列表同形）。
type SlogLogger struct {
	L *slog.Logger
}

func (s SlogLogger) Info(msg string, kv ...any)  { s.L.Info(msg, kv...) }
func (s SlogLogger) Error(msg string, kv ...any) { s.L.Error(msg, kv...) }

var _ Logger = SlogLogger{}

// Example_concurrentJobs 演示长短任务并发；用 Mutex+Builder 固定可测输出。
func Example_concurrentJobs() {
	c := New()

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

// Example_customJob 演示实现 Job 接口（CounterJob）。
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

// Example_customSchedule 演示固定间隔；复杂日历规则请自实现 Schedule（见 WeeklySchedule）。
func Example_customSchedule() {
	c := New()

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

// CounterJob 示例 Job：用 atomic 统计执行次数。
type CounterJob struct {
	Name  string
	Count atomic.Int32
}

func (j *CounterJob) Run() {
	j.Count.Add(1)
	fmt.Printf("%s: 执行次数=%d\n", j.Name, j.Count.Load())
}

// WeeklySchedule 示例 Schedule：每周指定星期几的 Hour:Minute。
type WeeklySchedule struct {
	Hour    int
	Minute  int
	Weekday time.Weekday
}

// Next 返回下一个匹配的周内时间点。
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
