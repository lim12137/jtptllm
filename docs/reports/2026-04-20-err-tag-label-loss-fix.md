# 2026-04-20 err 标签丢失修复报告

## 背景与定位

用户反馈“最新测试在 err 里有标签丢失”。先在 stderr 日志中定位到对应记录：

- 命令：

```powershell
rg -n --max-count 20 "assistant_tool_call: tool_name|assistant_tool_call: unknown|assistant_tool_call:" bin/logs/proxy_8022.err
```

- 结果摘要：
  - 命中 `bin/logs/proxy_8022.err:224`
  - 日志中可见原始片段包含：
    - `<tool_call><tool_name>Glob</tool_name>{"pattern":"CODEBUDDY.md"}</tool_name></tool_call>`
    - 历史压缩结果为：`assistant_tool_call: tool_name`
  - 预期应为：`assistant_tool_call: Glob`

## 根因分析（gstack-investigate + systematic-debugging）

- 丢标签环节：`internal/openai/compat.go` 的 `inferToolNameFromArtifact`
- 具体机制：
  - 对 malformed `tool_call` 块，`ParseToolSentinel` 解析失败后进入回退逻辑。
  - 回退正则 `taggedToolNameRe` 抓的是 `<tool_call>` 内第一层标签名，得到的是字面量 `tool_name`，不是 `<tool_name>...</tool_name>` 中的值。
  - 于是历史归一化链路把真实工具名丢失，输出 `assistant_tool_call: tool_name`。

## TDD 过程

### 1) 先加失败测试（RED）

- 新增测试：
  - `TestNormalizeAssistantHistoryContentMalformedToolNameWrapperUsesInnerName`
- 用例输入：
  - `<tool_call><tool_name>Glob</tool_name>{"pattern":"CODEBUDDY.md"}</tool_name></tool_call>`
- 期望输出：
  - `assistant_tool_call: Glob`

- 命令（修复前）：

```powershell
& 'C:/Users/Administrator/.tools/go1.22.12/go/bin/go.exe' test ./internal/openai -run TestNormalizeAssistantHistoryContentMalformedToolNameWrapperUsesInnerName -v
```

- 结果摘要（预期失败）：
  - `FAIL`
  - `got="assistant_tool_call: tool_name" want="assistant_tool_call: Glob"`

### 2) 最小修复（GREEN）

- 仅修改回退提取，不改主解析流程：
  - 在 `inferToolNameFromArtifact` 中，优先匹配 `<tool_name>...</tool_name>` 的值，再走原有 `taggedToolNameRe`。
  - 新增正则：`toolNameValueRe`

- 命令（修复后）：

```powershell
& 'C:/Users/Administrator/.tools/go1.22.12/go/bin/go.exe' test ./internal/openai -run TestNormalizeAssistantHistoryContentMalformedToolNameWrapperUsesInnerName -v
```

- 结果摘要：
  - `PASS`

## 回归验证

- 命令：

```powershell
& 'C:/Users/Administrator/.tools/go1.22.12/go/bin/go.exe' test ./internal/openai -v
& 'C:/Users/Administrator/.tools/go1.22.12/go/bin/go.exe' test ./internal/http -v
& 'C:/Users/Administrator/.tools/go1.22.12/go/bin/go.exe' test ./... -v
```

- 结果摘要：
  - 全部 `PASS`
  - 并发相关用例（位于 `internal/http`）通过：
    - `TestGlobalConcurrencyGateSeventeenthRequestWaits`
    - `TestGlobalConcurrencyGateSharedByChatAndResponses`
    - `TestGlobalConcurrencyGateReleasesOnError`

## 变更文件

- `internal/openai/compat.go`
- `internal/openai/compat_test.go`
- `docs/reports/2026-04-20-err-tag-label-loss-fix.md`

