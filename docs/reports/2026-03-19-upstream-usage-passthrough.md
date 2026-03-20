# 上游 Usage 透传验证报告 (2026-03-19)

## 目标

将上游 `/run` 返回的 `usage`（如存在）透传到 OpenAI 兼容接口：

- 非流式 `/v1/responses` 响应的 `usage`
- 流式 `/v1/responses` 的 `response.completed.response.usage`
- 非流式 `/v1/chat/completions` 响应的 `usage`（可透传则一并支持）

上游若无 `usage`：保持现状（`/v1/responses` 流式仍输出带 0 的 usage 字段，满足事件结构要求；非流式则保持默认值），不破坏协议结构。

## 测试命令

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v
```

## 结果摘要

- `go test ./internal/http -v`: PASS
  - 覆盖用例：
    - `TestChatCompletionsNonStreamPassesThroughUsage`
    - `TestResponsesNonStreamPassesThroughUsage`
    - `TestResponsesStreamPassesThroughUsage`
    - `TestResponsesStreamConformsToResponseEvents`
- `go test ./internal/openai -v`: PASS
  - 覆盖用例：
    - `TestNormalizeUsagePassthroughMappings`

