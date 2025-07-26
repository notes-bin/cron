package cron

import (
	"errors"
	"time"
)

// Option 定义用于配置Cron的函数选项类型
// 实现函数选项模式，允许灵活设置Cron实例参数
type Option func(*Cron) error

// WithLocation 设置调度器的时区
// 参数location为要使用的时区，不能为nil
// 返回选项函数和可能的错误
func WithLocation(location *time.Location) Option {
	return func(c *Cron) error {
		if location == nil {
			return errors.New("location cannot be nil")
		}
		c.location = location
		return nil
	}
}

// WithLogger 设置自定义日志器
// 参数logger为实现Logger接口的日志实例，不能为nil
// 返回选项函数和可能的错误
func WithLogger(logger Logger) Option {
	return func(c *Cron) error {
		if logger == nil {
			return errors.New("logger cannot be nil")
		}
		c.logger = logger
		return nil
	}
}
