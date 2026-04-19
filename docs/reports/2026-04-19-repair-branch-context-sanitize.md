# 2026-04-19 Repair Branch Context Sanitize Report

## Scope
- Task: `repair branch => sanitize before backfill`
- Area: `internal/openai` prompt construction compatibility layer (shared by chat/responses)
- Goal: sanitize assistant history before prompt backfill, preserve user text, keep behavior idempotent.

## Implementation Summary
- Assistant-only sanitize is applied in the common prompt path:
  - `messageToPromptLine()` -> `normalizeMessageContentForPrompt()` -> `normalizeAssistantHistoryContent()`
  - shared by `PromptFromChat()` and `PromptFromResponses()`.
- Sanitization behavior (assistant history only):
  - remove `<thinking>...</thinking>` and self-closing `<thinking/>`
  - normalize raw tool-call artifacts (`<tool_call>...</tool_call>`, `<<<TC>>>...<<<END>>>`)
  - tool-call only content => stable summary: `assistant_tool_call: <name>`
  - mixed natural language + tool-call artifacts => keep natural-language body and strip protocol wrappers
  - idempotence preserved (same normalized output on repeated runs)
- Additional hardening for mixed sentinel text:
  - in `normalizeAssistantHistoryContent`, when sentinel is embedded in surrounding natural text, preserve outside text instead of collapsing to tool-summary only.

## Tests Added / Covered
- New tests in `internal/openai/compat_test.go`:
  - `TestPromptFromChatAssistantSentinelOnlySummarized`
  - `TestPromptFromChatAssistantMixedNaturalLanguageAndSentinel`
  - `TestPromptFromChatRepairBranchOutputBackfillSanitized`
  - `TestNormalizeAssistantHistoryContentIdempotent`
- Existing tests already cover:
  - tool_call-only assistant text summarized
  - mixed natural language + tool_call keeps natural language
  - `<thinking>` stripped
  - user tool-like text unchanged
  - responses path sanitizes assistant-only history

## Commands Executed
1. `go test ./internal/openai -v`
2. `go test ./internal/http -v`
3. `go test ./... -v`

## Result Summary
- `go test ./internal/openai -v`: PASS
- `go test ./internal/http -v`: PASS
- `go test ./... -v`: PASS

## Risks / Notes
- Mixed sentinel stripping currently preserves surrounding text but may leave extra join-point spaces in some cases; semantic text is preserved and sanitize stays idempotent.
