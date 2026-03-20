# 2026-03-20 工具调用未识别问题最新记录

## 背景与结论

结论：本次“工具调用内容未识别再次出现”的**最新记录**来自 `2026-03-20` 的运行日志，而不是 `2026-03-19` 的修复报告。  
`2026-03-19` 报告记录的是另一类问题（裸 JSON `function_call` 被误当普通文本），今天日志显示的是新格式未被当前解析器识别。

## 关键证据（bin/logs/proxy_8022.err）

来源文件：`bin/logs/proxy_8022.err`

1. `line 10` / `2026/03/20 14:21:51`  
   `/v1/responses` 流式 `stream_output` 为空：`"stream_output":""`

2. `line 12` / `2026/03/20 14:22:04`  
   `/v1/responses` 流式 `stream_output` 再次为空：`"stream_output":""`

3. `line 14` / `2026/03/20 14:22:13`  
   `stream_output` 出现标签式工具调用文本：  
   `...<tool_call><multi_tool_use.parallel>...`  
   这不是当前代理预期的 `<<<TC>>>...<<<END>>>` 协议文本，因此会被当成普通文本/无效工具调用内容。

4. `line 134/135/136/137` / `2026/03/20 15:24` 附近  
   同一份日志里，标准 `tc_protocol` 路径（`<<<TC>>>...<<<END>>>`）的 `chat/completions` 与 `responses` 非流式链路可成功产出 `function_call`，说明问题是“特定输出格式未识别”，不是全链路失效。

## 根因候选（按优先级）

1. 上游输出了当前解析器未覆盖的标签式工具调用格式：`<tool_call><multi_tool_use.parallel>...`。  
2. `/v1/responses` 流式在部分请求中直接返回空 `stream_output`，导致无可解析内容。  
3. 测试覆盖集中在既有三种格式，对标签式格式缺少回归覆盖。

对应代码证据：`internal/openai/compat.go` 的 `ParseToolSentinel` 路径显示当前仅支持：
- sentinel：`<<<TC>>>...<<<END>>>`
- JSON 代码块：```json ... ```
- 裸 JSON 对象

未见 `<tool_call>...</tool_call>` 解析分支。

## 测试/命令与结果摘要

本次已重新执行以下命令：

```powershell
powershell -File scripts/codex_toolcall_smoke.ps1
```

结果摘要：
- `/v1/chat/completions`：`200`，返回 `finish_reason=tool_calls`，含 `get_weather` 工具调用。
- `/v1/responses`：`200`，返回 `output[0].type=function_call`，参数 `{"location":"Paris"}`。

解释：冒烟通过证明标准协议链路可用；“再次出现的未识别”与 `14:22:13` 记录的标签式输出格式相关。
