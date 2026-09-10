// Package cron 提供轻量级定时任务调度器。
//
// 要求 Go 1.26+。
//
// # 架构
//
//	External API                 run() 事件循环（单 goroutine）
//	─────────────                ────────────────────────────
//	AddJob / AddFunc ──add───→   slices.SortFunc(entries, entryByNext)
//	Remove ──────────remove──→   time.NewTimer(最早 Next - now)
//	Stop ────────────stop────→   select { timer | add | remove | stop }
//	Start / Run ─── go/阻塞 ──→   Job 经 WaitGroup.Go 在独立 goroutine 执行
//
//	退出 defer 顺序:
//	  close(runDone) → running=false → jobWaiter.Wait() → stopCancel()
//
// # 并发模型
//
//   - 运行中：entries 只由 run() 读写；AddJob/Remove 经无缓冲 channel 交给 run()。
//   - 未运行或 run 已退出：AddJob/Remove 在 runningMu 下直接改 entries。
//   - runDone 在事件循环退出时关闭，与 add/remove 发送并列在 select 中，
//     避免 run 退出后发送方永久阻塞，也避免 select+default 在 run 忙碌时误改 entries。
//   - running 仅在 run() defer 中清为 false；Stop() 只发 stop 信号。
//   - Job 与调度循环解耦：startJob 使用 sync.WaitGroup.Go 跟踪生命周期。
//
// # 特性
//
//   - 运行中动态增删任务（channel 背压）
//   - Schedule / Job 接口可扩展；内置 Every 固定间隔
//   - 时区可配置（默认 time.Local）
//   - Logger 可注入（默认静音；panic 时 discardLogger 仍有 stderr 兜底）
//   - 优雅退出：Stop() 返回 context，待全部 Job 结束后 cancel
//   - 无任务时用 nil channel 阻塞 select，不占用超长 timer
//
// # 注意
//
//   - Schedule.Next 必须基于传入的 now，不要用 time.Now()
//   - Next 返回零值表示不再调度（条目仍留在列表中，排序靠后）
//   - Job.Run 应尽快返回；长任务自行开 goroutine
//   - 每个 Cron 实例只应 Start/Run 一次，不支持 Stop 后再 Start
package cron
