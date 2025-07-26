package cron

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Cron 定时任务调度器的主要结构体
// 维护任务列表、运行状态和同步原语
// 使用示例:
//
//	c := cron.New()
//
//	c.AddFunc(cron.Every(1*time.Hour), func() {
//		fmt.Println("每小时执行一次")
//	})
//
//	c.Start()
type Cron struct {
	entries   []*Entry       // 所有已注册的定时任务
	stop      chan struct{}  // 停止信号通道
	add       chan *Entry    // 添加任务的通道
	remove    chan EntryID   // 删除任务的通道
	running   bool           // 调度器运行状态
	runningMu sync.Mutex     // 保护running状态的互斥锁
	entriesMu sync.RWMutex   // 保护entries的读写锁
	location  *time.Location // 时区信息
	nextID    EntryID        // 下一个任务ID
	jobWaiter sync.WaitGroup // 等待所有任务完成的WaitGroup
	logger    Logger         // 日志接口
}

// Job 定义了定时任务的接口
// 任何实现了Run方法的类型都可以作为定时任务
// Run方法会在任务触发时被调用
// 注意: Run方法应该快速执行，避免长时间阻塞
// 如果需要执行耗时操作，应该在内部启动goroutine
type Job interface {
	Run()
}

// Schedule 定义了任务调度的接口
// Next方法接收当前时间，返回下一次任务执行的时间
// 调度器会根据此时间安排下一次执行
type Schedule interface {
	Next(time.Time) time.Time
}

// EntryID 是定时任务的唯一标识符类型
// 用于添加、删除和识别任务
type EntryID int

// Entry 表示一个定时任务条目
// 包含任务ID、调度器、下次执行时间、上次执行时间和任务本身
type Entry struct {
	ID       EntryID   // 任务唯一标识符
	Schedule Schedule  // 任务调度器
	Next     time.Time // 下次执行时间
	Prev     time.Time // 上次执行时间
	Job      Job       // 任务实例
}

// byTime 实现了sort.Interface接口，用于按Next时间排序任务
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
// 默认使用本地时区和标准日志
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

// FuncJob 将普通函数转换为Job接口实现
// 方便直接使用匿名函数作为任务
type FuncJob func()

// Run 实现Job接口，调用函数本身
func (f FuncJob) Run() {
	f()
}

// AddFunc 添加一个函数作为定时任务
// 参数:
//
//	schedule - 任务调度器，决定任务何时执行
//	cmd - 要执行的函数
//
// 返回任务ID，可用于后续删除任务
func (c *Cron) AddFunc(schedule Schedule, cmd func()) EntryID {
	return c.AddJob(schedule, FuncJob(cmd))
}

// AddJob 添加一个任务到调度器
// 参数:
//
//	schedule - 任务调度器，决定任务何时执行
//	cmd - 实现了Job接口的任务实例
//
// 返回任务ID，可用于后续删除任务
// 如果调度器未运行，任务会立即添加到任务列表
// 如果调度器已运行，任务会通过通道异步添加
func (c *Cron) AddJob(schedule Schedule, cmd Job) EntryID {
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

// Location 返回当前调度器使用的时区
func (c *Cron) Location() *time.Location {
	return c.location
}

// Remove 从调度器中删除指定ID的任务
// 如果调度器正在运行，会通过通道异步删除
// 如果调度器未运行，会立即删除
func (c *Cron) Remove(id EntryID) {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		c.remove <- id
	} else {
		c.removeEntry(id)
	}
}

// Start 启动调度器的后台运行
// 此方法会启动一个goroutine执行run方法
// 如果调度器已经在运行，此方法会直接返回
func (c *Cron) Start() {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		return
	}
	c.running = true
	go c.run()
}

// Run 启动调度器并阻塞当前goroutine
// 与Start的区别是Start在后台goroutine运行，而Run会阻塞
// 通常在主goroutine中使用Run，在其他情况下使用Start
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

// run 是调度器的主循环
// 负责维护任务列表、计算下次执行时间和触发任务
// 不应直接调用，应通过Start或Run方法启动
func (c *Cron) run() {

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

		// 确保timer总是被停止
		defer timer.Stop()

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

// startJob 启动一个任务的执行
// 会启动新的goroutine执行任务，并处理可能的panic
// 参数j是要执行的任务
func (c *Cron) startJob(j Job) {
	c.jobWaiter.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.logger.Error("job panic recovered", "error", r)
			}
			c.jobWaiter.Done()
		}()
		j.Run()
	}()
}

// now 返回当前时间，考虑了调度器的时区设置
func (c *Cron) now() time.Time {
	return time.Now().In(c.location)
}

// Stop 停止调度器的运行
// 返回一个context.Context，当所有正在执行的任务完成后会被取消
// 调用后，新的任务不会被调度，但正在执行的任务会继续完成
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

// removeEntry 从任务列表中删除指定ID的任务
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
