# 2026-04-19 XML Tool Call Fix Validation

## 变更目标
- 修复 `<tool_call>...</tool_call>` 中 XML 参数块被当作普通文本的问题。
- 修复 `/v1/chat/completions` 在 `stream=true` 且 `HasTools=false` 时，标签式工具调用被透传为 `delta.content` 的问题。

## 代码变更摘要
- `internal/openai/compat.go`
  - 在 `parseToolCallTaggedBlock` 中新增 tagged 参数解析分支：
  - 优先按 JSON 解析参数块；
  - JSON 失败时回退按 XML 子节点解析，并转换为 arguments JSON。
  - 新增 XML 解析辅助逻辑（节点合并、重复标签数组化、叶子节点文本提取）。
- `internal/http/handlers.go`
  - 在 `/v1/chat/completions` 流式且 `HasTools=false` 路径中新增 tool-call 兜底识别：
  - 先聚合上游流文本，再判定是否存在工具调用；
  - 命中工具调用时输出 `tool_calls` SSE；
  - 未命中时按普通文本 SSE 输出。
- `internal/openai/compat_test.go`
  - 新增 tagged JSON 参数解析测试。
  - 新增 tagged XML 参数解析测试。
  - 新增普通文本不误判测试。
- `internal/http/handlers_test.go`
  - 新增 chat 流式兜底识别 XML tagged tool call 测试（无 tools 配置）。
  - 新增 chat 流式普通文本不误判测试（无 tools 配置）。

## 测试命令与结果
> 说明：默认用户目录 Go 缓存无权限，测试时将 `GOCACHE/GOMODCACHE` 指向仓库内目录。

1. `D:\go\bin\go.exe test ./internal/openai -v`
   - 环境变量：`GOCACHE=D:\1work\api调用\.gocache`，`GOMODCACHE=D:\1work\api调用\.gomodcache`
   - 结果：`PASS`
   - 新增用例通过：
     - `TestParseToolSentinelTagWrappedToolCallJSONArgs`
     - `TestParseToolSentinelTagWrappedToolCallXMLArgs`
     - `TestParseToolSentinelPlainTextDoesNotMisclassifyAsToolCall`

2. `D:\go\bin\go.exe test ./internal/http -v`
   - 环境变量同上
   - 结果：`PASS`
   - 新增用例通过：
     - `TestChatCompletionsStreamFallbackDetectsXMLTagToolCallWithoutTools`
     - `TestChatCompletionsStreamFallbackKeepsPlainTextWithoutTools`

3. `D:\go\bin\go.exe test ./... -v`
   - 环境变量同上
   - 结果：`PASS`（全仓通过）

## 验证结论
- XML 参数块已可转换为 tool call `arguments` JSON。
- chat 流式 `HasTools=false` 分支已具备工具调用兜底识别，不再把 tagged tool call 直接透传为普通文本内容。
- 普通文本路径保持不误判。
