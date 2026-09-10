package cron

// Logger 为调度器日志的最小接口（Info / Error）。
//
// keysAndValues 为结构化键值对，便于对接 slog 等实现。
// 通过 WithLogger 注入；未配置时使用 discardLogger。
type Logger interface {
	// Info 记录常规事件：添加、删除、触发、唤醒等。
	Info(msg string, keysAndValues ...any)
	// Error 记录异常：Job 或 run 的 panic 等。
	Error(msg string, keysAndValues ...any)
}

// discardLogger 丢弃所有日志；为默认实现。
// panic 时 logPanic 通过指针同一性识别本实例，并写 stderr 兜底。
type discardLogger struct{}

// 编译期断言 discardLogger 实现 Logger。
var _ Logger = (*discardLogger)(nil)

// discard 是包级单例，供 New 默认注入与 logPanic 识别，避免每次 new 一个不可比实例。
var discard Logger = &discardLogger{}

func (l *discardLogger) Info(msg string, keysAndValues ...any)  {}
func (l *discardLogger) Error(msg string, keysAndValues ...any) {}
