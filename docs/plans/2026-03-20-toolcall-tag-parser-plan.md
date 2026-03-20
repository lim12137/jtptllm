# Toolcall Tag Parser Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Support parsing tag-style tool calls like `<tool_call><multi_tool_use.parallel>...</multi_tool_use.parallel></tool_call>` so `/v1/responses` stream emits function call events instead of plain output text.

**Architecture:** Keep the existing `handlers.go` stream assembly and `openai.ParseToolSentinel` entrypoint unchanged. Add one minimal parsing branch in `internal/openai/compat.go` for the observed tag-style format, then verify both parser-level and `/v1/responses` streaming behavior with focused regression tests.

**Tech Stack:** Go 1.22, standard `testing` package, existing OpenAI compatibility and HTTP handler tests.

---

### Task 1: Add parser regression test for tag-style tool calls

**Files:**
- Modify: `internal/openai/compat_test.go`
- Test: `internal/openai/compat_test.go`

**Step 1: Write the failing test**

Add a test that passes `<tool_call><multi_tool_use.parallel>{"tool_uses":[{"recipient_name":"functions.shell_command","parameters":{"command":"Get-Date"}}]}</multi_tool_use.parallel></tool_call>` into `ParseToolSentinel` and asserts:
- exactly one tool call is returned,
- the tool name is `multi_tool_use.parallel`,
- the arguments preserve the inner JSON payload.

**Step 2: Run test to verify it fails**

Run: `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run TestParseToolSentinelTagWrappedToolCall -v`
Expected: FAIL because tag-style format is not parsed yet.

**Step 3: Write minimal implementation**

No production code in this task.

**Step 4: Re-run test and keep it failing until implementation task**

Run: `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run TestParseToolSentinelTagWrappedToolCall -v`
Expected: still FAIL with parser mismatch, confirming red state.

**Step 5: Commit**

Do not commit yet; continue to Task 2.

### Task 2: Implement minimal tag-style parsing in compatibility layer

**Files:**
- Modify: `internal/openai/compat.go`
- Test: `internal/openai/compat_test.go`

**Step 1: Write the minimal implementation**

Add one parser branch in `ParseToolSentinel` or its helpers to recognize the observed shape:
- outer `<tool_call>...</tool_call>` wrapper,
- one direct child tag whose name is the tool name,
- child body is JSON arguments string.

Keep scope narrow to the observed format. Do not introduce a generic XML parser if simple string scanning is sufficient.

**Step 2: Run the focused parser test to verify it passes**

Run: `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run TestParseToolSentinelTagWrappedToolCall -v`
Expected: PASS.

**Step 3: Run the full package tests for parser coverage**

Run: `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v`
Expected: PASS.

**Step 4: Commit**

Do not commit yet; continue to Task 3.

### Task 3: Add `/v1/responses` stream regression test for tag-style tool calls

**Files:**
- Modify: `internal/http/handlers_test.go`
- Test: `internal/http/handlers_test.go`

**Step 1: Write the failing test**

Add a stream regression test that feeds tag-style tool-call text through the existing `/v1/responses` stream path and asserts:
- no `response.output_text.delta` event is emitted for the tool-call payload,
- `response.function_call_arguments.delta` and `response.function_call_arguments.done` events are emitted,
- emitted tool name matches `multi_tool_use.parallel`.

**Step 2: Run test to verify it fails or is blocked before parser fix is present**

Run: `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestResponsesStreamTagWrappedToolCallUsesFunctionEventsNotOutputText -v`
Expected: FAIL before parser support, PASS after Task 2.

**Step 3: Adjust only the test scaffolding if needed**

Keep production behavior in handlers untouched. Only adapt test fixtures to feed the observed tag-style payload into the existing code path.

**Step 4: Run the focused stream test to verify it passes**

Run: `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestResponsesStreamTagWrappedToolCallUsesFunctionEventsNotOutputText -v`
Expected: PASS.

**Step 5: Commit**

Do not commit yet; continue to Task 4.

### Task 4: Verify packages and write test report

**Files:**
- Create: `docs/reports/2026-03-20-toolcall-tag-parser-test.md`
- Test: `internal/openai/compat_test.go`
- Test: `internal/http/handlers_test.go`

**Step 1: Run verification commands**

Run:
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v`
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v`

Expected: both PASS.

**Step 2: Write the report**

Create `docs/reports/2026-03-20-toolcall-tag-parser-test.md` with:
- executed commands,
- pass/fail summary,
- note that coverage includes parser-level tag parsing and `/v1/responses` stream regression.

**Step 3: Optional smoke validation**

If local proxy process is already running and stable, run:
- `powershell -File scripts/codex_toolcall_smoke.ps1`

Record its result in the same report, but do not start or restart the proxy just for this task.

**Step 4: Final verification**

Run: `git diff -- internal/openai/compat.go internal/openai/compat_test.go internal/http/handlers_test.go docs/reports/2026-03-20-toolcall-tag-parser-test.md docs/plans/2026-03-20-toolcall-tag-parser-plan.md`
Expected: diff only contains the intended minimal parser/test/report changes.

**Step 5: Commit**

Run:
- `git add internal/openai/compat.go internal/openai/compat_test.go internal/http/handlers_test.go docs/reports/2026-03-20-toolcall-tag-parser-test.md docs/plans/2026-03-20-toolcall-tag-parser-plan.md`
- `git commit -m "fix: support tag-style tool call parsing"`
