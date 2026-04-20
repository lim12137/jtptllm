# 2026-04-20 multi-tool-call-warning 测试报告

## 变更目标
- 按 TDD 为“多工具调用候选块 + 畸形标签”补充失败测试。
- 在解析阶段实现部分恢复：存在可解析候选块时继续提取一个有效工具调用。
- 当检测到 malformed block 且消息中存在多个工具调用候选块时，在返回内容中附加清晰提示，建议“请一次只调用一个工具，一个一个地调用”。

## 测试命令
```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/openai -run "TestParseToolSentinelMalformedTagWrappedToolCall(RecoversLaterCandidateWithWarning|WarnsWhenMixedWithValidCandidate)" -v
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/openai -v
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./... -v
```

## 结果摘要
- 定向新增用例：2/2 通过。
- `internal/openai` 全量：通过。
- 全仓 `go test ./... -v`：通过。
- 未发现与 `internal/http` 流式/非流式工具调用映射相关的回归失败。
