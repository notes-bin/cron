// Package cron 提供轻量级、高可靠的定时任务调度器。
//
// ┌─────────────────────────────────────────────────────────────┐
// │  架构概览                                                    │
// │                                                             │
// │   ┌──────────┐   Start()    ┌──────────────────────┐        │
// │   │ External  │ ──────────→ │  run() goroutine     │        │
// │   │ API       │             │                      │        │
// │   │           │   channel   │  for {                │        │
// │   │ AddJob ──→│──────────→  │    sort               │        │
// │   │ Remove ──→│──────────→  │    timer              │        │
// │   │ Stop   ──→│──────────→  │    select {           │        │
// │   └──────────┘             │      timer.C          │        │
// │               ┌───────┐    │      add/remove/stop  │        │
// │               │Cron{} │    │    }                  │        │
// │               └───────┘    │  }                    │        │
// │                             └──────────────────────┘        │
// │                                        │                    │
// │                                        ▼                    │
// │                             ┌──────────────────────┐        │
// │                             │  defer on exit:      │        │
// │                             │  jobWaiter.Wait()    │        │
// │                             │  stopCancel()        │        │
// │                             └──────────────────────┘        │
// └─────────────────────────────────────────────────────────────┘
//
// 核心特性:
//   - 动态添加/删除定时任务，无需重启调度器 — channel 驱动的异步操作
//   - 单 goroutine 事件循环 — 天然无锁、无竞态、无需复杂同步
//   - 自定义日志接口，默认静音，panic 有 stderr 兜底
//   - 完整时区支持，适应跨时区调度场景
//   - 灵活的 Schedule 接口，支持任意触发规则
//   - 任务 panic 安全捕获，单个 Job 崩溃不影响调度器和其他 Job
//   - 优雅退出：Stop() 返回 context，等待所有运行中 Job 完成
//   - nil channel sentinel — 无任务时零资源阻塞，替代传统的 long-timer hack
//
// 并发模型
//
// 调度器内部只有一个 run() goroutine 处理所有事件：
//   - 定时触发器（timer.C）
//   - 新增任务（add channel）
//   - 删除任务（remove channel）
//   - 停止信号（stop channel）
//
// 所有外部 API 调用通过 channel 将请求发送至此 goroutine，实现事件序列化。
// 这种设计避免了显式锁（除了 runningMu 保护状态切换），降低了并发复杂度。
// Job 的执行在分离的 goroutine 中进行，不阻塞事件循环。
//
// 注意事项
//
//   - Schedule.Next 应基于传入的 now 参数而非 time.Now() 计算
//   - Job.Run 应尽快返回；长时间运行应在内部自行管理 goroutine
//   - Schedule.Next 返回零值表示不再调度
//   - 每个 Cron 实例仅应 Start() 一次，不支持 Stop 后再 Start
//   - 默认使用 time.Local 时区，可通过 WithLocation 配置
//
package cron
