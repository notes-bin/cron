package cron

// Logger 调度器日志接口。
type Logger interface {
	Info(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// discardLogger 丢弃所有日志的默认 Logger 实现。
type discardLogger struct{}

var _ Logger = (*discardLogger)(nil)

func (l *discardLogger) Info(msg string, keysAndValues ...any)  {}
func (l *discardLogger) Error(msg string, keysAndValues ...any) {}
