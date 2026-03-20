# Usage CharCount Fallback Validation (2026-03-19)

## Goal

When upstream `/run` does not provide `usage`, return a non-zero `usage` by using Unicode character count (rune count) as a lightweight fallback:

- Chat Completions non-stream: `usage.prompt_tokens/completion_tokens/total_tokens`
- Responses non-stream: `usage.input_tokens/output_tokens/total_tokens`
- Responses stream: `response.completed.response.usage.*`

If upstream does provide `usage`, it must be preserved (no override).

## Test Commands

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v
```

## Result Summary

- `go test ./internal/http -v`: PASS
  - Key cases:
    - `TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing`
    - `TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing`
    - `TestResponsesStreamFallsBackToCharCountUsageWhenMissing`
    - Existing passthrough cases remain green:
      - `TestChatCompletionsNonStreamPassesThroughUsage`
      - `TestResponsesNonStreamPassesThroughUsage`
      - `TestResponsesStreamPassesThroughUsage`
- `go test ./internal/openai -v`: PASS

