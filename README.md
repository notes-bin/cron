# cron

一个轻量级 Go 定时任务调度库，支持动态增删任务、自定义日志和时区。

## 安装

```bash
go get github.com/notes-bin/cron
```

## 快速开始

```go
package main

import (
	"fmt"
	"time"

	"github.com/notes-bin/cron"
)

func main() {
	c := cron.New()
	defer c.Stop()

	// 每 3 秒执行一次
	c.AddFunc(cron.Every(3*time.Second), func() {
		fmt.Println("tick:", time.Now())
	})

	c.Start()
	time.Sleep(10 * time.Second)
}
```

## 调度策略

### 固定间隔

```go
cron.Every(5 * time.Minute)   // 每 5 分钟
cron.Every(1 * time.Hour)     // 每小时
cron.Every(30 * time.Second)  // 每 30 秒
```

### 自定义 Schedule

实现 `Schedule` 接口即可定义任意触发规则：

```go
// 每周一 9:00 执行
type WeeklySchedule struct{}

func (s *WeeklySchedule) Next(t time.Time) time.Time {
	daysUntilMonday := (8 - int(t.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	next := time.Date(t.Year(), t.Month(), t.Day()+daysUntilMonday, 9, 0, 0, 0, t.Location())
	return next
}

c.AddFunc(&WeeklySchedule{}, func() {
	fmt.Println("周一 9:00 执行")
})
```

## 日志

默认不输出日志。通过 `WithLogger` 注入自定义实现：

```go
type myLogger struct{}

func (l *myLogger) Info(msg string, kv ...any)  { log.Printf("INFO: %s %v", msg, kv) }
func (l *myLogger) Error(msg string, kv ...any) { log.Printf("ERROR: %s %v", msg, kv) }

c := cron.New(cron.WithLogger(&myLogger{}))
```

Job 内部 panic 会被捕获并通过 Logger.Error 报告，同时输出到 stderr 作为兜底。

## 时区

```go
loc, _ := time.LoadLocation("Asia/Shanghai")
c := cron.New(cron.WithLocation(loc))
```

## 动态管理

```go
id := c.AddFunc(cron.Every(time.Minute), someFunc) // 添加
c.Remove(id)                                        // 删除
```

调度器启动后也可安全调用 `AddFunc` / `AddJob` / `Remove`。

## 自定义 Job

```go
type MyJob struct{ Name string }

func (j *MyJob) Run() { fmt.Println("job:", j.Name) }

c.AddJob(cron.Every(time.Second), &MyJob{Name: "demo"})
```

## 停止与优雅退出

`Stop()` 返回一个 `context.Context`，所有正在执行的 job 完成后会被取消：

```go
ctx := c.Stop()
<-ctx.Done() // 等待所有运行中的 job 完成
```

## API 概览

| 方法 | 说明 |
|------|------|
| `New(opts ...Option)` | 创建调度器 |
| `AddFunc(schedule, func())` | 添加函数型任务，返回 ID |
| `AddJob(schedule, Job)` | 添加 Job 接口任务，返回 ID |
| `Remove(id)` | 按 ID 删除任务 |
| `Start()` | 后台启动 |
| `Run()` | 阻塞当前 goroutine 启动 |
| `Stop()` | 停止，返回等待完成的 context |
| `Every(delay)` | 创建固定间隔的 Schedule |
| `WithLogger(logger)` | 注入日志实现 |
| `WithLocation(loc)` | 设置时区 |

## License

MIT
