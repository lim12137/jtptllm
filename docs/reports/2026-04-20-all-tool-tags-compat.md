# 2026-04-20 all-tool-tags-compat 修复报告

## 1. 最近 err 中的 malformed tool-call 样例

来源：`bin/logs/proxy_8022.err`

- 行 424（参数子标签多一个 closing tag）：
  - `...<tool_call><Read><file_path>... </file_path></file_path></Read></tool_call>`
- 行 404 / 412 / 414（`tool_name` 层多一个 closing tag）：
  - `...<tool_call><tool_name>Glob</tool_name>{"pattern":"*.md"}</tool_name></tool_call>`
- 行 404（`stop` 返回中直接外露标签风险）：
  - 生成内容包含原始 `<tool_call>...</tool_call>` 片段，若未被兼容解析会直接进入用户可见文本。

结论：当前兼容已覆盖 `tool_call/tool_name` 多种畸形，但参数 XML 子标签（如 `file_path/pattern`）closing tag 畸形仍存在缺口，导致无法解析为工具调用，存在外露风险。

## 2. TDD 与实现摘要

先补失败测试，再做最小修复。

- 新增失败测试（参数子标签层）：
  - `xml args extra closing subtag`
  - `xml args missing closing subtag`
  - `xml args mismatched closing subtag`
  - 位置：`internal/openai/compat_test.go`
- 新增无外露回归测试（流式路径）：
  - `TestChatCompletionsStreamFallbackDetectsMalformedXMLArgTagsWithoutTools`
  - 位置：`internal/http/handlers_test.go`
- 最小修复：
  - 在 `parseTaggedToolArgumentsLoose` 中新增宽松参数解析兜底 `parseMalformedXMLArgsLoose`。
  - 仅在严格 JSON/XML 解析失败时启用。
  - 可兼容参数子标签 closing tag 多一个/少一个/错配的常见畸形，优先提取 `<key>value` 形式参数，避免 tool-call 片段外露。
  - 位置：`internal/openai/compat.go`

## 3. 测试命令与结果摘要

1. 失败用例验证（改前）

```powershell
& 'D:\go_install\go\bin\go.exe' test ./internal/openai -run TestParseToolSentinelTagWrappedToolCallMalformedDelimitersCompatibility -v
```

结果：失败（新增 3 个子用例 `toolcalls=0`）。

2. 定向回归（改后）

```powershell
& 'D:\go_install\go\bin\go.exe' test ./internal/openai -run TestParseToolSentinelTagWrappedToolCallMalformedDelimitersCompatibility -v
& 'D:\go_install\go\bin\go.exe' test ./internal/http -run "TestChatCompletionsStreamFallbackDetects(XMLTagToolCallWithoutTools|MalformedXMLArgTagsWithoutTools)$" -v
```

结果：全部通过。

3. 全量回归

```powershell
& 'D:\go_install\go\bin\go.exe' test ./... -v
```

结果：全部通过。

