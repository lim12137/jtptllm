# Global Concurrency Limit Design

## 背景

当前代理已经具备按 session key 划分的会话池与队列能力：同一个 key 下最多占用固定数量的 upstream session，池满后在该 key 内等待可用会话。但这并不能限制“不同 session key 下的总并发”，因此在多 key 同时打入时，系统仍可能出现整体并发过高、上游不稳定、长尾延迟放大等问题。

本次目标是在 HTTP 层增加第一层全局总并发门闸，把全局在途请求数限制为 `16`，并让 `chat` / `responses` 两条入口统一受限。超过 `16` 时不直接返回 `429`，而是进入全局等待队列；请求结束后释放名额。现有第二层 per-key pool / queue 继续保留。

## 已确认方案

- 全局总并发限制：`16`
- 实现层次：HTTP 层
- 受限入口：
  - `/v1/chat/completions`
  - `/v1/responses`
- 超过 `16` 时：排队等待，不直接返回 `429`
- 请求结束后释放全局名额
- 现有 per-key pool / queue 保留

## 设计目标

本次设计要解决的是“跨不同 session key 的总并发约束”，不是替代现有会话池。最终结构应能明确回答：

1. 哪一层控制总并发？
2. 哪一层控制单 key 内的 session 分配？
3. 两层同时存在时，请求生命周期如何流转？

## 两层并发控制关系

### 第一层：全局并发门闸

位置：HTTP 层，覆盖所有进入业务处理的请求。

职责：

- 控制全局在途请求数不超过 `16`
- 当全局已满时，让新请求等待空闲名额
- 请求结束后归还名额

这一层解决的是：

- 不同 session key 之间的总量约束
- chat / responses 两类入口共享同一上限
- 防止“每个 key 都能各自并发，叠加后总量失控”

### 第二层：per-key session pool / queue

位置：`internal/session`

职责：

- 同一个 key 下复用 upstream session
- 控制该 key 的会话池容量
- 当该 key 下 session 都忙时，在该 key 内等待

这一层解决的是：

- 单 key 的会话复用与轮换
- 单 key 的排队语义
- `forceNew` / `closeAfter` 等会话行为

### 两层协作关系

推荐顺序：

1. 请求先通过 HTTP 层全局门闸
2. 获得全局名额后，再进入现有 `ensureSession()` 逻辑
3. 在 `ensureSession()` 内继续走 per-key `Acquire()`
4. 请求完成后：
   - 先释放 session lease
   - 再释放全局名额

这样做的好处：

- 系统总并发先被压住
- per-key 池仍可保持现有语义
- 两层职责边界清晰，不把全局限制塞进 session pool 内部

## 推荐实现落点

### 主实现文件

- `internal/http/handlers.go`
  - 在 `Server` 上增加全局并发控制对象
  - 在 `handleChatCompletions()` / `handleResponses()` 的请求生命周期中统一获取与释放名额
  - 或封装在 `ensureSession()` 前后的一层全局 acquire/release 中

### 配置与初始化

- `internal/http/server.go`
  - 增加默认常量，例如：`defaultGlobalConcurrencyLimit = 16`
  - 启动时初始化 `Server`

### 测试

- `internal/http/handlers_test.go`
  - 补全局并发门闸相关测试

## 为什么不放在 `internal/session/pool.go`

`internal/session/pool.go` 当前是按 key 管理池与队列：

- key 是其天然边界
- 全局并发限制是跨 key 的语义

如果把全局限制塞进 `PoolManager`：

- 会让 `internal/session` 同时承担“全局 HTTP 流量门闸”和“per-key 会话池”两种职责
- 后续理解和测试都更复杂

因此，从最小改动和职责清晰度看，全局限制应落在 HTTP 层。

## 排队而不是直接拒绝的含义

本次已确认：超过 `16` 时排队等待，而不是返回 `429`。

这意味着：

- HTTP 层新增的是“全局队列型并发门闸”，不是“快速失败限流器”
- 一部分请求会在进入真正业务处理前阻塞等待
- 这会引入额外等待时间，因此验证时必须特别关注长尾延迟

## 风险

### 1. 双层排队

当前系统已有 per-key 排队；新增后会形成：

- 第一层：全局排队
- 第二层：per-key 排队

风险：

- 请求等待时间更长
- 延迟来源变复杂
- 调试时需要区分是“等全局名额”还是“等 key 内 session”

### 2. 长尾延迟变大

因为不是直接拒绝，而是等待：

- 当总并发接近或超过 `16` 时，`p95` / `p99` 可能明显升高
- 成功率可能仍然很好，但体验变差

### 3. 释放时机错误会泄漏名额

如果某些错误路径没有正确释放：

- 全局名额会被永久占用
- 最终表现为系统越来越容易卡住

因此 release 必须和现有 `defer` 风格严格对齐。

### 4. stream 请求占用更久

stream 请求会持有名额直到流结束：

- 这会自然压缩可用总并发
- 需要在验证时明确这一点是预期行为，不是 bug

## 验证要点

### 功能正确性

- chat / responses 两条入口都受全局 `16` 限制
- 第 `17` 个请求不会立即失败，而是等待
- 任一请求结束后，等待中的请求能继续进入
- 错误路径和成功路径都会释放名额

### 与 per-key pool 的兼容性

- 现有 per-key pool / queue 行为不被破坏
- `forceNew` / `closeAfter` 语义保持不变
- 单 key 内排队仍由 session pool 控制

### 运行态指标

- 总并发不会超过 `16`
- 过载时成功率可能仍高，但 `p95` 可能上升
- 需要记录全局等待导致的延迟变化

## 验收标准

- HTTP 层存在统一全局总并发门闸
- `/v1/chat/completions` 与 `/v1/responses` 都受限于同一个全局上限 `16`
- 超过 `16` 时请求进入等待，而不是直接 `429`
- 请求结束后正确释放全局名额
- 现有 per-key pool / queue 仍正常工作
- 有对应测试能证明“等待而非拒绝”的行为
