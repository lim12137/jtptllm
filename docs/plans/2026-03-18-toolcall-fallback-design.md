# Toolcall Fallback Parsing Design

**日期**: 2026-03-18

## 目标
- 在 system 前缀中明确输出哨兵协议，提升上游按协议输出的概率。
- 当上游未输出哨兵时，支持从 JSON 代码块中兜底解析工具调用。

## 范围
- 仅修改 `internal/openai/compat.go` 与对应测试。
- 不改动 HTTP 路由逻辑（已统一使用 `Build*FromText`）。

## 设计决策
1. **哨兵指令注入**
   - 在 `buildToolSystemPrefix` 的 payload 中增加 `tc_protocol` 字段，包含哨兵格式示例。
   - 当 `tool_choice=none` 时增加 `tc_forbid=true`。
2. **兜底解析策略**
   - **仅当未检测到哨兵**时启用兜底。
   - 仅解析首个 ```json 代码块。
   - 支持三类 JSON：
     - `{"toolCallId":"...","toolName":"...","arguments":{...}}`
     - `{"tool_calls":[{"id":"...","type":"function","function":{"name":"...","arguments":{...}}}]}`
     - `{"function_call":{"name":"...","arguments":{...}}}`
   - 解析成功后：
     - 生成 `tool_calls`；
     - `content` 为“移除代码块后的文本”；
     - 若无剩余文本则置空。
3. **回退**
   - 哨兵与兜底均失败时，保持纯文本返回。

## 测试策略
- 哨兵优先（哨兵存在时忽略代码块）。
- 兜底解析三种 JSON 形态。
- 兜底失败时回退纯文本。

## 风险与缓解
- **误判**：仅解析 `json` 代码块且只取首块，降低误判概率。
- **兼容性**：保留既有哨兵解析优先级，确保旧行为不回退。
