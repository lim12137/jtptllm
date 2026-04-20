# 2026-04-20 chat.completions 流式 usage 修复报告

## 结论

- 根因与假设一致：`/v1/chat/completions` 的 `stream=true` 分支此前只输出文本/工具调用 chunk 和 `[DONE]`，最终 chunk 不包含 `usage`，因此依赖最终 SSE chunk 的 CLI 无法显示 token 使用量。
- 已按 TDD 修复：
  - 先新增失败测试，覆盖 chat 流式最终 chunk 透传上游 usage。
  - 再新增失败测试，覆盖上游缺失 usage 时回退为字符数估算。
  - 最小实现只补齐 chat 流式最终 chunk 的 `usage`，不改变现有 SSE 基本顺序。

## 修改文件

- `internal/http/handlers.go`
- `internal/http/handlers_test.go`

## 测试命令

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/http -run 'TestChatCompletionsStream(PassesThroughUsage|FallsBackToCharCountUsageWhenMissing)' -count=1
```

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/http -count=1
```

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./... -count=1
```

## 结果摘要

- `TestChatCompletionsStreamPassesThroughUsage`: 通过
- `TestChatCompletionsStreamFallsBackToCharCountUsageWhenMissing`: 通过
- `go test ./internal/http -count=1`: 通过
- `go test ./... -count=1`: 通过

## 行为说明

- 非工具调用的 chat 流式响应：
  - 若上游流末尾带 `usage`，则代理在最终 `chat.completion.chunk` 中透传 `usage`。
  - 若上游未带 `usage`，则代理按现有 fallback 逻辑生成 `prompt_tokens` / `completion_tokens` / `total_tokens`。
- 工具调用的 chat 流式响应：
  - 最终 `finish_reason=tool_calls` 的 chunk 同样携带 `usage`。
