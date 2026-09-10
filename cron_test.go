package cron

import (
	"slices"
	"sync"
	"testing"
	"time"
)

// TestCronInitialization 检查 New 的默认状态：非 nil、Local、未运行。
func TestCronInitialization(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.location != time.Local {
		t.Errorf("expected location %v, got %v", time.Local, c.location)
	}
	if c.running {
		t.Error("new Cron should not be running")
	}
}

// TestAddJob 在未启动时 AddJob，确认返回非零 ID 且 entries 含该任务。
func TestAddJob(t *testing.T) {
	c := New()
	job := FuncJob(func() {})
	id := c.AddJob(&TestSchedule{}, job)

	if id == 0 {
		t.Error("expected non-zero EntryID")
	}

	if slices.IndexFunc(c.entries, func(e *Entry) bool { return e.ID == id }) < 0 {
		t.Errorf("entry with ID %d not found", id)
	}
}

// TestRemoveJob 添加后 Remove，确认 entries 中不再有该 ID。
func TestRemoveJob(t *testing.T) {
	c := New()
	job := FuncJob(func() {})
	id := c.AddJob(&TestSchedule{}, job)

	c.Remove(id)

	if slices.IndexFunc(c.entries, func(e *Entry) bool { return e.ID == id }) >= 0 {
		t.Errorf("entry with ID %d was not removed", id)
	}
}

// TestStartStop 验证 Start 后 running、Stop 且 <-Done 后 running 为 false。
func TestStartStop(t *testing.T) {
	c := New()
	c.Start()

	c.runningMu.Lock()
	running := c.running
	c.runningMu.Unlock()

	if !running {
		t.Error("Cron should be running after Start()")
	}

	ctx := c.Stop()
	<-ctx.Done()

	c.runningMu.Lock()
	running = c.running
	c.runningMu.Unlock()

	if running {
		t.Error("Cron should not be running after Stop()")
	}
}

// TestJobExecution 用 ImmediateSchedule 验证 Job 会被调度执行。
// sync.OnceFunc 防止连续立即触发导致多次 close(done)。
func TestJobExecution(t *testing.T) {
	c := New()
	done := make(chan struct{})
	closeDone := sync.OnceFunc(func() { close(done) })
	job := FuncJob(closeDone)

	c.AddJob(&ImmediateSchedule{}, job)
	c.Start()
	defer func() { <-c.Stop().Done() }()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("job was not executed")
	}
}

// TestConcurrentAddRemoveWhileRunning 运行中并发 Add/Remove；配合 -race 检测竞态。
func TestConcurrentAddRemoveWhileRunning(t *testing.T) {
	c := New()
	c.Start()
	defer func() { <-c.Stop().Done() }()

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := c.AddFunc(Every(time.Hour), func() {})
			c.Remove(id)
		}()
	}
	wg.Wait()
}

// TestAddRemoveDuringStopDoesNotBlock 在 Stop 窗口内 Add/Remove 不得永久阻塞。
func TestAddRemoveDuringStopDoesNotBlock(t *testing.T) {
	c := New()
	c.AddFunc(Every(time.Hour), func() {})
	c.Start()

	started := make(chan struct{})
	closeStarted := sync.OnceFunc(func() { close(started) })
	c.AddFunc(&ImmediateSchedule{}, func() {
		closeStarted()
		time.Sleep(50 * time.Millisecond)
	})
	<-started

	ctx := c.Stop()

	done := make(chan struct{})
	go func() {
		id := c.AddFunc(Every(time.Hour), func() {})
		c.Remove(id)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("AddJob/Remove blocked during/after Stop")
	}
	<-ctx.Done()
}

// TestEntryByNext 验证零值 Next 排在有效时间之后。
func TestEntryByNext(t *testing.T) {
	now := time.Now()
	entries := []*Entry{
		{ID: 1, Next: time.Time{}},
		{ID: 2, Next: now.Add(time.Hour)},
		{ID: 3, Next: now.Add(time.Minute)},
	}
	slices.SortFunc(entries, entryByNext)
	if entries[0].ID != 3 || entries[1].ID != 2 || entries[2].ID != 1 {
		t.Fatalf("unexpected order: %v, %v, %v", entries[0].ID, entries[1].ID, entries[2].ID)
	}
}

// TestSchedule 测试用：每小时触发一次。
type TestSchedule struct{}

func (s *TestSchedule) Next(t time.Time) time.Time { return t.Add(1 * time.Hour) }

// ImmediateSchedule 测试用：Next(now)=now，会立即且连续触发。
// 用例中须用 OnceFunc 或计数器限制执行次数。
type ImmediateSchedule struct{}

func (s *ImmediateSchedule) Next(t time.Time) time.Time { return t }
