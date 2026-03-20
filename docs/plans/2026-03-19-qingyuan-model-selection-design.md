# Qingyuan Model Selection Design

## 背景

当前代理对外暴露 OpenAI 兼容接口，请求中的 `model` 由兼容层解析。实际发往上游 `/run` 的 JSON 并没有独立 `model` 字段，模型选择依赖 prompt 头部注入的 marker。现有可选模型里，`fast` 和 `deepseek` 已通过 `**model = <name>**` 机制驱动上游模型切换，并在 `/v1/models` 中展示。

本次仅新增一个可选模型名 `qingyuan`，不改默认模型，不改请求协议，不引入新的配置项。

## 已确认范围

- 新增可选模型名：`qingyuan`
- 默认模型仍保持 `agent`
- 选择机制与 `fast`、`deepseek` 完全一致
- 当请求 `model=qingyuan` 且 prompt 非空时，向上游发送的 prompt 头部应包含：`**model = qingyuan**`
- `/v1/models` 应展示 `qingyuan`
- 不修改 gateway 协议，不新增上游字段，不调整 session 逻辑

## 当前实现梳理

### 请求解析与模型分支

- `internal/openai/compat.go`
  - `ParseChatRequest()` 读取 `payload["model"]`，默认 `agent`
  - `ParseResponsesRequest()` 读取 `payload["model"]`，默认 `agent`
  - `injectModelMarker()` 仅对白名单模型注入 `**model = ...**` marker
- `internal/http/handlers.go`
  - `handleChatCompletions()` / `handleResponses()` 使用 `parsed.Model` 生成响应
  - `/v1/models` 当前只返回 `fast` 与 `deepseek`
- `internal/gateway/client.go`
  - 上游 `/run` 请求体没有单独的 `model` 字段，模型选择依赖注入后的 prompt 文本

### 当前模型行为

- `agent`
  - 作为默认模型使用
  - 不注入 marker
- `fast`
  - 注入 `**model = fast**`
- `deepseek`
  - 注入 `**model = deepseek**`

## 目标设计

### 设计原则

- 最小改动
- 行为对齐现有模型选择机制
- 不改变默认值和已有请求路径
- 测试先行，优先补充兼容层与模型列表测试

### 方案

采用“扩展现有 marker 白名单”的方式支持 `qingyuan`：

1. 在兼容层将 `qingyuan` 视为需要注入 marker 的模型之一。
2. 保持 `ParseChatRequest()` / `ParseResponsesRequest()` 的默认模型逻辑不变，默认仍是 `agent`。
3. 在 `/v1/models` 中新增 `qingyuan`，让客户端可以发现该模型。
4. 保持最终 OpenAI 兼容响应中的 `model` 字段回显请求值 `qingyuan`。

### 数据流

1. 客户端向 `/v1/chat/completions` 或 `/v1/responses` 发送 `model: "qingyuan"`
2. `internal/openai/compat.go` 解析请求并构造 prompt
3. `injectModelMarker()` 将 prompt 改写为：

```text
**model = qingyuan**
<原始prompt>
```

4. `internal/gateway/client.go` 将该 prompt 作为 `message.text` 发往上游 `/run`
5. 上游依据 marker 选择模型
6. 代理继续按现有逻辑返回兼容响应，`model` 字段为 `qingyuan`

## 备选方案与取舍

### 方案 A：扩展 marker 白名单

- 优点：与现有 `fast` / `deepseek` 完全一致，代码改动最小，风险最低
- 缺点：模型白名单继续硬编码在兼容层

### 方案 B：放开为任意模型名都注入 marker

- 优点：后续新增模型不需要再改代码
- 缺点：改变现有语义，可能让原本不应走 marker 的模型也被强制切换；风险高于本次需求

### 方案 C：在 gateway 请求体增加独立 `model` 字段

- 优点：语义更显式
- 缺点：与当前上游协议不一致，需要额外联调，不符合“机制与 `fast` / `deepseek` 一致”的已确认方案

推荐采用方案 A。

## 影响面

### 需要修改的逻辑点

- `internal/openai/compat.go`
  - 扩展 `injectModelMarker()` 的允许列表，加入 `qingyuan`
- `internal/http/handlers.go`
  - `/v1/models` 返回列表增加 `qingyuan`

### 需要补充的测试

- `internal/openai/compat_test.go`
  - 新增 chat / responses 对 `qingyuan` 的 marker 注入断言
  - 验证已有 marker 可被替换为 `qingyuan`
- `internal/http/handlers_test.go`
  - 更新 `/v1/models` 列表断言，确认包含 `qingyuan`

## 风险与兼容性

### 风险

- 白名单遗漏：若只改模型列表，不改 marker 注入，客户端会看到模型但实际不会触发上游切换
- 测试覆盖不足：若只测 chat，不测 responses，可能遗漏另一条请求路径

### 兼容性

- 对默认模型 `agent` 无行为变化
- 对现有 `fast`、`deepseek` 无行为变化
- 对不使用 `qingyuan` 的调用方无协议变化

## 验收标准

- `/v1/models` 返回 `fast`、`deepseek`、`qingyuan`
- `model=qingyuan` 的 chat 请求会在 prompt 头部注入 `**model = qingyuan**`
- `model=qingyuan` 的 responses 请求会在 prompt 头部注入 `**model = qingyuan**`
- 默认模型仍为 `agent`
- 现有测试与新增测试全部通过
