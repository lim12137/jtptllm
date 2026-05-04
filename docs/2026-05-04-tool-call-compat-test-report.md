# 2026-05-04 Tool Call Compatibility Test Report

## Test Command

```powershell
go test ./internal/openai ./internal/http
```

## Result Summary

- `internal/openai`: passed
- `internal/http`: passed

## Coverage Summary

- Verified bracket-style tool call parsing using `[function_calls]` and `[call:tool_name]...[/call]`
- Verified non-stream chat completion responses emit `tool_calls` when the upstream text contains tool call blocks
- Verified streaming chat completion responses emit `delta.tool_calls` for `fast` and keep raw text for non-compat models
- Verified compatibility is restricted to `qwen3`/`fast` and `deepseek v3.2`
- Verified specific fallback handling for `write_to_file` and `replace_in_file`
- Confirmed existing HTTP handler tests still pass after OpenAI compatibility changes
