package cron

import (
	"errors"
	"time"
)

// Option 是函数选项模式中的单个选项。
//
// Go 函数选项模式（Functional Options Pattern）由 Dave Cheney 提出，
// 解决了构造函数参数膨胀问题。每个选项是一个修改 *Cron 实例的函数。
// 如果选项参数无效，返回 error 导致 New() panic，快速暴露配置错误。
type Option func(*Cron) error

// WithLocation 设置调度器使用的时区。
//
// 所有时间计算（Next、now、日志时间戳）都基于此时间。
// 若传入 nil，Option 返回 error，New() 会 panic。
//
// 使用示例:
//
//	loc, _ := time.LoadLocation("Asia/Shanghai")
//	c := cron.New(cron.WithLocation(loc))
func WithLocation(location *time.Location) Option {
	return func(c *Cron) error {
		if location == nil {
			return errors.New("cron: location cannot be nil")
		}
		c.location = location
		return nil
	}
}

// WithLogger 设置自定义日志实现。
//
// 默认使用 discardLogger（丢弃所有日志）。
// 若传入 nil，Option 返回 error，New() 会 panic。
//
// 使用示例:
//
//	type myLogger struct{}
//	func (l *myLogger) Info(msg string, kv ...any)  { log.Printf("INFO: %s %v", msg, kv) }
//	func (l *myLogger) Error(msg string, kv ...any) { log.Printf("ERROR: %s %v", msg, kv) }
//	c := cron.New(cron.WithLogger(&myLogger{}))
func WithLogger(logger Logger) Option {
	return func(c *Cron) error {
		if logger == nil {
			return errors.New("cron: logger cannot be nil")
		}
		c.logger = logger
		return nil
	}
}
