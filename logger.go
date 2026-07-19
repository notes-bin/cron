package cron

// Logger 调度器日志接口。
//
// 设计目标：保持最小接口，让用户的选择最大化。
// 只定义 Info 和 Error 两个级别，覆盖调度器所有输出场景。
// 用户可将任何日志实现（slog、logrus、zap、自定义等）通过 WithLogger 注入。
//
// 接口参数 keysAndValues 采用结构化日志的 key-value 风格，
// 便于日志系统直接消费（如 slog.Handler 的 slog.Attr 模式）。
type Logger interface {
	// Info 记录调度器常规事件：任务添加、删除、触发、唤醒等。
	Info(msg string, keysAndValues ...any)
	// Error 记录异常事件：job panic、调度器 panic 等。
	Error(msg string, keysAndValues ...any)
}

// discardLogger 是默认的 Logger 实现，丢弃所有日志输出。
//
// 用户未配置 Logger 时使用此实现，保持"安静启动"的体验。
// 即使使用 discardLogger，panic 时仍有 stderr 兜底输出（见 logPanic）。
type discardLogger struct{}

// 编译期接口检查：确保 discardLogger 实现了 Logger。
var _ Logger = (*discardLogger)(nil)

func (l *discardLogger) Info(msg string, keysAndValues ...any)  {}
func (l *discardLogger) Error(msg string, keysAndValues ...any) {}
