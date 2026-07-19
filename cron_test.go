package cron

import (
	"sync"
	"testing"
	"time"
)

// TestCronInitialization 验证 New() 创建实例的默认状态。
// 测试项：非空返回值、默认时区、未运行状态。
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

// TestAddJob 验证 AddJob 能正常添加任务并返回非零 ID。
// 任务在未启动状态下直接追加到 entries，验证 entries 中包含该任务。
func TestAddJob(t *testing.T) {
	c := New()
	job := FuncJob(func() {})
	id := c.AddJob(&TestSchedule{}, job)

	if id == 0 {
		t.Error("expected non-zero EntryID")
	}

	found := false
	for _, entry := range c.entries {
		if entry.ID == id {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("entry with ID %d not found", id)
	}
}

// TestRemoveJob 验证 Remove 能正确删除指定 ID 的任务。
// 先添加任务，再删除，确认 entries 中不再包含该任务。
func TestRemoveJob(t *testing.T) {
	c := New()
	job := FuncJob(func() {})
	id := c.AddJob(&TestSchedule{}, job)

	c.Remove(id)

	for _, entry := range c.entries {
		if entry.ID == id {
			t.Errorf("entry with ID %d was not removed", id)
		}
	}
}

// TestStartStop 验证调度器的启动与停止流程。
// 测试项：
//   - Start 后 running = true
//   - Stop 发送信号后 running = false
//   - Stop() 返回的 context 最终被 cancel（所有 job 完成）
//
// 注意：此测试不添加任何任务，验证的是调度器本身的 lifecycle。
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

// TestJobExecution 验证 ImmediateSchedule 能立即触发任务执行。
// 使用 channel + sync.Once 确保任务恰好被执行一次，超时 1s。
// 使用 Once 防止 ImmediateSchedule 的立即重触发导致多次 Done()。
func TestJobExecution(t *testing.T) {
	c := New()
	done := make(chan struct{})
	var once sync.Once
	job := FuncJob(func() {
		once.Do(func() { close(done) })
	})

	c.AddJob(&ImmediateSchedule{}, job)
	c.Start()
	defer c.Stop()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("job was not executed")
	}
}

// TestSchedule 用于测试，每小时触发一次。
type TestSchedule struct{}

func (s *TestSchedule) Next(t time.Time) time.Time { return t.Add(1 * time.Hour) }

// ImmediateSchedule 用于测试，返回与 now 相同的时间。
// 此调度器会立即触发（timer duration ≤ 0），适合验证 Job 被调度器执行的流程。
// 注意：由于 Next(now) = now，会形成连续触发（每个周期 Next 都是当前时间），
// 测试中应使用 sync.Once 或计数器避免无限执行。
type ImmediateSchedule struct{}

func (s *ImmediateSchedule) Next(t time.Time) time.Time { return t }
