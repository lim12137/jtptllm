# 2026-04-20 Codebuddy Agent 子代理命令归一化修复报告

## 背景
问题现象：`codebuddy` assistant 历史里出现如下片段时，归一化后仍会把说明性文本保留进上下文，未归一成 `assistant_tool_call` 摘要：

```text
<tool_call>
<Agent
{ ... }
</Agent>
</tool_call>
```

## 根因定位（investigate + systematic-debugging）
- 触发链路：`normalizeAssistantHistoryContent -> ParseToolSentinel -> stripRawToolCallArtifacts`
- 形态差异：
  - 当前 `parseToolCallTaggedBlock/parseTaggedToolCall` 仅支持标准 tagged 形态（如 `<Read>...</Read>`、`<tool_name>...</tool_name>`、自闭合 XML）。
  - 该 `Agent` 片段属于“非标准开标签”形态：`<Agent` 后直接是 JSON，缺少标准 `>` 结束开标签，因此不会被 `ParseToolSentinel` 识别为有效 tool_call。
- 漏网原因：
  - raw 清洗阶段虽然能从 artifact 推断出工具名 `Agent`，但现有逻辑在“清洗后仍有残留文本”时优先返回残留文本，导致未替换为 `assistant_tool_call` 摘要。

## TDD 过程
### 1) 先补失败测试（RED）
- 新增测试：`TestPromptFromChatNormalizesCodebuddyAgentArtifactToToolSummary`
- 目标：该 `Agent` 形态在 assistant 历史归一化后应输出 `assistant_tool_call: Agent`

修复前验证命令：

```powershell
$env:GOROOT='D:\go_install\go'; $env:GOPATH='D:\gopath'; $env:Path='D:\go_install\go\bin;' + $env:Path; go test ./internal/openai -run TestPromptFromChatNormalizesCodebuddyAgentArtifactToToolSummary -v
```

结果摘要：`FAIL`，实际为保留说明文本，未归一为 `assistant_tool_call: Agent`。

### 2) 最小修复（GREEN）
- 修复范围仅限 assistant 历史归一化兼容层：`internal/openai/compat.go`
- 调整点：
  - 在 `normalizeAssistantHistoryContent` 中记录 raw 清洗前已解析出的 tool_call 数量。
  - 当满足以下条件时，优先返回 `assistant_tool_call` 摘要：
    - 命中 raw tool_call artifact；
    - raw 清洗前未解析出标准 tool_call；
    - raw 清洗阶段成功推断出工具名（如 `Agent`）；
    - 清洗后仍有残留说明文本。
- 不改动其他解析规则与协议映射逻辑。

## 回归验证
1. 新增用例（修复后）：

```powershell
$env:GOROOT='D:\go_install\go'; $env:GOPATH='D:\gopath'; $env:Path='D:\go_install\go\bin;' + $env:Path; go test ./internal/openai -run TestPromptFromChatNormalizesCodebuddyAgentArtifactToToolSummary -v
```

结果摘要：`PASS`

2. openai 包回归：

```powershell
$env:GOROOT='D:\go_install\go'; $env:GOPATH='D:\gopath'; $env:Path='D:\go_install\go\bin;' + $env:Path; go test ./internal/openai -v
```

结果摘要：`PASS`（全部通过）

3. 仓库级回归：

```powershell
$env:GOROOT='D:\go_install\go'; $env:GOPATH='D:\gopath'; $env:Path='D:\go_install\go\bin;' + $env:Path; go test ./... -v
```

结果摘要：`PASS`（全部通过）

## 结论
- 该问题属于“非标准 `Agent` 子代理命令形态”与现有标准 tagged tool_call 解析规则不一致导致的兼容缺口。
- 修复已控制在 assistant 历史归一化兼容层，未扩大到其他业务逻辑。
