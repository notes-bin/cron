package cron

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// startCron 启动调度器，并在测试结束时等待优雅退出。
func startCron(t *testing.T, c *Cron) {
	t.Helper()
	c.Start()
	t.Cleanup(func() { <-c.Stop().Done() })
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		c := New()
		if c == nil {
			t.Fatal("New() returned nil")
		}
		if c.Location() != time.Local {
			t.Errorf("Location() = %v; want Local", c.Location())
		}
		if c.running {
			t.Error("new Cron should not be running")
		}
	})

	t.Run("WithLocation", func(t *testing.T) {
		t.Parallel()
		loc := time.FixedZone("UTC+8", 8*3600)
		c := New(WithLocation(loc))
		if c.Location() != loc {
			t.Errorf("Location() = %v; want %v", c.Location(), loc)
		}
	})

	t.Run("WithLogger", func(t *testing.T) {
		t.Parallel()
		l := &recordingLogger{}
		c := New(WithLogger(l))
		if c.logger != l {
			t.Error("logger not injected")
		}
	})

	t.Run("WithLocation nil panics", func(t *testing.T) {
		t.Parallel()
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		New(WithLocation(nil))
	})

	t.Run("WithLogger nil panics", func(t *testing.T) {
		t.Parallel()
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		New(WithLogger(nil))
	})
}

func TestAddRemove(t *testing.T) {
	t.Parallel()

	t.Run("AddJob before start", func(t *testing.T) {
		t.Parallel()
		c := New()
		id := c.AddJob(&TestSchedule{}, FuncJob(func() {}))
		if id == 0 {
			t.Fatal("expected non-zero EntryID")
		}
		if slices.IndexFunc(c.entries, func(e *Entry) bool { return e.ID == id }) < 0 {
			t.Fatalf("entry %d not found", id)
		}
	})

	t.Run("Remove before start", func(t *testing.T) {
		t.Parallel()
		c := New()
		id := c.AddJob(&TestSchedule{}, FuncJob(func() {}))
		c.Remove(id)
		if slices.IndexFunc(c.entries, func(e *Entry) bool { return e.ID == id }) >= 0 {
			t.Fatalf("entry %d still present", id)
		}
	})

	t.Run("Remove missing id is no-op", func(t *testing.T) {
		t.Parallel()
		c := New()
		c.AddJob(&TestSchedule{}, FuncJob(func() {}))
		before := len(c.entries)
		c.Remove(EntryID(999))
		if len(c.entries) != before {
			t.Fatalf("len(entries)=%d; want %d", len(c.entries), before)
		}
	})

	t.Run("IDs increment", func(t *testing.T) {
		t.Parallel()
		c := New()
		id1 := c.AddFunc(Every(time.Hour), func() {})
		id2 := c.AddFunc(Every(time.Hour), func() {})
		if id2 != id1+1 {
			t.Fatalf("id2=%d; want %d", id2, id1+1)
		}
	})
}

func TestAddPanics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   func()
	}{
		{"AddFunc nil func", func() { New().AddFunc(Every(time.Second), nil) }},
		{"AddJob nil schedule", func() { New().AddJob(nil, FuncJob(func() {})) }},
		{"AddJob nil job", func() { New().AddJob(Every(time.Second), nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			tt.fn()
		})
	}
}

func TestStartStop(t *testing.T) {
	t.Parallel()

	t.Run("Start and Stop", func(t *testing.T) {
		t.Parallel()
		c := New()
		c.Start()

		c.runningMu.Lock()
		running := c.running
		c.runningMu.Unlock()
		if !running {
			t.Fatal("should be running after Start")
		}

		<-c.Stop().Done()

		c.runningMu.Lock()
		running = c.running
		c.runningMu.Unlock()
		if running {
			t.Fatal("should not be running after Stop Done")
		}
	})

	t.Run("Start is idempotent", func(t *testing.T) {
		t.Parallel()
		c := New()
		startCron(t, c)
		c.Start() // 第二次应为 no-op
		c.Start()
	})

	t.Run("Stop when not running", func(t *testing.T) {
		t.Parallel()
		c := New()
		ctx := c.Stop()
		select {
		case <-ctx.Done():
			t.Fatal("stopCtx should not be cancelled without Start")
		default:
		}
	})

	t.Run("Run blocks until Stop", func(t *testing.T) {
		t.Parallel()
		c := New()
		done := make(chan struct{})
		go func() {
			c.Run()
			close(done)
		}()

		// 等到确实进入 running
		deadline := time.After(time.Second)
		for {
			c.runningMu.Lock()
			running := c.running
			c.runningMu.Unlock()
			if running {
				break
			}
			select {
			case <-deadline:
				t.Fatal("Run did not set running")
			case <-time.After(5 * time.Millisecond):
			}
		}

		<-c.Stop().Done()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Run did not return after Stop")
		}
	})

	t.Run("Run is idempotent while running", func(t *testing.T) {
		t.Parallel()
		c := New()
		startCron(t, c)
		c.Run() // 已在跑，应立即返回
	})
}

func TestJobExecution(t *testing.T) {
	t.Parallel()

	t.Run("ImmediateSchedule fires", func(t *testing.T) {
		t.Parallel()
		c := New()
		done := make(chan struct{})
		closeDone := sync.OnceFunc(func() { close(done) })
		c.AddJob(&ImmediateSchedule{}, FuncJob(closeDone))
		startCron(t, c)

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("job was not executed")
		}
	})

	t.Run("Every fires at least once", func(t *testing.T) {
		t.Parallel()
		c := New()
		done := make(chan struct{})
		closeDone := sync.OnceFunc(func() { close(done) })
		c.AddFunc(Every(20*time.Millisecond), closeDone)
		startCron(t, c)

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Every job was not executed")
		}
	})

	t.Run("job panic does not stop scheduler", func(t *testing.T) {
		t.Parallel()
		log := &recordingLogger{}
		c := New(WithLogger(log))

		var okHits atomic.Int32
		c.AddJob(&ImmediateSchedule{}, FuncJob(func() { panic("boom") }))
		c.AddFunc(Every(20*time.Millisecond), func() { okHits.Add(1) })
		startCron(t, c)

		deadline := time.After(time.Second)
		for okHits.Load() == 0 {
			select {
			case <-deadline:
				t.Fatal("healthy job never ran after panic")
			case <-time.After(10 * time.Millisecond):
			}
		}
		if log.errors.Load() == 0 {
			t.Fatal("expected Logger.Error for job panic")
		}
	})

	t.Run("once schedule runs once", func(t *testing.T) {
		c := New()
		done := make(chan struct{})
		var hits atomic.Int32
		c.AddJob(&OnceSchedule{}, FuncJob(func() {
			hits.Add(1)
			close(done)
		}))
		c.Start()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("once job not executed")
		}

		time.Sleep(50 * time.Millisecond) // 确认不会二次触发
		<-c.Stop().Done()

		if hits.Load() != 1 {
			t.Fatalf("hits=%d; want 1", hits.Load())
		}
	})
}

func TestConcurrentAddRemoveWhileRunning(t *testing.T) {
	c := New()
	startCron(t, c)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			id := c.AddFunc(Every(time.Hour), func() {})
			c.Remove(id)
		})
	}
	wg.Wait()
}

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

func TestEntryByNext(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		in   []*Entry
		want []EntryID
	}{
		{
			name: "zero sorts last",
			in: []*Entry{
				{ID: 1, Next: time.Time{}},
				{ID: 2, Next: now.Add(time.Hour)},
				{ID: 3, Next: now.Add(time.Minute)},
			},
			want: []EntryID{3, 2, 1},
		},
		{
			name: "both zero compare equal",
			in: []*Entry{
				{ID: 1, Next: time.Time{}},
				{ID: 2, Next: time.Time{}},
			},
			want: nil, // 仅校验 Compare==0，排序不稳定
		},
		{
			name: "ascending times",
			in: []*Entry{
				{ID: 1, Next: now.Add(3 * time.Second)},
				{ID: 2, Next: now.Add(1 * time.Second)},
				{ID: 3, Next: now.Add(2 * time.Second)},
			},
			want: []EntryID{2, 3, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entries := slices.Clone(tt.in)
			if tt.want == nil {
				if entryByNext(entries[0], entries[1]) != 0 {
					t.Fatal("expected entryByNext == 0 for two zero Next")
				}
				return
			}
			slices.SortFunc(entries, entryByNext)
			got := make([]EntryID, len(entries))
			for i, e := range entries {
				got[i] = e.ID
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("order=%v; want %v", got, tt.want)
			}
		})
	}
}

func TestDiscardLogger(t *testing.T) {
	t.Parallel()
	// 覆盖 discard 的 Info/Error，确保可调用且无 panic
	discard.Info("info")
	discard.Error("error")
}

// recordingLogger 记录 Error 调用次数，供 panic 路径断言。
type recordingLogger struct {
	errors atomic.Int32
}

func (l *recordingLogger) Info(msg string, keysAndValues ...any) {}
func (l *recordingLogger) Error(msg string, keysAndValues ...any) {
	l.errors.Add(1)
}

// TestSchedule 测试用：每小时触发一次。
type TestSchedule struct{}

func (s *TestSchedule) Next(t time.Time) time.Time { return t.Add(1 * time.Hour) }

// ImmediateSchedule 测试用：Next(now)=now，会立即且连续触发。
type ImmediateSchedule struct{}

func (s *ImmediateSchedule) Next(t time.Time) time.Time { return t }

// OnceSchedule 首次返回 now，之后返回零值（一次性任务）。
type OnceSchedule struct {
	fired atomic.Bool
}

func (s *OnceSchedule) Next(t time.Time) time.Time {
	if s.fired.Swap(true) {
		return time.Time{}
	}
	return t
}
