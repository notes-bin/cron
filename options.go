package cron

import (
	"errors"
	"time"
)

// Option 函数选项模式，用于配置 Cron 实例。
type Option func(*Cron) error

// WithLocation 设置调度器时区，不能为 nil。
func WithLocation(location *time.Location) Option {
	return func(c *Cron) error {
		if location == nil {
			return errors.New("location cannot be nil")
		}
		c.location = location
		return nil
	}
}

// WithLogger 设置自定义日志实现，不能为 nil。
func WithLogger(logger Logger) Option {
	return func(c *Cron) error {
		if logger == nil {
			return errors.New("logger cannot be nil")
		}
		c.logger = logger
		return nil
	}
}
