# 2026-04-20 seen-tags-tolerance 修复报告

## 1) 最近 err 中归纳的 tool-call 相关标签与畸形模式

来源：`bin/logs/proxy_8022.err` 及当天关联报告（`2026-04-20-all-tool-tags-compat.md`、`2026-04-20-err-tag-label-loss-fix.md`、`2026-04-20-codebuddy-agent-toolcall-normalization-fix.md`、`2026-04-20-unclosed-tool-call-normalization-fix.md`）。

- 标签集合（按层）：
  - `tool_call`（外层）
  - `tool_name`（wrapper）
  - 工具名层：`Read`、`Glob`、`Bash`、`Agent`
  - 参数层：`file_path`、`pattern`、`command`、`description`
- 畸形模式（最近 err 已出现）：
  - `tool_call/tool_name` 层：
    - `</tool_name>` 多一个：`<tool_call><tool_name>Glob</tool_name>{"pattern":"*.md"}</tool_name></tool_call>`
    - `</tool_name>` 缺失
    - `</tool_call>` 多一个 / 缺失
    - 非标准工具开标签：`<tool_call><Agent ... </Agent></tool_call>`（`<Agent` 后直接 JSON）
  - 参数标签层：
    - `</file_path>` 多一个 / 缺失 / 错配
    - `command/description` 层错配 closing（如 `description` 被 `</command>` 关闭）

## 2) TDD 记录

- 先补失败测试（RED）：
  - `TestParseToolSentinelSingleMalformedTaggedArtifactNeedsRetry`
  - `TestBuildChatCompletionResponseFromTextSingleMalformedTaggedArtifactNoLeak`
  - `TestBuildResponsesResponseFromTextSingleMalformedTaggedArtifactNoLeak`
  - `TestChatCompletionsNonStreamRetriesOnceOnSingleMalformedToolCallArtifact`
  - `TestChatCompletionsNonStreamSingleMalformedToolCallRetriesAtMostOnceAndNoLeak`
  - 以及参数层扩展子用例：`xml args command description mismatched close`
- 修复后转绿（GREEN），并完成包级与全量回归。

## 3) 最小修复说明

- 文件：`internal/openai/compat.go`
  - `parseToolCallTaggedBlock`：
    - 对“单个畸形 tool-call artifact”也标记 `NeedsRetry=true`（此前仅多候选+畸形才重试）。
    - 对畸形 block 做局部清洗（去除 `<tool_call>...</tool_call>` 片段）避免外露原始标签。
  - 新增 `sanitizeTaggedToolCallContent`、`looksLikeMalformedToolCallArtifact`（仅用于畸形判定/清洗，不改变正常路径）。
  - `BuildChatCompletionResponseFromText` / `BuildResponsesResponseFromText`：
    - 当 `NeedsRetry=true` 且 `ToolCalls==0` 时不再回填原始文本，避免把畸形标签重新暴露给用户。
- 文件：`internal/openai/compat_test.go`
  - 增加“单畸形 artifact + 不外露 + 需要重试”覆盖。
  - 增加 `command/description` 参数层错配覆盖。
  - 同步更新旧 malformed 用例断言为“清洗后不外露标签”。
- 文件：`internal/http/handlers_test.go`
  - 增加“单畸形 tool-call 自动重试 1 次”和“重试上限 1 次且不外露”覆盖。

## 4) 测试命令与结果摘要

### RED（修复前）

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/openai -run "Test(ParseToolSentinelSingleMalformedTaggedArtifactNeedsRetry|BuildChatCompletionResponseFromTextSingleMalformedTaggedArtifactNoLeak|BuildResponsesResponseFromTextSingleMalformedTaggedArtifactNoLeak|ParseToolSentinelTagWrappedToolCallMalformedDelimitersCompatibility)$" -v
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/http -run "TestChatCompletionsNonStream(RetriesOnceOnSingleMalformedToolCallArtifact|SingleMalformedToolCallRetriesAtMostOnceAndNoLeak)$" -v
```

结果摘要：失败（`NeedsRetry=false`、`<tool_call>` 原样外露、未触发重试）。

### GREEN（修复后）

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/openai -run "Test(ParseToolSentinelSingleMalformedTaggedArtifactNeedsRetry|BuildChatCompletionResponseFromTextSingleMalformedTaggedArtifactNoLeak|BuildResponsesResponseFromTextSingleMalformedTaggedArtifactNoLeak|ParseToolSentinelTagWrappedToolCallMalformedDelimitersCompatibility)$" -v
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/http -run "TestChatCompletionsNonStream(RetriesOnceOnSingleMalformedToolCallArtifact|SingleMalformedToolCallRetriesAtMostOnceAndNoLeak)$" -v
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/openai -v
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/http -v
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./... -v
```

结果摘要：全部通过（PASS）。
