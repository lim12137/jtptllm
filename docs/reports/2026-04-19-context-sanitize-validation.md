# 2026-04-19 Context Sanitize Validation

## Scope

- Implement minimal sanitize/normalize for assistant history before prompt backfill.
- Entry points:
  - `internal/openai/compat.go` chat inbound (`chatMessagesToPrompt`)
  - `internal/openai/compat.go` responses inbound (`PromptFromResponses`)
- Rules covered:
  - strip `<thinking>...</thinking>`
  - detect and sanitize `<tool_call>...</tool_call>` / sentinel-style traces for assistant history
  - preserve natural language around tool-call wrappers
  - summarize assistant-only tool-call blocks as `assistant_tool_call: <name>`
  - avoid applying the same sanitize rules to user content

## Commands

```powershell
go test ./internal/openai -v
go test ./internal/http -v
go test ./... -v
```

## Result Summary

- `go test ./internal/openai -v`: PASS
  - Includes new sanitize tests:
    - assistant only tool_call -> summary
    - assistant mixed natural language + tool_call -> natural language kept, tool wrapper removed
    - `<thinking>` stripped
    - user content unchanged
    - responses inbound path coverage (messages and input-array forms)
- `go test ./internal/http -v`: PASS
- `go test ./... -v`: PASS

## Notes

- Existing unrelated uncommitted changes were kept intact.
- Changes were limited to the requested sanitize/normalize behavior area and validation report.
