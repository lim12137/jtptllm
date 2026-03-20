# Usage Fallback K2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When upstream does not provide `usage`, estimate usage as `runeCount * 2` for chat/responses non-stream and responses stream paths, while preserving upstream usage passthrough behavior.

**Architecture:** Keep usage source priority unchanged: upstream passthrough first, fallback second. Only update fallback builders in `internal/openai/compat.go` so all existing handler call sites inherit `K=2` consistently. Validate with focused failing tests first, then package-level regression and a docs report.

**Tech Stack:** Go 1.22, standard `testing` package, existing HTTP/SSE test helpers.

---

### Task 1: Write Failing Tests For K2 Fallback

**Files:**
- Modify: `internal/openai/compat_test.go`
- Modify: `internal/http/handlers_test.go`
- Test: `internal/openai/compat_test.go`
- Test: `internal/http/handlers_test.go`

**Step 1: Add openai-layer failing test**

In `internal/openai/compat_test.go`, add a focused test (for example `TestUsageFromCharCountAppliesK2Multiplier`) to assert:
- `ChatUsageFromCharCount("user: hi", "智能体输出文本")` returns `prompt_tokens=16`, `completion_tokens=14`, `total_tokens=30`
- `ResponsesUsageFromCharCount("hi", "回应")` returns `input_tokens=4`, `output_tokens=4`, `total_tokens=8`

These assertions should fail against current x1 behavior.

**Step 2: Update handler fallback expectations to K2**

In `internal/http/handlers_test.go`, update fallback assertions:
- `TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing`
  - from `8/7/15` to `16/14/30`
- `TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing`
  - from `2/2/4` to `4/4/8`
- `TestResponsesStreamFallsBackToCharCountUsageWhenMissing`
  - from `2/5/7` to `4/10/14`

Do not change passthrough tests.

**Step 3: Run focused tests and verify RED**

Run:
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run TestUsageFromCharCountAppliesK2Multiplier -v`
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run 'TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesStreamFallsBackToCharCountUsageWhenMissing' -v`

Expected:
- tests fail because production fallback still uses x1.

### Task 2: Implement Minimal K2 Fallback

**Files:**
- Modify: `internal/openai/compat.go`
- Test: `internal/openai/compat_test.go`
- Test: `internal/http/handlers_test.go`

**Step 1: Apply minimal implementation**

In `internal/openai/compat.go`, update only fallback usage builders:
- `ChatUsageFromCharCount`
- `ResponsesUsageFromCharCount`

Implement `tokens = utf8.RuneCountInString(value) * 2`.
Keep output shape unchanged.
Do not modify upstream passthrough logic in handlers.

**Step 2: Run focused tests and verify GREEN**

Run:
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -run TestUsageFromCharCountAppliesK2Multiplier -v`
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run 'TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesStreamFallsBackToCharCountUsageWhenMissing' -v`

Expected:
- all focused tests pass.

### Task 3: Protect Passthrough Behavior And Package Regression

**Files:**
- Modify: `internal/http/handlers_test.go` (only if needed for assertions/messages)
- Test: `internal/http/handlers_test.go`
- Test: `internal/openai/compat_test.go`

**Step 1: Verify passthrough tests remain green**

Run:
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run 'TestChatCompletionsNonStreamPassesThroughUsage|TestResponsesNonStreamPassesThroughUsage|TestResponsesStreamPassesThroughUsage' -v`

Expected:
- all passthrough tests pass unchanged.

**Step 2: Run package-level regression**

Run:
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/openai -v`
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v`

Expected:
- both packages pass.

### Task 4: Report And Commit

**Files:**
- Create: `docs/reports/2026-03-20-usage-fallback-k2-test.md`
- Modify: `docs/reports/2026-03-20-usage-fallback-k2-test.md`
- Modify: `internal/openai/compat.go`
- Modify: `internal/openai/compat_test.go`
- Modify: `internal/http/handlers_test.go`
- Modify: `docs/plans/2026-03-20-usage-fallback-k2-plan.md`

**Step 1: Write verification report**

Create `docs/reports/2026-03-20-usage-fallback-k2-test.md` with:
- execution timestamp
- exact commands run
- pass/fail summary per command
- explicit statement: fallback now uses `runeCount * 2` only when upstream usage is missing
- explicit statement: passthrough usage tests remain green

**Step 2: Commit**

Run:

```bash
git add internal/openai/compat.go internal/openai/compat_test.go internal/http/handlers_test.go docs/plans/2026-03-20-usage-fallback-k2-plan.md docs/reports/2026-03-20-usage-fallback-k2-test.md
git commit -m "fix: apply k2 multiplier to usage charcount fallback"
```
