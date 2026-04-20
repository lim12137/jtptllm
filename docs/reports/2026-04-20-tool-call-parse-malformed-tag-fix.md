# 2026-04-20 tool-call-parse 畸形标签容错修复验证报告

## 变更目标
修复 `tool_call / tool_name` 标签在畸形输入下的解析漏网，覆盖以下场景：
- 多一个 `</tool_name>`
- 缺少 `</tool_name>`
- 多一个 `</tool_call>`
- 缺少 `</tool_call>`
- 标签错配（如 `</tool_name>` 错关 `<Read>`）
- `<tool_call ...>` 含属性或额外空格

## TDD 执行记录

### RED：先加失败用例并单测确认失败
命令：

```powershell
$env:GOROOT='D:\go_install\go'; $env:GOPATH='D:\gopath'; $env:Path='D:\go_install\go\bin;' + $env:Path; go test ./internal/openai -run TestParseToolSentinelTagWrappedToolCallMalformedDelimitersCompatibility -v
```

结果摘要：`FAIL`（符合预期）
- `extra_closing_tool_name`: `toolcalls=0`
- `missing_closing_tool_name`: `toolcalls=0`
- `extra_closing_tool_call`: `content="</tool_call>"`
- `missing_closing_tool_call`: `toolcalls=0`
- `mismatched_inner_closing_tag`: `toolcalls=0`
- `tool_call_open_tag_with_attributes_or_spaces`: `toolcalls=0`

### GREEN：最小修复后重跑新增用例
命令：

```powershell
$env:GOROOT='D:\go_install\go'; $env:GOPATH='D:\gopath'; $env:Path='D:\go_install\go\bin;' + $env:Path; go test ./internal/openai -run TestParseToolSentinelTagWrappedToolCallMalformedDelimitersCompatibility -v
```

结果摘要：`PASS`
- 6 个子场景全部通过。

### 回归验证：相关包 + 全量
命令：

```powershell
$env:GOROOT='D:\go_install\go'; $env:GOPATH='D:\gopath'; $env:Path='D:\go_install\go\bin;' + $env:Path; go test ./internal/openai -v
$env:GOROOT='D:\go_install\go'; $env:GOPATH='D:\gopath'; $env:Path='D:\go_install\go\bin;' + $env:Path; go test ./... -v
```

结果摘要：
- `go test ./internal/openai -v`：`PASS`
- `go test ./... -v`：`PASS`

