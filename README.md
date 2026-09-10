# cron

轻量级 Go 定时任务调度库。要求 **Go 1.26+**。

```bash
go get github.com/notes-bin/cron
```

---

## 目录

- [设计原理](#设计原理)
  - [并发模型](#并发模型)
  - [事件驱动架构](#事件驱动架构)
  - [Channel 策略](#channel-策略)
  - [Shutdown 流程](#shutdown-流程)
- [工作流程](#工作流程)
  - [主循环时序](#主循环时序)
  - [调度计算](#调度计算)
- [使用指南](#使用指南)
- [开发指南](#开发指南)
- [API 参考](#api-参考)
- [TODO](#todo)
- [License](#license)

---

## 设计原理

### 并发模型

```
┌──────────────┐  channel / 直改   ┌──────────────────────────────────┐
│ External API │ ───────────────→  │ run() 单 goroutine               │
│              │                   │                                  │
│ AddJob ──────│── add ──────────→ │ for {                            │
│ Remove ──────│── remove ───────→ │   slices.SortFunc(entries, …)    │
│ Stop ────────│── stop ─────────→ │   timer = 最早 Next − now        │
│ Start/Run ───│── go / 阻塞 ────→ │   select { timer|add|remove|stop}│
└──────────────┘                   │ }                                │
                                   │ defer:                           │
        runDone ←──────────────────│   close(runDone)                 │
        （解除阻塞发送）            │   running=false                  │
                                   │   jobWaiter.Wait()               │
                                   │   stopCancel()                   │
                                   └──────────────────────────────────┘
```

**运行中**：`entries` 只由 `run()` 读写；增删经无缓冲 channel 序列化。

**未运行 / run 已退出**：`AddJob`/`Remove` 在 `runningMu` 下直接改 `entries`。

**`runningMu`** 保护 `running`、`nextID`，以及上述非 `run()` 路径上的 `entries`。

**`running`** 只在 `run()` 的 defer 里清为 `false`；`Stop()` 只发 stop，避免与仍在跑的循环竞态。

Job 经 `sync.WaitGroup.Go` 在独立 goroutine 执行，不阻塞事件循环。

### 事件驱动架构

| 事件 | 来源 | 处理 |
|------|------|------|
| `timer.C` | 最早任务到期 | 触发到期 Job，更新 `Next` |
| `add` | `AddJob` / `AddFunc` | 计算首次 `Next` 并 append |
| `remove` | `Remove` | `slices.DeleteFunc` 按 ID 删除 |
| `stop` | `Stop` | 停 timer 并退出循环 |

无任务或队头 `Next` 为零时，用 **nil channel** 参与 select，不创建超长 timer。

add/remove/stop 分支会 `stopTimer`，避免未触发 timer 泄漏。

### Channel 策略

| Channel | 容量 | 说明 |
|---------|------|------|
| `stop` | 缓冲 1 | `Stop` 永不阻塞；重复 Stop 走 default |
| `add` / `remove` | 无缓冲 | 背压：发送方等到 `run` 处理 |
| `runDone` | 关闭即广播 | 循环退出时关闭；与发送并列在 `select` 中 |

禁止 `select { case ch <- v: default: }` 在“发不出去”时直改 `entries`：`run` 在排序/建 timer 时也会走 default，造成数据竞争。应阻塞发送，或在 `runDone` 已关闭后再加锁修改。

### Shutdown 流程

约束：

1. `Stop()` 立即返回
2. 已启动的 Job 必须跑完
3. `jobWaiter.Wait()` 须在不再有新的 `WaitGroup.Go` 之后（Go 1.26+）
4. 调用方能通过 context 得知全部结束

```
Stop()
  ├─ stop <- （缓冲 1；不在此处清 running）
  └─ 返回 stopCtx

run() 收到 stop
  ├─ stopTimer → return
  └─ defer:
       1. recover（若有）
       2. close(runDone)     ← 解除 add/remove 阻塞
       3. running = false
       4. jobWaiter.Wait()   ← 等待全部 Job
       5. stopCancel()       ← <-ctx.Done() 解除
```

`Wait` 必须在 `stopCancel` 之前，否则调用方可能在 Job 未结束时就收到 Done。

---

## 工作流程

### 主循环时序

以 `Every(1s)` 为例：

```
Start() → go run()
  初始化 Next = now + 1s
  SortFunc → NewTimer(1s)

[1s] timer 触发
  startJob → WaitGroup.Go(j.Run)
  Prev/Next 更新 → 再排序 → 再等 1s

Stop() → stop
  run return → close(runDone) → Wait → cancel
<-ctx.Done()
```

### 调度计算

`Schedule.Next(now) time.Time`：

| 返回值 | 含义 |
|--------|------|
| 零值 | 不再调度（条目仍在列表，排序靠后） |
| `After(now)` | 正常等待 `Next - now` |
| `== now` 或更早 | 立即触发（依赖 `NewTimer` 对非正时长的行为） |

`now` 由调度器传入（通常为本次唤醒时间），实现方不要改用 `time.Now()`。

内置：`Every(d)` → `DelaySchedule`，`Next(t) = t + d`。

---

## 使用指南

### 快速开始

```go
package main

import (
    "fmt"
    "time"

    "github.com/notes-bin/cron"
)

func main() {
    c := cron.New()
    c.AddFunc(cron.Every(2*time.Second), func() {
        fmt.Println("tick:", time.Now())
    })
    c.Start()
    time.Sleep(10 * time.Second)
    <-c.Stop().Done()
}
```

### 固定间隔

```go
cron.Every(5 * time.Minute)
cron.Every(30 * time.Second)
```

### 自定义 Schedule

```go
type DailySchedule struct{ Hour, Minute int }

func (s *DailySchedule) Next(t time.Time) time.Time {
    next := time.Date(t.Year(), t.Month(), t.Day(), s.Hour, s.Minute, 0, 0, t.Location())
    if next.Before(t) {
        next = next.Add(24 * time.Hour)
    }
    return next
}
```

返回 `time.Time{}` 即一次性任务（执行后不再触发）。

### 自定义 Job

```go
type MyJob struct{ Name string }

func (j *MyJob) Run() { fmt.Println(j.Name) }

c.AddJob(cron.Every(5*time.Second), &MyJob{Name: "备份"})
```

### 动态增删

启动前后均可调用；运行中经 channel 投递，线程安全。

```go
id := c.AddFunc(cron.Every(time.Minute), func() { /* ... */ })
c.Remove(id)
```

### 日志与时区

```go
c := cron.New(
    cron.WithLogger(myLogger),
    cron.WithLocation(loc), // 如 Asia/Shanghai
)
```

默认无日志；panic 在 discardLogger 下仍会打到 stderr。

### 优雅退出

```go
<-c.Stop().Done() // 停调度并等待全部 Job
```

| 用法 | 说明 |
|------|------|
| `c.Stop()` | 只发停止信号，立即返回 context |
| `<-c.Stop().Done()` | 等到全部 Job 结束 |

勿用 `defer c.Stop()` 代替等待 Done：函数返回时 Job 可能仍在跑，易触发竞态。

---

## 开发指南

### 要求

- Go **1.26+**（`go.mod` 中 `go 1.26`）
- 零第三方依赖

### 项目结构

```
cron/
├── cron.go          # Cron、run、增删启停、WaitGroup.Go
├── doc.go           # 包文档
├── logger.go        # Logger / discardLogger
├── options.go       # WithLocation / WithLogger
├── schedule.go      # DelaySchedule / Every
├── cron_test.go     # 生命周期、并发、排序
├── schedule_test.go # Schedule 行为
├── example_test.go  # Example（文档即测试）
└── README.md
```

单包 `package cron`，无子目录。

### 测试

```bash
go test ./...
go test -race ./...
go test -run Example -v
```

| 文件 | 覆盖 |
|------|------|
| `cron_test.go` | 增删、启停、执行、并发 Add/Remove、Stop 窗口、`entryByNext` |
| `schedule_test.go` | 间隔 / 立即 / 每日示例 Schedule |
| `example_test.go` | 基础、并发 Job、自定义 Job/Schedule |

Example 须 `<-c.Stop().Done()`；并发输出用 `Mutex` + `Builder`。

### 性能

| 操作 | 复杂度 |
|------|--------|
| 每轮排序 | `slices.SortFunc` → O(N log N) |
| 删除 | `DeleteFunc` → O(N) |
| 取最近任务 | 排序后 O(1) |

典型 N≪1000 足够。大规模可考虑最小堆 + ID 索引（见 TODO）。

---

## API 参考

| 符号 | 说明 |
|------|------|
| `New(...Option) *Cron` | 构造；可选 `WithLocation`、`WithLogger` |
| `AddFunc` / `AddJob` | 注册任务，返回 `EntryID` |
| `Remove` | 按 ID 删除 |
| `Start` / `Run` | 后台 / 阻塞启动 |
| `Stop() context.Context` | 停止；Done 表示 Job 全部结束 |
| `Location()` | 当前时区 |
| `Every(d)` | 固定间隔 Schedule |
| `Job` / `Schedule` / `Logger` | 扩展接口 |

```go
type Job interface{ Run() }
type Schedule interface{ Next(time.Time) time.Time }
type Logger interface {
    Info(msg string, keysAndValues ...any)
    Error(msg string, keysAndValues ...any)
}
```

---

## TODO

### 短期

- [ ] `container/heap`：将每轮 O(N log N) 降为 O(log N)
- [ ] `Entries()`：线程安全的只读快照
- [ ] 明确处理 `Next.Sub(now) < 0`（立即触发）

### 中期

- [ ] `Once` / `At` 一次性 API
- [ ] Cron 表达式 Schedule
- [ ] 执行统计、Pause/Resume
- [ ] `Job.Run(context.Context)`

### 长期

- [ ] 分布式锁、持久化、任务依赖、动态配置、OpenTelemetry

---

## License

MIT
