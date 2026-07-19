package cron

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// ┌─────────────────────────────────────────────────────────┐
// │  Cron 调度器核心结构                                      │
// │  并发模型: 单一调度 goroutine + channel 事件序列化         │
// └─────────────────────────────────────────────────────────┘

// Cron 是定时任务调度器，围绕一个中心事件循环（run()）构建。
//
// 并发设计：
//   - 所有 entries 的读写操作最终都由 run() goroutine 串行处理。
//   - 外部 API（AddJob/Remove/Stop）通过 channel 向 run() 发送事件；
//     当调度器未启动时，它们直接在调用者 goroutine 中操作 entries。
//   - runningMu 保护 running、nextID，以及在 !running 时 entries 的直接读写。
//
// Shutdown 流程（stop channel 缓冲 1，确保 Stop() 永不阻塞）:
//
//	Stop() → stop 信号 → run() 退出 → 在 defer 中 Wait() 所有 job → cancel context
//
// 这确保了 Wait() 只在 Add() 不再发生后执行，避免了 WaitGroup 的并发重用问题。
type Cron struct {
	entries   []*Entry       // 所有注册的定时任务（仅 run() goroutine 或 runningMu 保护下访问）
	stop      chan struct{}   // stop 信号（缓冲 1，Stop() 直接发送不阻塞）
	add       chan *Entry     // add 信号（无缓冲，提供背压；run() 退出时 select+default 兜底）
	remove    chan EntryID    // remove 信号（同上）
	running   bool            // 调度器是否运行中（runningMu 保护）
	runningMu sync.Mutex      // 保护 running、nextID；!running 时也保护 entries 直接读写
	location  *time.Location  // 任务触发的时间基准
	nextID    EntryID         // 自增任务 ID（runningMu 保护）
	jobWaiter sync.WaitGroup  // 跟踪所有已启动的 job goroutine；run() 退出前 Wait()
	logger    Logger          // 日志输出（默认 discardLogger）
	stopCtx   context.Context // Stop() 返回的 context，run() 退出时 cancel
	stopCancel context.CancelFunc
}

// ┌─────────────────────────────────────────────────────────┐
// │  核心接口定义                                            │
// └─────────────────────────────────────────────────────────┘

// Job 是需要定时执行的业务逻辑。
//
// Run 在调度器 goroutine 中由 startJob 启动一个独立 goroutine 执行。
// Run 应尽快返回；若需执行长时间操作，应在内部自行管理 goroutine。
// Run 中的 panic 会被 startJob 捕获并通过 Logger 报告，不影响调度器运行。
type Job interface {
	Run()
}

// Schedule 决定 Job 何时触发。
//
// Next 接收当前调度时间 now，返回下一次执行时间。
// 若返回零值 time.Time{}，表示不会再次调度，Entry 变为一次性任务。
// 【重要】Next 应当基于 now 参数计算，而非 time.Now()，
// 因为调度器可能在 now 之后才调用 Next，场景如下：
//   - 调度器被阻塞（如 Stop() 等待中）
//   - 前一个触发被 Job 本身阻塞
//   - 系统时间调整
// 基于 now 计算能保证调度相对于"本应触发的时间"是正确的。
type Schedule interface {
	Next(time.Time) time.Time
}

// EntryID 是每个定时任务的唯一标识，由 nextID 自增生成。
type EntryID int

// Entry 代表调度器中的一个任务条目。
//
// 生命周期:
//   AddFunc/AddJob → 加入 entries → 排序 → Next 到达 → startJob → 更新 Next → 重新排序
//   Remove → 从 entries 中移除
//   Job 返回零值 Next → Entry 保留在 entries 中但不会再被触发
type Entry struct {
	ID       EntryID
	Schedule Schedule       // 调度策略
	Next     time.Time      // 下次执行时间；零值表示已不再调度
	Prev     time.Time      // 上次执行时间（零值表示尚未执行过）
	Job      Job
}

// byTime 实现 sort.Interface，按 Next 升序排列 entries。
// 零值 Next 视为"永不触发"，排在最后。
// 排序后 c.entries[0] 就是下个触发的任务。
type byTime []*Entry

func (s byTime) Len() int      { return len(s) }
func (s byTime) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byTime) Less(i, j int) bool {
	// 零值排在最后（永不触发）
	if s[i].Next.IsZero() {
		return false
	}
	if s[j].Next.IsZero() {
		return true
	}
	return s[i].Next.Before(s[j].Next)
}

// ┌─────────────────────────────────────────────────────────┐
// │  构造与选项                                              │
// └─────────────────────────────────────────────────────────┘

// New 创建 Cron 实例。
//
// 默认行为：
//   - 使用本地时区 (time.Local)
//   - 不输出日志（discardLogger）
//   - stop channel 缓冲 1（Stop() 从不阻塞）
//   - add / remove channel 无缓冲（背压机制）
//   - stopCtx 提前创建，run() 退出时 cancel
//
// 通过 Option 函数选项模式注入自定义配置。
func New(opts ...Option) *Cron {
	c := &Cron{
		stop:     make(chan struct{}, 1),  // 缓冲 1：Stop() 不阻塞，run() 总会读到
		add:      make(chan *Entry),       // 无缓冲：AddJob 等待 run() 处理，形成自然背压
		remove:   make(chan EntryID),      // 同上
		location: time.Local,
		logger:   &discardLogger{},
	}
	// context 提前创建；cancel 由 run() 的 defer 在退出时调用。
	// 这样 Stop() 始终返回有效 context，不用考虑首次/后续调用的差异。
	c.stopCtx, c.stopCancel = context.WithCancel(context.Background())

	for _, opt := range opts {
		if err := opt(c); err != nil {
			panic(err)
		}
	}
	return c
}

// ┌─────────────────────────────────────────────────────────┐
// │  FuncJob — 函数适配器                                    │
// └─────────────────────────────────────────────────────────┘

// FuncJob 将 func() 适配为 Job 接口，方便使用匿名函数或闭包作为任务。
type FuncJob func()

func (f FuncJob) Run() { f() }

// ┌─────────────────────────────────────────────────────────┐
// │  添加任务                                                │
// └─────────────────────────────────────────────────────────┘

// AddFunc 添加函数作为定时任务，返回唯一 EntryID。
// 底层调用 AddJob，将 func() 包装为 FuncJob。
func (c *Cron) AddFunc(schedule Schedule, cmd func()) EntryID {
	return c.AddJob(schedule, FuncJob(cmd))
}

// AddJob 添加 Job 到调度器。
//
// schedule 和 cmd 都不能为 nil（panic 以快速暴露调用方错误）。
//
// 根据 running 状态选择路径：
//   - 未启动：直接在调用者 goroutine 中追加到 entries（runningMu 保护）
//   - 已启动：通过 add channel 发送给 run() goroutine 处理
//     若 run() 已退出（select default 分支），回退到直接追加
//
// 返回自增的 EntryID，从 1 开始（零值表示无效 ID）。
func (c *Cron) AddJob(schedule Schedule, cmd Job) EntryID {
	if schedule == nil {
		panic("cron: schedule cannot be nil")
	}
	if cmd == nil {
		panic("cron: job cannot be nil")
	}

	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	c.nextID++
	entry := &Entry{
		ID:       c.nextID,
		Schedule: schedule,
		Job:      cmd,
	}
	if !c.running {
		// 未启动：直接追加
		c.entries = append(c.entries, entry)
	} else {
		// 已启动：通过 channel 发送给 run() goroutine
		select {
		case c.add <- entry:
		default:
			// run() 已退出（panic 后），回退到直接追加
			c.entries = append(c.entries, entry)
		}
	}
	return entry.ID
}

// Location 返回调度器配置的时区。
func (c *Cron) Location() *time.Location { return c.location }

// ┌─────────────────────────────────────────────────────────┐
// │  删除任务                                                │
// └─────────────────────────────────────────────────────────┘

// Remove 从调度器中删除指定 ID 的任务。
//
// 与 AddJob 同理，根据 running 状态选择路径：
//   - 未启动：直接调用 removeEntry（runningMu 保护）
//   - 已启动：通过 remove channel 发送，
//     若 run() 已退出则直接调用 removeEntry
func (c *Cron) Remove(id EntryID) {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		select {
		case c.remove <- id:
		default:
			c.removeEntry(id)
		}
	} else {
		c.removeEntry(id)
	}
}

// ┌─────────────────────────────────────────────────────────┐
// │  启动 / 停止                                             │
// └─────────────────────────────────────────────────────────┘

// Start 在后台 goroutine 启动调度器。
// 如果调度器已在运行，Start 是安全的空操作（no-op）。
func (c *Cron) Start() {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		return
	}
	c.running = true
	go c.run()
}

// Run 启动调度器并阻塞当前 goroutine。
// 与 Start 的区别：Run 直接调用 run() 而不是 go run()。
// 当 run() 返回时（收到 stop 信号），Run 才返回。
//
// 典型用法：
//
//	func main() {
//		c := cron.New()
//		c.AddFunc(...)
//		c.Run()  // 阻塞直到 Stop()
//	}
func (c *Cron) Run() {
	c.runningMu.Lock()
	if c.running {
		c.runningMu.Unlock()
		return
	}
	c.running = true
	c.runningMu.Unlock()
	c.run()
}

// ┌─────────────────────────────────────────────────────────┐
// │  主调度循环                                              │
// │  设计要点:                                                │
// │  - 单 goroutine 串行处理所有事件（定时器、add、remove、stop）│
// │  - 外层 for 循环：排序 → 创建定时器 → select → 回到排序    │
// │  - 每次 select 后都回到排序，保证下次定时器基于最新状态      │
// │  - nil channel 替代 100000h sentinel timer：零运行时开销    │
// └─────────────────────────────────────────────────────────┘

// run 是调度器主循环，应在独立 goroutine 中运行。
//
// 进入流程：
//  1. 对所有 entry 执行初始调度计算（设置 Next）
//  2. 进入事件循环直到收到 stop 信号
//
// 退出流程（defer）:
//  1. 若 panic：记录日志、恢复到 safe state
//  2. 等待所有已启动的 job goroutine 完成 (jobWaiter.Wait)
//  3. 取消 Stop() 返回的 context (stopCancel)
//     注意：Wait → Cancel 的顺序是关键的——cancel 后调用者才会收到通知，
//     所以必须在 cancel 前确保所有 job 已完成。
func (c *Cron) run() {
	defer func() {
		if r := recover(); r != nil {
			c.logPanic("run", r)
			c.runningMu.Lock()
			c.running = false
			c.runningMu.Unlock()
		}
		// 等待所有 job goroutine 完成再通知 Stop()，
		// 避免 WaitGroup.Add 与 Wait 并发（Go 1.26+ 严格检测）。
		c.jobWaiter.Wait()
		c.stopCancel()
	}()

	// — 初始调度 —
	// 对已注册的 entry 计算首次触发时间。
	// 注意：此时所有 entry 的 Next 都是零值，
	// 经过此循环后每个 entry 获得有效的 Next 值。
	now := c.now()
	for _, entry := range c.entries {
		entry.Next = entry.Schedule.Next(now)
		c.logger.Info("schedule", "now", now, "entry", entry.ID, "next", entry.Next)
	}

	// — 主事件循环 —
	// 每轮迭代：
	//   1. 排序 entries（最近触发排最前）
	//   2. 创建定时器等待第一个 entry 触发
	//   3. select 等待四个事件之一
	//   4. 处理事件后回到步骤 1
	for {
		sort.Sort(byTime(c.entries))

		// 使用 nil channel 替代 time.NewTimer(100000 * time.Hour)。
		// 在 Go 中，nil channel 在 select 中永远阻塞，不消耗任何资源。
		// 而有任务的真实定时器每次循环创建，用后丢弃（Go 1.23+ GC 可回收）。
		var timerCh <-chan time.Time
		if len(c.entries) == 0 || c.entries[0].Next.IsZero() {
			timerCh = nil
		} else {
			// now 在每次 select 后更新（来自 timer.C、c.now() 或 add/remove 刷新），
			// 因此 timer 的时长始终基于最新时间戳，不会累积漂移。
			timerCh = time.NewTimer(c.entries[0].Next.Sub(now)).C
		}

		select {
		// ── 定时触发 ──
		case now = <-timerCh:
			now = now.In(c.location)
			c.logger.Info("wake", "now", now)

			// 遍历 entries（已排序），触发所有已到期的任务。
			// 因为按 Next 升序排列，遇到第一个未到期或零值的就可停止。
			for _, e := range c.entries {
				if e.Next.After(now) || e.Next.IsZero() {
					break
				}
				c.startJob(e.Job)   // 启动独立 goroutine 执行
				e.Prev = e.Next      // 记录本次触发时间
				e.Next = e.Schedule.Next(now)  // 计算下次触发
				c.logger.Info("run", "now", now, "entry", e.ID, "next", e.Next)
			}

		// ── 新增任务 ──
		case newEntry := <-c.add:
			now = c.now()
			newEntry.Next = newEntry.Schedule.Next(now)
			c.entries = append(c.entries, newEntry)
			c.logger.Info("added", "now", now, "entry", newEntry.ID, "next", newEntry.Next)

		// ── 停止信号 ──
		// Stop() 发送后立即返回，不阻塞等待 job 完成；
		// 等待逻辑在 defer 中（jobWaiter.Wait → stopCancel）。
		case <-c.stop:
			c.logger.Info("stop")
			return

		// ── 删除任务 ──
		case id := <-c.remove:
			now = c.now()
			c.removeEntry(id)
			c.logger.Info("removed", "entry", id)
		}
	}
}

// ┌─────────────────────────────────────────────────────────┐
// │  Job 执行与安全                                          │
// └─────────────────────────────────────────────────────────┘

// startJob 在独立 goroutine 中执行 Job，捕获 panic 防止调度器崩溃。
//
// 每个 job 在独立的 goroutine 中运行，确保：
//   - 长时间运行的 job 不阻塞调度器
//   - 一个 job 的 panic 不影响其他 job
//   - 调度器可以实时响应 add/remove/stop 事件
//
// jobWaiter 跟踪所有活跃的 job goroutine，
// 用于 Stop() 的优雅退出（等待所有 job 完成后再取消 context）。
func (c *Cron) startJob(j Job) {
	c.jobWaiter.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.logPanic("job", r)
			}
			c.jobWaiter.Done()
		}()
		j.Run()
	}()
}

// logPanic 统一处理 panic 记录的日志和 stderr 输出。
//
// 双输出设计：
//   - Logger.Error：通过注入的 Logger 处理（可对接文件、JSON、云监控等）
//   - fmt.Fprintf(stderr)：兜底输出，当 Logger 为 discardLogger（默认）时生效
//     这样即使用户没有配置 Logger，panic 也不会完全被静默丢弃。
//
// kind 参数区分来源（"run" / "job"），便于日志查询时区分调度器 panic 和任务 panic。
func (c *Cron) logPanic(kind string, r any) {
	c.logger.Error("cron "+kind+" panic recovered", "error", r)
	if _, ok := c.logger.(*discardLogger); ok {
		fmt.Fprintf(os.Stderr, "cron: %s panic recovered: %v\n", kind, r)
	}
}

// now 返回调度器时区下的当前时间。
// 所有时间相关操作（调度计算、日志等）使用此方法以确保时区一致性。
func (c *Cron) now() time.Time { return time.Now().In(c.location) }

// Stop 停止调度器并返回一个 context，在所有正在执行的 job 完成后被取消。
//
// 设计要点：
//   - stop channel 缓冲 1，因此直接发送永不阻塞
//   - 设置 running = false 后，未来的 AddJob/Remove 不会尝试 channel 发送
//   - 返回的 context 在 run() 退出并等待所有 job 完成后被 cancel
//   - 多次调用返回同一个 context，且幂等
//
// 调用者可通过 <-ctx.Done() 等待优雅退出：
//
//	ctx := c.Stop()
//	<-ctx.Done()  // 等待所有 job 完成
func (c *Cron) Stop() context.Context {
	c.runningMu.Lock()
	if c.running {
		c.stop <- struct{}{} // 缓冲 1，永不阻塞
		c.running = false
	}
	c.runningMu.Unlock()
	return c.stopCtx
}

// removeEntry 从 entries 中删除指定 ID 的 entry。
//
// 算法：遍历 + append 重建切片（O(n)）。
// 对于定时任务调度器，entry 数量通常 < 1000，线性删除足够高效。
// 如果 entry 不存在，遍历不会产生任何写入，操作安全。
func (c *Cron) removeEntry(id EntryID) {
	var entries []*Entry
	for _, e := range c.entries {
		if e.ID != id {
			entries = append(entries, e)
		}
	}
	c.entries = entries
}
