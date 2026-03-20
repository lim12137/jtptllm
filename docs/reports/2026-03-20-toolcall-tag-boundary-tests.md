# 2026-03-20 Tag Toolcall Boundary Tests

## Scope

- 只补边界回归测试，不修改生产实现。
- 覆盖 tag-style tool call 的外层文本保留、非法标签回退、碎片化 `/v1/responses` stream 聚合三类行为。

## Commands

执行时间：2026-03-20 Asia/Shanghai

1. `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run TestParseToolSentinelTagWrappedToolCallPreservesOuterText -v`
   - 结果：PASS
   - 摘要：确认 tag block 前后文本会保留在 `Content` 中；当前实际行为是 `before  after`，中间保留双空格。

2. `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run TestParseToolSentinelMalformedTagWrappedToolCallFallsBackToText -v`
   - 结果：PASS
   - 摘要：非法 tag block 不会误解析为 tool call，会安全回退为普通文本。

3. `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestResponsesStreamFragmentedTagWrappedToolCallUsesFunctionEventsNotOutputText -v`
   - 结果：PASS
   - 摘要：碎片化 stream 输入在最终聚合后仍输出 `response.function_call_arguments.*`，不会落到 `response.output_text.delta`。

4. `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v`
   - 结果：PASS
   - 摘要：`internal/openai` 全部测试通过。

5. `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v`
   - 结果：PASS
   - 摘要：`internal/http` 全部测试通过。

6. `powershell -File scripts/codex_toolcall_smoke.ps1`
   - 结果：PASS
   - 摘要：`/v1/chat/completions` 与 `/v1/responses` 均返回 `200`，tool call flow 正常。

## Re-Verification (Independent)

执行时间：2026-03-20 17:01:54 +08:00 (Asia/Shanghai)

1. `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run 'TestParseToolSentinelTagWrappedToolCall|TestParseToolSentinelTagWrappedToolCallPreservesOuterText|TestParseToolSentinelMalformedTagWrappedToolCallFallsBackToText' -v`
   - 结果：PASS
   - 摘要：命令执行通过；`TestParseToolSentinelTagWrappedToolCall`、`TestParseToolSentinelTagWrappedToolCallPreservesOuterText`、`TestParseToolSentinelMalformedTagWrappedToolCallFallsBackToText` 均被匹配并运行。

2. `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run 'TestResponsesStreamTagWrappedToolCallUsesFunctionEventsNotOutputText|TestResponsesStreamFragmentedTagWrappedToolCallUsesFunctionEventsNotOutputText' -v`
   - 结果：PASS
   - 摘要：命令执行通过；`TestResponsesStreamTagWrappedToolCallUsesFunctionEventsNotOutputText` 与 `TestResponsesStreamFragmentedTagWrappedToolCallUsesFunctionEventsNotOutputText` 均被匹配并运行。

3. `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v`
   - 结果：PASS
   - 摘要：`internal/openai` 全量测试通过（包含 malformed tag fallback 用例）。

4. `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v`
   - 结果：PASS
   - 摘要：`internal/http` 全量测试通过（包含 fragmented tag stream 用例）。

## Notes

- 本轮没有修改 `internal/openai/compat.go` 或其他生产代码。
- 新增测试锁定了当前 parser 的一个细节：tag block 前后文本合并时，中间会保留双空格，而不是折叠为单空格。
