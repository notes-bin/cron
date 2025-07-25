package cron

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

type Cron struct {
	entries   []*Entry
	stop      chan struct{}
	add       chan *Entry
	remove    chan EntryID
	running   bool
	runningMu sync.Mutex
	location  *time.Location
	parser    ScheduleParser
	nextID    EntryID
	jobWaiter sync.WaitGroup
	entriesMu sync.RWMutex
}

type ScheduleParser interface {
	Parse(spec string) (Schedule, error)
}

type Job interface {
	Run()
}

type Schedule interface {
	Next(time.Time) time.Time
}

type EntryID int

type Entry struct {
	ID       EntryID
	Schedule Schedule
	Next     time.Time
	Prev     time.Time
	Job      Job
}

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

func New() *Cron {
	c := &Cron{
		entries:   nil,
		add:       make(chan *Entry),
		stop:      make(chan struct{}),
		remove:    make(chan EntryID),
		running:   false,
		runningMu: sync.Mutex{},
		location:  time.Local,
	}
	return c
}

type FuncJob func()

func (f FuncJob) Run() { f() }

func (c *Cron) AddFunc(spec string, cmd func()) (EntryID, error) {
	return c.AddJob(spec, FuncJob(cmd))
}

func (c *Cron) AddJob(spec string, cmd Job) (EntryID, error) {
	schedule, err := c.parser.Parse(spec)
	if err != nil {
		return 0, err
	}
	return c.Schedule(schedule, cmd), nil
}

func (c *Cron) Schedule(schedule Schedule, cmd Job) EntryID {
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

func (c *Cron) Location() *time.Location {
	return c.location
}

func (c *Cron) Remove(id EntryID) {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		c.remove <- id
	} else {
		c.removeEntry(id)
	}
}

func (c *Cron) Start() {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		return
	}
	c.running = true
	go c.run()
}

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

func (c *Cron) run() {

	now := c.now()
	for _, entry := range c.entries {
		entry.Next = entry.Schedule.Next(now)
		slog.Info("schedule", "now", now, "entry", entry.ID, "next", entry.Next)
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
				slog.Info("wake", "now", now)

				for _, e := range c.entries {
					if e.Next.After(now) || e.Next.IsZero() {
						break
					}
					c.startJob(e.Job)
					e.Prev = e.Next
					e.Next = e.Schedule.Next(now)
					slog.Info("run", "now", now, "entry", e.ID, "next", e.Next)
				}

			case newEntry := <-c.add:
				timer.Stop()
				now = c.now()
				newEntry.Next = newEntry.Schedule.Next(now)
				c.entries = append(c.entries, newEntry)
				slog.Info("added", "now", now, "entry", newEntry.ID, "next", newEntry.Next)

			case <-c.stop:
				timer.Stop()
				slog.Info("stop")
				return

			case id := <-c.remove:
				timer.Stop()
				now = c.now()
				c.removeEntry(id)
				slog.Info("removed", "entry", id)
			}

			break
		}
	}
}

func (c *Cron) startJob(j Job) {
	c.jobWaiter.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("job panic recovered", "error", r)
			}
			c.jobWaiter.Done()
		}()
		j.Run()
	}()
}

func (c *Cron) now() time.Time {
	return time.Now().In(c.location)
}

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
