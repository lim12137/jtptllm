# 2026-03-20 标签式工具调用解析测试报告

## 复验信息

- 执行时间：`2026-03-20 15:51:15 +08:00`
- 复验目的：独立重跑修复后的验证命令，确认结果为最新且可复现。

## 执行命令

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
powershell -File scripts/codex_toolcall_smoke.ps1
```

## 结果摘要

- `go test ./internal/openai -v`：通过（`PASS`，`ok github.com/lim12137/jtptllm/internal/openai (cached)`）。
- `go test ./internal/http -v`：通过（`PASS`，`ok github.com/lim12137/jtptllm/internal/http (cached)`）。
- `TestResponsesStreamTagWrappedToolCallUsesFunctionEventsNotOutputText`：在 `./internal/http -v` 中执行并通过。
- `powershell -File scripts/codex_toolcall_smoke.ps1`：通过；`/v1/chat/completions` 与 `/v1/responses` 均返回 `200`，并返回可解析的工具调用（`get_weather` + `{"location":"Paris"}`）。

## 结论

本次独立复验 3 条命令全部通过，标签式工具调用解析相关回归用例通过，当前报告已更新为最新执行结果。
