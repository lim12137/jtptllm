# Toolcall Tag Boundary Tests Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add regression tests that lock down current boundary behavior for tag-style tool call parsing without changing production code.

**Architecture:** Keep production parsing and response handling unchanged. Add focused tests in `internal/openai/compat_test.go` and `internal/http/handlers_test.go` to prove three behaviors: plain text around a tag-wrapped tool call is preserved, malformed tag blocks safely fall back to plain text, and fragmented `/v1/responses` stream input still emits `response.function_call_arguments.*` after final aggregation.

**Tech Stack:** Go 1.22, standard `testing` package, existing HTTP/SSE test helpers.

---

### Task 1: Parser Boundary Tests

**Files:**
- Modify: `internal/openai/compat_test.go`
- Test: `internal/openai/compat_test.go`

**Step 1: Write the first regression test**

Add a test named `TestParseToolSentinelTagWrappedToolCallPreservesOuterText` that feeds:

```go
"before <tool_call><multi_tool_use.parallel>{\"tool_uses\":[{\"recipient_name\":\"functions.shell_command\",\"parameters\":{\"command\":\"Get-Date\"}}]}</multi_tool_use.parallel></tool_call> after"
```

Assert:
- `len(res.ToolCalls) == 1`
- `res.ToolCalls[0].Name == "multi_tool_use.parallel"`
- `res.Content == "before after"`

**Step 2: Run the focused test**

Run: `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run TestParseToolSentinelTagWrappedToolCallPreservesOuterText -v`

Expected:
- `PASS` if current parser already preserves outer text
- `FAIL` only if the observed behavior differs, in which case stop and open a separate fix task

**Step 3: Write the malformed-tag fallback test**

Add a test named `TestParseToolSentinelMalformedTagWrappedToolCallFallsBackToText` that feeds an invalid tag block such as:

```go
"prefix <tool_call><multi_tool_use.parallel>{\"tool_uses\":[}</multi_tool_use.parallel></tool_call> suffix"
```

Assert:
- `len(res.ToolCalls) == 0`
- `res.Content` stays equal to the trimmed original input

**Step 4: Run the focused malformed-tag test**

Run: `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run TestParseToolSentinelMalformedTagWrappedToolCallFallsBackToText -v`

Expected:
- `PASS`
- If `FAIL`, stop and open a separate fix task instead of changing production code in this task

### Task 2: Fragmented Stream Regression Test

**Files:**
- Modify: `internal/http/handlers_test.go`
- Test: `internal/http/handlers_test.go`

**Step 1: Write the fragmented stream test**

Add a test named `TestResponsesStreamFragmentedTagWrappedToolCallUsesFunctionEventsNotOutputText`.

Construct SSE input using multiple `data:` events whose `message.text` fragments join into:

```go
"<tool_call><multi_tool_use.parallel>{\"tool_uses\":[{\"recipient_name\":\"functions.shell_command\",\"parameters\":{\"command\":\"Get-Date\"}}]}</multi_tool_use.parallel></tool_call>"
```

Assert:
- HTTP status is `200`
- zero `response.output_text.delta`
- at least one `response.function_call_arguments.delta`
- exactly one `response.function_call_arguments.done`
- `done[0].Data["name"] == "multi_tool_use.parallel"`

**Step 2: Run the focused fragmented-stream test**

Run: `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestResponsesStreamFragmentedTagWrappedToolCallUsesFunctionEventsNotOutputText -v`

Expected:
- `PASS` if stream aggregation already preserves the existing behavior
- `FAIL` means open a separate implementation task

### Task 3: Verification And Report

**Files:**
- Create: `docs/reports/2026-03-20-toolcall-tag-boundary-tests.md`
- Modify: `docs/reports/2026-03-20-toolcall-tag-boundary-tests.md`
- Test: `internal/openai/compat_test.go`
- Test: `internal/http/handlers_test.go`

**Step 1: Run package-level verification**

Run:
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v`
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v`

Expected:
- both commands pass

**Step 2: Run smoke verification**

Run:
- `powershell -File scripts/codex_toolcall_smoke.ps1`

Expected:
- `200` responses for `/v1/chat/completions` and `/v1/responses`
- tool call flow still succeeds

**Step 3: Write verification report**

Create `docs/reports/2026-03-20-toolcall-tag-boundary-tests.md` containing:
- execution date/time
- exact test commands
- pass/fail result summary for each command
- a short note that this task intentionally did not change production code

**Step 4: Commit**

Run:

```bash
git add internal/openai/compat_test.go internal/http/handlers_test.go docs/plans/2026-03-20-toolcall-tag-boundary-tests-plan.md docs/reports/2026-03-20-toolcall-tag-boundary-tests.md
git commit -m "test: add tag toolcall boundary coverage"
```
