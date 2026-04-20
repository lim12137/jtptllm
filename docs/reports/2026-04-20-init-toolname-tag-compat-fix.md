# 2026-04-20 /init 工具调用 `<tool_name>` 兼容修复报告

## 背景
- 复现输入形态：`<tool_call><tool_name>Glob</tool_name>{"pattern":"*.md"}</tool_call>`
- 现象：解析层未识别为工具调用，`ToolCalls` 为空。

## TDD 执行记录
1. 新增失败测试 `TestParseToolSentinelTagWrappedToolCallToolNameJSONArgs`。
2. 先运行定向测试，确认红灯（`toolcalls=0`）。
3. 做最小修复：在 `parseTaggedToolCall` 增加 `<tool_name>NAME</tool_name>{...}` 分支解析。
4. 重新执行定向与全量测试，全部通过。

## 测试命令与结果摘要
- 命令：`& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/openai -run TestParseToolSentinelTagWrappedToolCallToolNameJSONArgs -v`
  - 结果（修复前）：`FAIL`，`toolcalls=0`
  - 结果（修复后）：`PASS`
- 命令：`& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/openai -v`
  - 结果：`PASS`
- 命令：`& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./... -v`
  - 结果：`PASS`
  - 并发相关测试摘要：`TestGlobalConcurrencyGateSeventeenthRequestWaits`、`TestGlobalConcurrencyGateSharedByChatAndResponses`、`TestGlobalConcurrencyGateReleasesOnError` 均通过。

## 变更范围
- `internal/openai/compat_test.go`
  - 新增测试覆盖 `<tool_call><tool_name>Glob</tool_name>{"pattern":"*.md"}</tool_call>`。
- `internal/openai/compat.go`
  - 增加 `parseToolNameTaggedToolCall` 并在 `parseTaggedToolCall` 中接入。

## 结论
- `/init` 复现形态已被兼容解析为单个工具调用：
  - `Name = Glob`
  - `Arguments = {"pattern":"*.md"}`
