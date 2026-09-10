package cron

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"
)

// Cron 是定时任务调度器，由单一 run() 事件循环驱动。
//
// 并发约定：
//   - 运行中：仅 run() 读写 entries；外部经 add/remove channel 投递变更。
//   - 未运行或 run 已退出：AddJob/Remove 在 runningMu 下直接改 entries。
//   - runningMu 还保护 running、nextID。
//   - runDone 在事件循环退出时关闭，解除仍阻塞在 add/remove 上的发送方。
//
// 关闭路径（stop 缓冲 1，Stop 不阻塞）：
//
//	Stop → stop → run 退出 → close(runDone) → running=false
//	→ jobWaiter.Wait → stopCancel
//
// running 只在 run() defer 中置 false，避免与仍在执行的 run 并发改表。
type Cron struct {
	entries    []*Entry       // 任务表（运行中仅 run 访问；否则需 runningMu）
	stop       chan struct{}  // 停止信号（缓冲 1）
	add        chan *Entry    // 新增（无缓冲，背压）
	remove     chan EntryID   // 删除（无缓冲，背压）
	runDone    chan struct{}  // 事件循环结束时关闭
	running    bool           // 是否在跑（仅 run defer 清 false）
	runningMu  sync.Mutex     // 保护 running、nextID 与非 run 路径的 entries
	location   *time.Location // 调度时区
	nextID     EntryID        // 自增 ID（从 1 起）
	jobWaiter  sync.WaitGroup // 跟踪 Job goroutine；退出前 Wait
	logger     Logger         // 默认 discardLogger
	stopCtx    context.Context
	stopCancel context.CancelFunc
}

// Job 是定时执行的业务逻辑。
//
// 由 startJob 通过 WaitGroup.Go 在独立 goroutine 中调用 Run。
// Run 应尽快返回；长耗时工作请自行开 goroutine。
// Run 内 panic 会被捕获并记日志，不影响调度器与其它 Job。
type Job interface {
	Run()
}

// Schedule 决定下次触发时间。
//
// Next(now) 必须基于参数 now 计算，不要调用 time.Now()。
// 返回零值 time.Time{} 表示不再调度。
// 调度器可能在“应触发时刻”之后才调用 Next（阻塞、时钟调整等），
// 基于 now 可保持相对该基准的正确间隔。
type Schedule interface {
	Next(time.Time) time.Time
}

// EntryID 是任务唯一标识，由 nextID 自增分配；零值表示无效 ID。
type EntryID int

// Entry 是调度器中的一条任务。
//
// 生命周期：Add → 排序 → Next 到期 → startJob → 更新 Next → 再排序；
// Remove 可随时删除；Next 为零则保留在列表中但不再触发。
type Entry struct {
	ID       EntryID
	Schedule Schedule  // 调度策略
	Next     time.Time // 下次触发；零值表示不再调度
	Prev     time.Time // 上次触发；零值表示尚未执行
	Job      Job
}

// entryByNext 供 slices.SortFunc 使用：按 Next 升序；零值 Next 排最后。
// 排序后 entries[0] 为下一次应触发的任务。
func entryByNext(a, b *Entry) int {
	if a.Next.IsZero() {
		if b.Next.IsZero() {
			return 0
		}
		return 1
	}
	if b.Next.IsZero() {
		return -1
	}
	return a.Next.Compare(b.Next)
}

// New 创建 Cron。
//
// 默认：time.Local、discardLogger、stop 缓冲 1、add/remove 无缓冲、
// runDone 未关闭、stopCtx 已创建（run 退出时 cancel）。
// 可通过 Option 覆盖；无效 Option 会使 New panic。
func New(opts ...Option) *Cron {
	c := &Cron{
		stop:     make(chan struct{}, 1),
		add:      make(chan *Entry),
		remove:   make(chan EntryID),
		runDone:  make(chan struct{}), // 仅关闭一次；不支持 Stop 后再 Start
		location: time.Local,
		logger:   discard,
	}
	c.stopCtx, c.stopCancel = context.WithCancel(context.Background())

	for _, opt := range opts {
		if err := opt(c); err != nil {
			panic(err)
		}
	}
	return c
}

// FuncJob 将 func() 适配为 Job。
type FuncJob func()

// Run 调用底层函数。
func (f FuncJob) Run() { f() }

// AddFunc 添加函数任务，等价于 AddJob(schedule, FuncJob(cmd))。
// cmd 为 nil 时 panic（避免 FuncJob(nil) 装入非 nil 的 Job 接口后延迟崩溃）。
func (c *Cron) AddFunc(schedule Schedule, cmd func()) EntryID {
	if cmd == nil {
		panic("cron: func cannot be nil")
	}
	return c.AddJob(schedule, FuncJob(cmd))
}

// AddJob 注册 Job，返回 EntryID（从 1 递增）。
//
// schedule 或 cmd 为 nil 时 panic。
//
//   - 未运行：在 runningMu 下 append 到 entries。
//   - 运行中：先解锁再发往 add；若 runDone 已关闭则再加锁 append。
//     不使用 select+default，以免 run 忙碌时与事件循环竞态。
func (c *Cron) AddJob(schedule Schedule, cmd Job) EntryID {
	if schedule == nil {
		panic("cron: schedule cannot be nil")
	}
	if cmd == nil {
		panic("cron: job cannot be nil")
	}

	c.runningMu.Lock()
	c.nextID++
	entry := &Entry{
		ID:       c.nextID,
		Schedule: schedule,
		Job:      cmd,
	}
	if !c.running {
		c.entries = append(c.entries, entry)
		c.runningMu.Unlock()
		return entry.ID
	}
	c.runningMu.Unlock()

	// 发送期间不持锁，避免与 run defer 抢 runningMu 死锁
	select {
	case c.add <- entry:
	case <-c.runDone:
		c.runningMu.Lock()
		c.entries = append(c.entries, entry)
		c.runningMu.Unlock()
	}
	return entry.ID
}

// Location 返回调度时区。
func (c *Cron) Location() *time.Location { return c.location }

// Remove 按 ID 删除任务；不存在则无操作。
//
//   - 未运行：加锁 removeEntry。
//   - 运行中：发往 remove；runDone 已关闭则加锁删除。
func (c *Cron) Remove(id EntryID) {
	c.runningMu.Lock()
	if !c.running {
		c.removeEntry(id)
		c.runningMu.Unlock()
		return
	}
	c.runningMu.Unlock()

	select {
	case c.remove <- id:
	case <-c.runDone:
		c.runningMu.Lock()
		c.removeEntry(id)
		c.runningMu.Unlock()
	}
}

// Start 在后台启动 run()；已在运行则为 no-op。
func (c *Cron) Start() {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		return
	}
	c.running = true
	go c.run()
}

// Run 在当前 goroutine 阻塞执行调度循环，直到 Stop。
// 已在运行则为 no-op。
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

// run 为调度主循环（由 Start 异步或 Run 同步调用）。
//
// 启动时为已有 entry 计算首次 Next，然后循环：
// 排序 → 建 timer（或 nil channel）→ select 处理事件。
//
// defer 顺序：recover → close(runDone) → running=false →
// jobWaiter.Wait → stopCancel。Wait 必须在 cancel 之前，
// 且须在不再有新的 WaitGroup.Go 之后（Go 1.26+）。
func (c *Cron) run() {
	defer func() {
		if r := recover(); r != nil {
			c.logPanic("run", r)
		}
		close(c.runDone)
		c.runningMu.Lock()
		c.running = false
		c.runningMu.Unlock()
		c.jobWaiter.Wait()
		c.stopCancel()
	}()

	now := c.now()
	for _, entry := range c.entries {
		entry.Next = entry.Schedule.Next(now)
		c.logger.Info("schedule", "now", now, "entry", entry.ID, "next", entry.Next)
	}

	for {
		slices.SortFunc(c.entries, entryByNext)

		// 无到期任务时用 nil channel，select 中永久阻塞且不占 timer
		var timer *time.Timer
		var timerCh <-chan time.Time
		if len(c.entries) == 0 || c.entries[0].Next.IsZero() {
			timerCh = nil
		} else {
			// 时长相对调度时钟 now；非正数视为立即触发
			d := max(c.entries[0].Next.Sub(now), 0)
			timer = time.NewTimer(d)
			timerCh = timer.C
		}

		select {
		case now = <-timerCh:
			now = now.In(c.location)
			c.logger.Info("wake", "now", now)

			// 已按 Next 升序；遇到未到期或零值即可停止扫描
			for _, e := range c.entries {
				if e.Next.After(now) || e.Next.IsZero() {
					break
				}
				c.startJob(e.Job)
				e.Prev = e.Next
				e.Next = e.Schedule.Next(now)
				c.logger.Info("run", "now", now, "entry", e.ID, "next", e.Next)
			}

		case newEntry := <-c.add:
			stopTimer(timer)
			now = c.now()
			newEntry.Next = newEntry.Schedule.Next(now)
			c.entries = append(c.entries, newEntry)
			c.logger.Info("added", "now", now, "entry", newEntry.ID, "next", newEntry.Next)

		case <-c.stop:
			stopTimer(timer)
			c.logger.Info("stop")
			return

		case id := <-c.remove:
			stopTimer(timer)
			now = c.now()
			c.removeEntry(id)
			c.logger.Info("removed", "entry", id)
		}
	}
}

// stopTimer 停止未触发的 timer；nil 或已触发时安全无操作（已触发则排空 C）。
func stopTimer(t *time.Timer) {
	if t == nil {
		return
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

// startJob 用 WaitGroup.Go 启动 Job，并 recover panic。
func (c *Cron) startJob(j Job) {
	c.jobWaiter.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				c.logPanic("job", r)
			}
		}()
		j.Run()
	})
}

// logPanic 将 panic 写入 Logger.Error；若为 discardLogger 则同时写 stderr。
// kind 为 "run" 或 "job"。
func (c *Cron) logPanic(kind string, r any) {
	c.logger.Error("cron "+kind+" panic recovered", "error", r)
	// 仅对默认 discard 写 stderr，避免自定义 Logger 被重复刷屏
	if c.logger == discard {
		fmt.Fprintf(os.Stderr, "cron: %s panic recovered: %v\n", kind, r)
	}
}

// now 返回 location 下的当前时间，供调度与日志统一使用。
func (c *Cron) now() time.Time { return time.Now().In(c.location) }

// Stop 请求停止调度并立即返回 stopCtx。
//
// 不在此处将 running 置 false（由 run defer 在 close(runDone) 后清除）。
// stop 已发送过则走 default，保证可重入且不阻塞。
// 全部 Job 结束后 cancel stopCtx；多次调用返回同一 context。
//
//	<-c.Stop().Done()
func (c *Cron) Stop() context.Context {
	c.runningMu.Lock()
	if c.running {
		select {
		case c.stop <- struct{}{}:
		default:
		}
	}
	c.runningMu.Unlock()
	return c.stopCtx
}

// removeEntry 用 slices.DeleteFunc 按 ID 删除；不存在则不变。O(n)。
func (c *Cron) removeEntry(id EntryID) {
	c.entries = slices.DeleteFunc(c.entries, func(e *Entry) bool {
		return e.ID == id
	})
}
