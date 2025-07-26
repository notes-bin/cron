package cron

import "log/slog"

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

// defaultLogger 是默认的日志实现
// 使用标准库log/slog进行日志输出
type defaultLogger struct {
	logger *slog.Logger
}

// Info 实现Logger接口的Info方法
func (l *defaultLogger) Info(msg string, keysAndValues ...any) {
	l.logger.Info(msg, keysAndValues...)
}

// Error 实现Logger接口的Error方法
func (l *defaultLogger) Error(msg string, keysAndValues ...any) {
	l.logger.Error(msg, keysAndValues...)
}
