package cron

import "time"

// Option 以函数选项方式配置 *Cron。
// 非法参数（如 nil）应在 Option 内 panic，与 AddJob/Every 一致。
type Option func(*Cron)

// WithLocation 设置调度时区；所有 Next/now/日志时间基于该 Location。
// location 为 nil 时 panic。
//
//	loc, _ := time.LoadLocation("Asia/Shanghai")
//	c := cron.New(cron.WithLocation(loc))
func WithLocation(location *time.Location) Option {
	return func(c *Cron) {
		if location == nil {
			panic("cron: location cannot be nil")
		}
		c.location = location
	}
}

// WithLogger 注入 Logger；默认 discard（静音）。logger 为 nil 时 panic。
//
// 对接 slog 见 Logger 接口注释与 README「对接 log/slog」；完整可运行示例见 Example_slog。
//
//	c := cron.New(cron.WithLogger(myLogger))
func WithLogger(logger Logger) Option {
	return func(c *Cron) {
		if logger == nil {
			panic("cron: logger cannot be nil")
		}
		c.logger = logger
	}
}
