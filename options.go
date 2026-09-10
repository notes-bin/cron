package cron

import (
	"errors"
	"time"
)

// Option 以函数选项方式配置 *Cron；无效参数返回 error，New 会 panic。
type Option func(*Cron) error

// WithLocation 设置调度时区；所有 Next/now/日志时间基于该 Location。
// location 为 nil 时返回 error。
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

// WithLogger 注入 Logger；默认 discardLogger。logger 为 nil 时返回 error。
//
//	c := cron.New(cron.WithLogger(myLogger))
func WithLogger(logger Logger) Option {
	return func(c *Cron) error {
		if logger == nil {
			return errors.New("cron: logger cannot be nil")
		}
		c.logger = logger
		return nil
	}
}
