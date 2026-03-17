# OpenAI Function/Tool Call 纯文本协议与 Proxy 兼容设计

**日期**: 2026-03-17

## 背景与目标
当前代理网关仅接受/输出纯文本（message.text），需要在 proxy 层兼容 OpenAI 的函数/工具调用（Chat Completions 与 Responses）。目标是：
- 让客户端保持 OpenAI 原生请求格式；proxy 侧做输入/输出转换与兼容性修正。
- 低复杂度、高稳定性，同时尽量节省 token。

## 范围
- **输入侧**：Chat Completions 的 `functions/function_call` 与 `tools/tool_calls`，以及 Responses 的 `tools/tool_choice`。
- **输出侧**：将代理纯文本输出解析为 OpenAI 的 `tool_calls` / `function_call` / Responses `function_call`。
- 支持强制调用（`tool_choice` / legacy `function_call`）。

## 非目标
- 不覆盖多模态输入、结构化输出完整 schema 校验。
- 不实现 OpenAI 所有字段，只覆盖常用文本对话场景。

## 设计决策（已确认）
- 采用 **短哨兵包裹 JSON** 的纯文本协议：`<<<TC>>> ... <<<END>>>`。
- 输入侧工具定义做 **裁剪版 JSON Schema** 压缩：保留 `type/required/properties/enum/items`。
- 解析策略采用 **宽松解析 + 兜底回退**：解析失败退回普通文本 `content`。

## 纯文本协议（输出侧约定）
- 哨兵：`<<<TC>>>` 与 `<<<END>>>`
- JSON key（压缩）：
  - `tc`: tool_calls 数组
  - `c`: content
  - `id`: call id
  - `n`: tool name
  - `a`: arguments（对象或 JSON 字符串）

示例：
```
<<<TC>>>{"tc":[{"id":"call_1","n":"get_weather","a":{"location":"Paris"}}],"c":""}<<<END>>>
```

解析规则：
- 优先提取哨兵块；若存在则忽略块外文本。
- 找不到哨兵块则把全文当 `content`。
- `a` 若为对象，统一序列化为 JSON 字符串。
- 缺失 `id` 时由 proxy 生成（用于 Responses 的 `call_id`）。

## 输入侧转换（OpenAI -> 纯文本）
1. **工具定义压缩**：
   - 保留：`name/description/parameters`。
   - `parameters` 仅保留：`type/required/properties/enum/items`。
   - 删除冗余：`title/default/examples/$schema/definitions` 等。
2. **工具选择约束**：
   - `tool_choice=none` => 在系统前缀加“禁止调用工具”。
   - `tool_choice=auto` => 不加约束。
   - `tool_choice={name: X}` => 加“必须调用 X”。
3. **统一系统前缀**：把压缩工具定义与约束整理成短 JSON 块注入 system 前缀，代理只接收纯文本。

## 输出侧转换（纯文本 -> OpenAI）
- 若解析出 `tc`：
  - Chat Completions：`tool_calls`（`type:function` + `function{name,arguments}`）。
  - legacy `function_call`：当仅 1 个 tool_call 时降级填充。
  - Responses：`type:function_call`（`call_id` 对应 `id`）。
- 工具执行结果：Responses 使用 `function_call_output`（`call_id` 对应 `id`）。
- 解析失败：回退为普通文本 `content`。

## 流式策略
为保证工具调用完整性，**流式返回时缓冲至结束**再解析输出并一次性返回 tool_calls；若需要流式文本可保留现有逻辑，但工具调用仅在完成后发出。

## 错误处理
- 解析失败 -> 退回 `content`。
- JSON 解析异常 -> 记录日志（如有）并回退。
- 结构缺失（如 name/arguments）-> 视为普通文本。

## 兼容性矩阵（高层）
- Chat Completions:
  - 支持 `functions/function_call` 与 `tools/tool_calls`。
  - 输出 `tool_calls`，可选降级 `function_call`。
- Responses:
  - 支持 `tools`、`tool_choice`。
  - 输出 `function_call` 与 `function_call_output`。

## 测试策略
- 单测：
  - 输入侧压缩：schema 裁剪覆盖字段。
  - 输出侧解析：哨兵块提取、JSON 解析、回退路径。
  - tool_choice 映射：none/auto/指定。
- 端到端：
  - Chat Completions 与 Responses 的非流式/流式模式，验证 tool_calls 正确生成。

## 风险与缓解
- 模型输出杂音：用哨兵包裹 + 回退。
- arguments JSON 不合法：回退为 content。
- 多 tool_calls 顺序：保持原序。
