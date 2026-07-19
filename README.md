# cron

一个轻量级、高可靠的 Go 定时任务调度库。

```
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
  - [主循环时序图](#主循环时序图)
  - [调度计算逻辑](#调度计算逻辑)
- [使用指南](#使用指南)
  - [快速开始](#快速开始)
  - [固定间隔调度](#固定间隔调度)
  - [自定义 Schedule](#自定义-schedule)
  - [自定义 Job](#自定义-job)
  - [动态管理](#动态管理)
  - [日志](#日志)
  - [时区](#时区)
  - [优雅退出](#优雅退出)
- [开发指南](#开发指南)
  - [项目结构](#项目结构)
  - [测试策略](#测试策略)
  - [性能考虑](#性能考虑)
- [API 参考](#api-参考)
- [TODO](#todo)
- [License](#license)

---

## 设计原理

### 并发模型

```
┌───────────────┐    channel      ┌─────────────────────────────┐
│  External API  │  ──────────→    │  run() goroutine            │
│               │                 │                              │
│  AddJob ──────│── add ──────→   │  for {                      │
│  Remove ──────│── remove ───→   │    sort.Sort(entries)       │
│  Stop   ──────│── stop ─────→   │    timer = Next - now       │
│               │                 │    select {                 │
│  Start ───────│── go run()      │      ←timer.C: execute jobs │
└───────────────┘                 │      ←add:    append entry  │
                                  │      ←remove: delete entry  │
                                  │      ←stop:   return        │
                                  │    }                        │
                                  │  }                          │
                                  │                              │
                                  │  defer {                     │
                                  │    jobWaiter.Wait()         │
                                  │    stopCancel()              │
                                  │  }                           │
                                  └─────────────────────────────┘
```

**核心原则：所有状态变更运行在同一个 goroutine 中。**

这是本项目最关键的架构决策。外部 API（AddJob、Remove、Stop）通过 Go channel 向唯一的 `run()` goroutine 发送请求，由它串行处理所有事件。这种设计的优势：

1. **天然无竞态** — entries 切片只需要在一个 goroutine 中修改，不需要复杂的锁策略
2. **简化推理** — 任何时候 entries 的状态都是确定性的，不会被并发突变
3. **线程安全公开 API** — 外部调用者不需要持有锁或担心竞态条件

`runningMu` 是这个规则的唯一例外，它只保护 `running` 布尔值和 `nextID` 计数器，这两个字段需要在 channel 路由决策（走 channel 还是直接操作）和 ID 生成之间提供原子性。

### 事件驱动架构

`run()` 主循环基于 Go 的 `select` 多路复用机制，等待四类事件：

| 事件 | 来源 | 处理动作 |
|------|------|---------|
| `timer.C` | 定时器到期 | 触发到期任务，更新 Next，重新排序 |
| `add` channel | AddJob/AddFunc | 追加新 Entry，计算首次 Next |
| `remove` channel | Remove | 从 entries 中删除指定 ID |
| `stop` channel | Stop | 设置 running=false，等待 job 完成，取消 context |

**为什么选择 select 而不是轮询？**

- 零 CPU 占用 — 没有事件时 goroutine 休眠，不消耗 CPU
- 即时响应 — 新事件立即被处理，无需等待下一个轮询周期
- 自然排队 — 多个事件同时到达时，select 随机选择一个执行，其余的保持排队

### Channel 策略

不同 channel 使用不同的缓冲策略，基于各自的使用场景：

| Channel | 容量 | 原因 |
|---------|------|------|
| `stop` | **缓冲 1** | Stop() 必须永不阻塞，且 stop 信号不能丢失。缓冲 1 确保无论 run() 是否在 select 中，信号都能可靠送达 |
| `add` | **无缓冲** | AddJob 等待 run() 确认处理，形成自然背压，限制无限制的任务添加速率 |
| `remove` | **无缓冲** | 同上，Remove 等待确认删除 |

当 run() 因 panic 退出后，add/remove channel 会失去消费者。此时 AddJob/Remove 通过 `select { case ch <- v: default: }` 回退到直接操作 entries，避免永久阻塞。

### Shutdown 流程

Shutdown 是系统设计中最微妙的部分，必须保证以下约束：

1. Stop() 必须立即返回，不能阻塞等待 job 完成
2. 所有已启动的 job 必须完成执行
3. `jobWaiter.Wait()` 必须在 `jobWaiter.Add()` 不再发生后调用（Go 1.26+ 不允许 Wait 与 Add 并发）
4. 调用者最终必须能被通知到所有 job 已完成

当前的 shutdown 流程：

```
Stop()
  ├─ 发送 stop 信号到缓冲 channel（立即返回）
  └─ 返回 stopCtx

run() 收到 stop 信号
  ├─ 从 select/return 退出
  └─ defer 按序执行：
     1. panic 恢复（若有）
     2. jobWaiter.Wait()  ← 此时再无 Add()，安全
     3. stopCancel()      ← 通知 Stop() 的调用者
```

**关键设计决定：Wait → Cancel 的顺序。**

`jobWaiter.Wait()` 必须在 `stopCancel()` 之前执行。如果顺序颠倒，调用者会收到 `<-ctx.Done()` 通知时 job 可能仍在运行。这个顺序保证了"先完成所有工作，再通知完成"。

**为什么不在 `Stop()` 中直接等待？**

`Stop()` 返回的是一个 `context.Context`，调用者通过 `<-ctx.Done()` 等待。把 `Wait()` 放在 run() 的 defer 中意味着：
- Stop() 立即返回（不阻塞主流程）
- 等待发生在后台（run() goroutine 清理阶段）
- 调用者在需要同步时显式 `<-ctx.Done()`

---

## 工作流程

### 主循环时序图

以 Every(1s) 的任务为例：

```
时间线
│
├─ Start() → go run()
│
│  run() 执行:
│    now = time.Now()
│    entry.Next = now + 1s
│    sort entries
│    timer = time.NewTimer(1s)     ← 等待 1 秒
│
├─ [1s 后] timer fires
│
│    now = timer.C (≈ now + 1s)
│    startJob(entry.Job)            ← 启动 goroutine
│    entry.Prev = entry.Next
│    entry.Next = now + 1s          ← 计算下次
│    sort entries
│    timer = time.NewTimer(1s)      ← 等待下一轮
│
├─ [同时] Job goroutine 运行
│    j.Run() 执行业务逻辑
│    ... (可能耗时)
│    jobWaiter.Done()
│
├─ [1s 后] timer fires again
│    ...重复...
│
│  Stop()
│    stop ← struct{}{}
│
│  run() select 收到 stop
│    return
│
├─ defer:
│    jobWaiter.Wait()  ← 等待所有 Job goroutine
│    stopCancel()      ← 通知 Stop() 的调用者
│
└─ <-ctx.Done() 解除阻塞
```

### 调度计算逻辑

Schedule 接口的核心方法是 `Next(now time.Time) time.Time`：

```
输入: now = 调度器当前时间（通常是触发时间）
输出: next = 下次执行时间

规则:
  next.IsZero() → Entry 不再调度
  next.After(now) → 正常调度，定时器等待 next - now
  next == now 或 next.Before(now) → 立即触发

场景:
  Every(d):   Next(now) = now + d     → 固定间隔
  Immediate:  Next(now) = now          → 每次触发都立即重触发（持续循环）
  Once:       Next(now) = zero Time    → 执行一次后停止
```

**重要设计决策**：`Next` 的参数 `now` 由调度器传入，代表"本应触发的时间"。Job 的实现应基于此参数而非 `time.Now()` 计算，否则在多事件并发或调度器延迟时会产生偏差。

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

    // 每 2 秒执行一次
    c.AddFunc(cron.Every(2*time.Second), func() {
        fmt.Println("tick:", time.Now())
    })

    c.Start()
    time.Sleep(10 * time.Second)

    ctx := c.Stop()
    <-ctx.Done() // 等待正在执行的 job 完成
}
```

### 固定间隔调度

```go
cron.Every(5 * time.Minute)   // 每 5 分钟
cron.Every(1 * time.Hour)     // 每小时
cron.Every(30 * time.Second)  // 每 30 秒
```

`Every` 使用 `DelaySchedule`，它的 `Next(t) = t + delay`。这意味着：
- Job 执行时间不影响间隔 — 从"应该触发的时间"开始计算
- 如果调度器阻塞，`t` 是阻塞后的当前时间，所以间隔从阻塞后开始计算

### 自定义 Schedule

```go
// 每天 9:00 执行
type DailySchedule struct {
    Hour, Minute int
}

func (s *DailySchedule) Next(t time.Time) time.Time {
    next := time.Date(t.Year(), t.Month(), t.Day(), s.Hour, s.Minute, 0, 0, t.Location())
    if next.Before(t) {
        next = next.Add(24 * time.Hour) // 今天已过，推到明天
    }
    return next
}

c.AddFunc(&DailySchedule{Hour: 9, Minute: 0}, func() {
    fmt.Println("早上好！")
})
```

`Next` 返回零值 `time.Time{}` 表示"不再调度"，Entry 变为一次性任务。

### 自定义 Job

```go
type MyJob struct {
    Name string
}

func (j *MyJob) Run() {
    fmt.Println("任务执行:", j.Name)
}

c.AddJob(cron.Every(5*time.Second), &MyJob{Name: "数据备份"})
```

`Job` 接口的优势：
- 可以携带状态（计数器、配置等）
- 可以通过结构体字段传递依赖
- 可以通过方法封装复杂的业务逻辑

### 动态管理

任务可以在调度器运行中安全地添加和删除：

```go
id := c.AddFunc(cron.Every(time.Minute), func() {
    // 定时执行
})

// 在另一个 goroutine 或条件满足时
if condition {
    c.Remove(id) // 停止这个任务
}
```

**注意**：AddFunc/AddJob/Remove 在调度器启动前和启动后都能调用。

### 日志

默认不输出任何日志。通过 `WithLogger` 注入自定义实现：

```go
type myLogger struct{}

func (l *myLogger) Info(msg string, kv ...any)  { log.Printf("INFO: %s %v", msg, kv) }
func (l *myLogger) Error(msg string, kv ...any) { log.Printf("ERROR: %s %v", msg, kv) }

c := cron.New(cron.WithLogger(&myLogger{}))
```

Job 和调度器内部的 panic 会被捕获并通过 Logger.Error 报告。如果使用默认的 discardLogger，panic 信息会输出到 stderr 作为兜底。

### 时区

```go
loc, _ := time.LoadLocation("Asia/Shanghai")
c := cron.New(cron.WithLocation(loc))
```

所有时间计算（Next、now、日志时间戳）都基于设置的时区。

### 优雅退出

```go
func main() {
    c := cron.New()
    // ... 添加任务 ...

    c.Start()

    // 等待中断信号
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    <-sig

    // 优雅关闭：停止调度器，等待所有 Job 完成
    fmt.Println("正在关闭调度器，等待任务完成...")
    <-c.Stop().Done()
    fmt.Println("所有任务已完成，退出。")
}
```

**传入/传出 context 的对比：**

| 方法 | 说明 |
|------|------|
| `c.Stop()` | 停止调度，返回 context |
| `<-c.Stop().Done()` | 停止并等待所有 Job 完成（阻塞） |
| `ctx, cancel := c.Stop(), context.WithTimeout(c.Stop(), 5*time.Second)` | 最多等待 5 秒 |

---

## 开发指南

### 项目结构

```
cron/
├── cron.go            # 核心调度器：Cron 结构体、run() 主循环、entry 管理
├── doc.go             # 包文档和架构概述
├── logger.go          # Logger 接口 + discardLogger 默认实现
├── options.go         # Option 模式：WithLocation、WithLogger
├── schedule.go        # DelaySchedule 和 Every() 工厂函数
├── cron_test.go       # 单元测试：初始化、增删、启动停止、任务执行
├── schedule_test.go   # Schedule 接口的测试和测试用 Schedule 实现
├── example_test.go    # Example 测试（文档即测试）
└── README.md          # 设计文档和使用指南
```

所有源码在同一个 Go 包 (`package cron`) 中，无需子包。这种设计保证：
- 使用方只需 `import "github.com/notes-bin/cron"`
- 内部类型可以互相访问，不需要跨包导出
- 降低使用复杂度

### 测试策略

| 测试类型 | 文件 | 覆盖内容 |
|---------|------|---------|
| 单元测试 | `cron_test.go` | 调度器生命周期、任务增删、启动停止、Job 执行验证 |
| Example 测试 | `example_test.go` | 用户可读的示例代码，同时作为测试运行 |
| Schedule 测试 | `schedule_test.go` | 调度算法的正确性验证 |

**运行测试：**

```bash
go test -v ./...           # 基础测试
go test -race ./...        # 竞态检测
go test -run Example -v    # Example 测试
```

**编写 Example 测试的注意事项：**

1. 必须使用 `<-c.Stop().Done()` 等待优雅退出（替代 `defer c.Stop()`）
2. 不要用 `defer c.Stop()` — 它在函数返回后执行，此时 goroutine 可能仍在运行，导致数据竞争
3. 并发输出的示例应使用 `sync.Mutex` + `strings.Builder` 避免输出竞态
4. Output 注释必须与 stdout 严格匹配

### 性能考虑

**当前架构的性能边界：**

- 单 goroutine 事件循环 → 每个事件 O(1) 处理
- 排序 O(N log N) → 每次事件循环都对所有 entry 排序
- 线性删除 O(N) → removeEntry 遍历整个 entries

**优化方向（待实现）：**

- 使用 `container/heap` 替代 `sort.Sort`，降到每次 O(log N)
- 使用 map 索引 EntryID 实现 O(1) 删除

---

## API 参考

| 方法/函数 | 类型 | 说明 |
|-----------|------|------|
| `New(opts ...Option)` | 构造函数 | 创建 Cron 实例。可选 WithLocation、WithLogger |
| `(*Cron) AddFunc(schedule, func()) EntryID` | 方法 | 添加函数作为定时任务 |
| `(*Cron) AddJob(schedule, Job) EntryID` | 方法 | 添加 Job 实现作为定时任务 |
| `(*Cron) Remove(id EntryID)` | 方法 | 按 ID 删除任务 |
| `(*Cron) Start()` | 方法 | 后台 goroutine 启动调度器 |
| `(*Cron) Run()` | 方法 | 阻塞当前 goroutine 启动 |
| `(*Cron) Stop() context.Context` | 方法 | 停止调度，返回等待 Job 完成的 Context |
| `(*Cron) Location() *time.Location` | 方法 | 返回配置的时区 |
| `Every(delay) DelaySchedule` | 函数 | 创建固定间隔 Schedule |
| `WithLocation(loc) Option` | 函数 | 设置时区选项 |
| `WithLogger(logger) Option` | 函数 | 设置日志选项 |

### 接口

```go
type Job interface {
    Run()
}

type Schedule interface {
    Next(time.Time) time.Time
}

type Logger interface {
    Info(msg string, keysAndValues ...any)
    Error(msg string, keysAndValues ...any)
}
```

---

## TODO

以下特性按优先级排列：

### 短期（下一个 Minor 版本）

- [ ] **`container/heap` 优化**：用最小堆替代固定排序，将每次事件循环的 O(N log N) 降到 O(log N)。删除操作仍需 O(N)，可用 map + 延迟删除优化
- [ ] **`Entries()` 只读方法**：提供线程安全的 entry 查询接口，用 `runningMu.RLock` 保护（不增加 entriesMu）
- [ ] **负数 timer duration 保护**：当 `Next.Sub(now) < 0` 时，明确处理为立即触发而非依赖 Go 内部行为

### 中期（下一个 Minor 版本）

- [ ] **一次性任务**：提供 `Once(schedule)` 或 `At(time)` 快捷方法创建只执行一次的任务
- [ ] **Cron 表达式支持**：添加 `CronSchedule`，支持标准 5 位或 6 位 cron 表达式
- [ ] **任务统计**：每个 Entry 记录执行次数、最后执行时间、累计执行时长
- [ ] **任务暂停/恢复**：`Pause(id)` / `Resume(id)` 不删除 entry 但暂停触发
- [ ] **上下文传递**：Job.Run 接收 context.Context，支持超时取消

### 长期（Major 版本）

- [ ] **分布式锁**：多个实例运行时通过外部存储（Redis/etcd）防止重复执行
- [ ] **持久化任务**：支持将任务状态持久化，重启后恢复
- [ ] **任务依赖**：Job A 完成后触发 Job B
- [ ] **动态配置**：运行时通过配置中心更新任务参数
- [ ] **链路追踪**：集成 OpenTelemetry，每个 Job 执行生成 span

---

## License

MIT
