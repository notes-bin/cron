package cron

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// Cron 定时任务调度器的主要结构体
// 维护任务列表、运行状态和同步原语
type Cron struct {
	entries   []*Entry
	stop      chan struct{}
	add       chan *Entry
	remove    chan EntryID
	running   bool
	runningMu sync.Mutex   // 保护 running 和 nextID
	entriesMu sync.RWMutex // 保护 entries 切片；run() 是唯一写者，外部读取需持有此锁
	location  *time.Location
	nextID    EntryID
	jobWaiter sync.WaitGroup // 等待所有运行中 job 完成
	logger    Logger
}

// Job 定时任务接口。
// Run 在任务触发时被调用，应快速返回；耗时操作自行启动 goroutine。
type Job interface {
	Run()
}

// Schedule 调度策略接口。
// Next 返回下一次执行时间；返回零值表示不再调度。
type Schedule interface {
	Next(time.Time) time.Time
}

// EntryID 是任务的唯一标识符。
type EntryID int

// Entry 表示一个定时任务条目
type Entry struct {
	ID       EntryID
	Schedule Schedule
	Next     time.Time // 下次执行时间；零值表示无调度
	Prev     time.Time // 上次执行时间
	Job      Job
}

// byTime 实现 sort.Interface，按 Next 升序排列。
// Next 为零值的 entry 排在最后（视为无调度，不应触发）。
type byTime []*Entry

func (s byTime) Len() int      { return len(s) }
func (s byTime) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byTime) Less(i, j int) bool {
	if s[i].Next.IsZero() {
		return false
	}
	if s[j].Next.IsZero() {
		return true
	}
	return s[i].Next.Before(s[j].Next)
}

// New 创建一个新的Cron调度器实例
// 默认使用本地时区，不输出日志（通过WithLogger注入日志实现）
func New(opts ...Option) *Cron {
	c := &Cron{
		entries:   nil,
		add:       make(chan *Entry),
		stop:      make(chan struct{}),
		remove:    make(chan EntryID),
		running:   false,
		runningMu: sync.Mutex{},
		location:  time.Local,
		logger:    &discardLogger{},
	}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			panic(err)
		}
	}
	return c
}

// FuncJob 将 func() 适配为 Job 接口。
type FuncJob func()

// Run 实现 Job 接口。
func (f FuncJob) Run() { f() }

// AddFunc 添加一个函数作为定时任务，返回任务 ID。
func (c *Cron) AddFunc(schedule Schedule, cmd func()) EntryID {
	return c.AddJob(schedule, FuncJob(cmd))
}

// AddJob 添加一个 Job 到调度器，返回任务 ID。
// schedule 和 cmd 均不能为 nil。
// 调度器未启动时直接加入列表；已启动则通过 channel 异步添加。
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
		c.entries = append(c.entries, entry)
	} else {
		c.add <- entry
	}
	return entry.ID
}

// Location 返回调度器使用的时区。
func (c *Cron) Location() *time.Location { return c.location }

// Remove 从调度器中删除指定 ID 的任务。
func (c *Cron) Remove(id EntryID) {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		c.remove <- id
	} else {
		c.removeEntry(id)
	}
}

// Start 在后台 goroutine 启动调度器。已启动则直接返回。
func (c *Cron) Start() {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		return
	}
	c.running = true
	go c.run()
}

// Run 启动调度器并阻塞当前 goroutine，直到 Stop 被调用。
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

// run 是调度器主循环。
// 外层循环：排序并创建定时器；内层 select：处理定时触发或增删事件。
// 内层 break 后回到外层重新排序，保证下次定时器基于最新状态。
func (c *Cron) run() {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("cron run panic recovered", "error", r)
			fmt.Fprintf(os.Stderr, "cron: run panic recovered: %v\n", r)
			c.runningMu.Lock()
			c.running = false
			c.runningMu.Unlock()
		}
	}()

	now := c.now()
	for _, entry := range c.entries {
		entry.Next = entry.Schedule.Next(now)
		c.logger.Info("schedule", "now", now, "entry", entry.ID, "next", entry.Next)
	}

	for {
		c.entriesMu.RLock()
		sort.Sort(byTime(c.entries))
		c.entriesMu.RUnlock()

		var timer *time.Timer
		if len(c.entries) == 0 || c.entries[0].Next.IsZero() {
			timer = time.NewTimer(100000 * time.Hour)
		} else {
			timer = time.NewTimer(c.entries[0].Next.Sub(now))
		}

		for {
			select {
			case now = <-timer.C:
				now = now.In(c.location)
				c.logger.Info("wake", "now", now)

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
				timer.Stop()
				now = c.now()
				newEntry.Next = newEntry.Schedule.Next(now)
				c.entries = append(c.entries, newEntry)
				c.logger.Info("added", "now", now, "entry", newEntry.ID, "next", newEntry.Next)

			case <-c.stop:
				timer.Stop()
				c.logger.Info("stop")
				return

			case id := <-c.remove:
				timer.Stop()
				now = c.now()
				c.removeEntry(id)
				c.logger.Info("removed", "entry", id)
			}

			break
		}
	}
}

// startJob 在独立 goroutine 中执行 job，捕获 panic 防止调度器崩溃。
func (c *Cron) startJob(j Job) {
	c.jobWaiter.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.logger.Error("job panic recovered", "error", r)
				fmt.Fprintf(os.Stderr, "cron: job panic recovered: %v\n", r)
			}
			c.jobWaiter.Done()
		}()
		j.Run()
	}()
}

// now 返回当前时间（使用调度器设置的时区）。
func (c *Cron) now() time.Time { return time.Now().In(c.location) }

// Stop 停止调度器。
// 返回的 context 在所有运行中的 job 完成后被取消，调用方可据此等待优雅退出。
func (c *Cron) Stop() context.Context {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		c.stop <- struct{}{}
		c.running = false
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		c.jobWaiter.Wait()
		cancel()
	}()
	return ctx
}

// removeEntry 从 entries 中删除指定 ID 的 entry。
func (c *Cron) removeEntry(id EntryID) {
	if c.entries == nil {
		return
	}
	var entries []*Entry
	for _, e := range c.entries {
		if e.ID != id {
			entries = append(entries, e)
		}
	}
	c.entries = entries
}
