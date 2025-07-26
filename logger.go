package cron

// Logger 定义了调度器使用的日志接口
// 允许用户提供自定义日志实现
// 包含Info和Error两个级别
type Logger interface {
	Info(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// discardLogger 实现了Logger接口，所有日志操作均无实际输出
type discardLogger struct{}

// Info 实现Logger接口的Info方法
func (l *discardLogger) Info(msg string, keysAndValues ...any) {}

// Error 实现Logger接口的Error方法
func (l *discardLogger) Error(msg string, keysAndValues ...any) {}
