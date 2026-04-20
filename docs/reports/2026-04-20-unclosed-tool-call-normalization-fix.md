# 未闭合 `</tool_call>` 兜底归一化修复报告（2026-04-20）

## 背景
用户反馈：assistant 历史中出现未闭合 `</tool_call>` 的非标片段时，整块无法被匹配替换，原始标签会污染后续上下文。

示例输入：

```xml
<tool_call>
  <Bash>
  <command>ls -la</command>
  <description>List all files and directories in the current directory</description>
  </command>
  </Bash>
```

## 根因定位
- 归一化链路在 `internal/openai/compat.go` 的 `normalizeAssistantHistoryContent -> stripRawToolCallArtifacts`。
- 原实现仅处理：
  - 成对标签：`<tool_call ...> ... </tool_call>`（`toolCallTagBlockRe`）
  - 哨兵协议：`<<<TC>>> ... <<<END>>>`
- 对未闭合 `<tool_call>` 没有兜底匹配，导致该片段直接保留进 prompt。

## TDD 过程
### 1) 先补失败测试（修复前失败）
- 新增测试：`TestNormalizeAssistantHistoryContentUnclosedToolCallTagStrippedInNormalMode`
- 断言：示例输入应归一化为 `assistant_tool_call: Bash`

执行命令：

```powershell
& 'D:\go_install\go\bin\go.exe' test ./internal/openai -run TestNormalizeAssistantHistoryContentUnclosedToolCallTagStrippedInNormalMode -v
```

结果摘要：
- **FAIL**
- 关键输出：`got="<tool_call> ... </Bash>" want="assistant_tool_call: Bash"`

### 2) 最小修复
- 文件：`internal/openai/compat.go`
- 变更点：
  - 新增 `toolCallOpenTagRe`（仅识别 `<tool_call ...>` 开标签）
  - 新增 `stripUnclosedToolCallArtifacts(content string)`：
    - 扫描未闭合 `<tool_call>` 开标签（右侧无 `</tool_call>`）
    - 若可推断工具名（如 `Bash`），剥离该残留片段并回传工具名
  - 在 `stripRawToolCallArtifacts` 中串联该兜底逻辑
- 影响范围：仅 assistant 历史归一化，不改正常闭合标签解析主路径。

### 3) 修复后验证
执行命令：

```powershell
& 'D:\go_install\go\bin\go.exe' test ./internal/openai -run "TestNormalizeAssistantHistoryContent(UnclosedToolCallTagStrippedInNormalMode|MalformedToolCallStrippedInNormalMode|MalformedToolCallPreservedInDebugMode|MalformedToolNameWrapperUsesInnerName|Idempotent)$" -v
```

结果摘要：
- **PASS**（目标新增用例 + 相关归一化回归均通过）

## 回归测试
执行命令：

```powershell
& 'D:\go_install\go\bin\go.exe' test ./internal/openai -v
& 'D:\go_install\go\bin\go.exe' test ./internal/http -run "TestChatCompletionsStreamFallback(DetectsXMLTagToolCallWithoutTools|KeepsPlainTextWithoutTools)$" -v
& 'D:\go_install\go\bin\go.exe' test ./... -v
```

结果摘要：
- `internal/openai`：PASS
- `internal/http`（相关流式 fallback 用例）：PASS
- 全仓 `./...`：PASS

## 结论
- 已修复“未闭合 `</tool_call>` 非标 tool call 无法兜底归一化”问题。
- 示例场景在进入上下文前会被收敛为 `assistant_tool_call: Bash`，避免原始标签污染 prompt。
