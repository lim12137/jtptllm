# Token Usage Estimator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current `runeCount * 2` token fallback with a pure-Go heuristic estimator that stays low-dependency, preserves upstream `usage` passthrough priority, and materially improves fallback accuracy for chat and responses paths.

**Architecture:** Keep handler-level `usage` precedence unchanged: upstream `usage` remains authoritative and fallback only runs when usage is absent. Implement a shared text estimator in `internal/openai/compat.go`, use it from both chat and responses fallback builders, and add a lightweight chat prompt overhead layer that accounts for role lines, tool-prefix payloads, and model markers without changing handler signatures.

**Tech Stack:** Go 1.22, standard library only, existing `testing` package, existing HTTP/SSE test helpers, Markdown reports under `docs/reports`.

---

## File Map

- `internal/openai/compat.go`
  - Replace the fixed-multiplier fallback internals with a shared heuristic estimator.
  - Keep exported fallback entrypoints stable: `ChatUsageFromCharCount` and `ResponsesUsageFromCharCount`.
- `internal/openai/compat_test.go`
  - Add estimator-focused table tests and fallback contract tests.
- `internal/http/handlers_test.go`
  - Update fallback assertions to reflect new estimator values.
  - Preserve passthrough tests so upstream `usage` still wins.
- `docs/reports/2026-04-20-token-usage-estimator-validation.md`
  - Record exact commands run and a concise result summary.
- `docs/plans/2026-04-20-token-usage-estimator-implementation-plan.md`
  - This plan; do not modify during implementation unless scope changes are explicitly approved.

## Constraints To Preserve

- Do not change `internal/http/handlers.go` usage-source priority.
- Do not add third-party tokenizer libraries or external services.
- Do not add model-specific tokenizer tables.
- Do not change HTTP response shapes for chat or responses usage fields.
- Do not remove or weaken existing passthrough tests.

## Calibration Fixtures

Use fixed sample texts in `internal/openai/compat_test.go` so the estimator has deterministic guardrails. The implementation should tune weights against these fixtures, not against ad hoc manual inspection during coding.

Required sample classes:

- Pure Chinese short and long text
- Pure English short and long text
- Mixed Chinese/English text
- JSON payload with nesting
- Code snippet with punctuation and indentation
- XML/tool schema text
- Multi-line chat prompt text with `system:`, `user:`, `assistant:` prefixes

Each fixture should include:

- input text
- exact expected token estimate

All fixture tests must use exact expected values by the time the implementation commit is created. Temporary bounded assertions are allowed only during the initial RED step and must be removed before Task 4 completes.

### Task 1: Lock Down Estimator Coverage With Failing Unit Tests

**Files:**
- Modify: `internal/openai/compat_test.go`
- Test: `internal/openai/compat_test.go`

- [ ] **Step 1: Add fixture-driven tests for raw text estimation**

Add a table-driven test named `TestEstimateTextTokensFixtures` with fixtures covering the required sample classes.

Use a shape like:

```go
func TestEstimateTextTokensFixtures(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{
			name: "english sentence",
			text: "Write a concise summary of this API response.",
			want: 12,
		},
		{
			name: "chinese sentence",
			text: "请总结这个接口返回的数据结构。",
			want: 14,
		},
		{
			name: "json payload",
			text: "{\"tool\":\"search\",\"args\":{\"query\":\"weather\",\"days\":3}}",
			want: 20,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := estimateTextTokens(tc.text)
			if got != tc.want {
				t.Fatalf("estimateTextTokens(%q)=%d want %d", tc.name, got, tc.want)
			}
		})
	}
}
```

Use real fixture values chosen from the confirmed spec and final coefficient set. Include at least seven fixture cases before implementation begins.

- [ ] **Step 2: Add chat prompt overhead tests**

Add focused tests for the prompt wrapper layer:

```go
func TestEstimateChatPromptTokensAddsRoleOverhead(t *testing.T) {
	prompt := "system: follow the schema strictly\nuser: hi"
	base := estimateTextTokens(prompt)
	got := estimateChatPromptTokens(prompt)
	if got <= base {
		t.Fatalf("chat prompt estimate=%d base=%d", got, base)
	}
}

func TestEstimateChatPromptTokensAddsToolPrefixOverhead(t *testing.T) {
	prompt := "system: You have tools available.\n<tool_call>{\"name\":\"search\"}</tool_call>\nuser: hi"
	plain := estimateChatPromptTokens("user: hi")
	got := estimateChatPromptTokens(prompt)
	if got <= plain {
		t.Fatalf("tool prompt estimate=%d plain=%d", got, plain)
	}
}
```

These tests must fail initially because the current implementation has no estimator helper or wrapper overhead logic.

- [ ] **Step 3: Add fallback contract tests that assert new values, not K=2**

Update or replace the current fallback tests so they assert the heuristic estimator outputs rather than the legacy `utf8.RuneCountInString(text) * 2` rule.

At minimum, add and keep these exact tests:

- `TestChatUsageFromCharCountUsesHeuristicEstimator`
- `TestResponsesUsageFromCharCountUsesHeuristicEstimator`
- `TestResponsesUsageFromCharCountHandlesEmptyStrings`

Use explicit expected values from the finalized coefficient set.

- [ ] **Step 4: Run the focused openai tests and verify RED**

Run:

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/openai -run 'TestEstimateTextTokensFixtures|TestEstimateChatPromptTokensAddsRoleOverhead|TestEstimateChatPromptTokensAddsToolPrefixOverhead|TestChatUsageFromCharCountUsesHeuristicEstimator|TestResponsesUsageFromCharCountUsesHeuristicEstimator|TestResponsesUsageFromCharCountHandlesEmptyStrings' -v
```

Expected:

- the new estimator helper tests fail because helper functions and coefficient-based behavior do not exist yet
- existing passthrough-only tests remain unaffected

### Task 2: Implement The Shared Heuristic Estimator In `compat.go`

**Files:**
- Modify: `internal/openai/compat.go`
- Test: `internal/openai/compat_test.go`

- [ ] **Step 1: Introduce a single statistics collector**

Add one internal scanner that walks a string once and collects the feature counts required by the spec. Keep the implementation private to `compat.go`.

Target shape:

```go
type tokenEstimateStats struct {
	cjkCount          int
	asciiLetterCount  int
	digitCount        int
	whitespaceCount   int
	punctCount        int
	jsonSyntaxCount   int
	xmlSyntaxCount    int
	newlineCount      int
	wordCount         int
	longWordCount     int
	roleLineCount     int
	modelMarkerCount  int
	toolPrefixSignals int
}

func collectTokenEstimateStats(text string) tokenEstimateStats
```

Implementation requirements:

- use a single linear scan plus lightweight state for ASCII word segmentation
- classify `\n` separately from generic whitespace
- count JSON syntax characters independently of generic punctuation
- count XML/tag syntax independently of generic punctuation
- detect role-line prefixes with anchored line checks such as `system:`, `user:`, `assistant:`, `tool:`
- detect model-marker presence from lines beginning with `**model = `
- detect tool-prefix signals from known prompt decorations already emitted by this repo, such as `<tool_call>`, XML-like wrappers, or serialized tool schema fragments

- [ ] **Step 2: Centralize coefficients and smoothing constants**

Declare all estimator constants together near the helper implementation. Do not scatter magic numbers through the fallback builders.

Required constant names:

- `minNonEmptyTokenEstimate`
- `cjkTokenWeight`
- `asciiWordWeight`
- `asciiLongWordExtra`
- `digitTokenWeight`
- `punctTokenWeight`
- `structureTokenWeight`
- `newlineTokenWeight`
- `shortTextBaseWeight`
- `roleLineOverheadWeight`
- `modelMarkerOverhead`
- `toolPrefixOverhead`

Coefficient rules:

- `cjkTokenWeight` must not simply mirror the old `*2` multiplier
- ASCII words must contribute primarily through `wordCount`
- long ASCII identifiers must add residual cost
- JSON/XML/code-heavy content must rise faster than plain prose
- empty string must still return `0`

- [ ] **Step 3: Implement `estimateTextTokens` and `estimateChatPromptTokens`**

Add two internal helpers:

```go
func estimateTextTokens(text string) int
func estimateChatPromptTokens(prompt string) int
```

Behavior requirements:

- `estimateTextTokens("") == 0`
- non-empty text always returns `>= 1`
- apply the coefficients from the shared stats collector
- include a short-text base so very small prompts like `hi` or `{}` are not underestimated
- `estimateChatPromptTokens` must call `estimateTextTokens(prompt)` and then add wrapper overhead using the collected role/tool/model-marker signals

- [ ] **Step 4: Switch the exported fallback builders to the new estimator**

Update only these functions:

- `ChatUsageFromCharCount`
- `ResponsesUsageFromCharCount`

Required behavior:

- keep function names stable for now, even though they no longer use char count
- `ChatUsageFromCharCount(prompt, completion)` must use `estimateChatPromptTokens(prompt)` for prompt usage and `estimateTextTokens(completion)` for completion usage
- `ResponsesUsageFromCharCount(input, output)` must use `estimateTextTokens` for both sides
- `total_tokens` remains the sum of prompt/input and completion/output

Do not change:

- `NormalizeChatUsage`
- `NormalizeResponsesUsage`
- any handler call sites

- [ ] **Step 5: Run focused openai tests and verify GREEN**

Run:

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/openai -run 'TestEstimateTextTokensFixtures|TestEstimateChatPromptTokensAddsRoleOverhead|TestEstimateChatPromptTokensAddsToolPrefixOverhead|TestChatUsageFromCharCountUsesHeuristicEstimator|TestResponsesUsageFromCharCountUsesHeuristicEstimator|TestResponsesUsageFromCharCountHandlesEmptyStrings' -v
```

Expected:

- all new estimator tests pass
- fallback contract tests pass with heuristic values

### Task 3: Update Handler-Level Fallback Expectations Without Touching Passthrough Priority

**Files:**
- Modify: `internal/http/handlers_test.go`
- Test: `internal/http/handlers_test.go`

- [ ] **Step 1: Update non-stream and stream fallback assertions**

Adjust the fallback tests that currently encode `runeCount * 2` expectations so they assert the new estimator-driven values instead.

Review and update at minimum:

- `TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing`
- `TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing`
- `TestResponsesStreamFallsBackToCharCountUsageWhenMissing`
- `TestChatCompletionsStreamFallsBackToCharCountUsageWhenMissing`

If helper names or test names are now misleading, keep coverage but prefer minimal rename churn unless it materially improves clarity.

- [ ] **Step 2: Preserve passthrough behavior with explicit focused checks**

Run passthrough-only tests without changing their assertions:

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/http -run 'TestChatCompletionsNonStreamPassesThroughUsage|TestResponsesNonStreamPassesThroughUsage|TestResponsesStreamPassesThroughUsage|TestChatCompletionsStreamPassesThroughUsage' -v
```

Expected:

- upstream usage passthrough tests stay green without estimator involvement

- [ ] **Step 3: Run fallback-focused handler tests and verify GREEN**

Run:

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/http -run 'TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing|TestResponsesStreamFallsBackToCharCountUsageWhenMissing|TestChatCompletionsStreamFallsBackToCharCountUsageWhenMissing' -v
```

Expected:

- all fallback-focused handler tests pass with the new heuristic values

### Task 4: Run Package Regression And Record Validation Evidence

**Files:**
- Create: `docs/reports/2026-04-20-token-usage-estimator-validation.md`
- Modify: `docs/reports/2026-04-20-token-usage-estimator-validation.md`
- Test: `internal/openai/compat_test.go`
- Test: `internal/http/handlers_test.go`

- [ ] **Step 1: Run package-level regression**

Run:

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/openai -v
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/http -v
```

Expected:

- both packages pass
- no passthrough regressions

- [ ] **Step 2: Run full repository regression**

Run:

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./... -count=1
```

Expected:

- full test suite passes

- [ ] **Step 3: Write the required validation report**

Create `docs/reports/2026-04-20-token-usage-estimator-validation.md` with these sections:

- summary of the estimator change
- exact test commands executed
- per-command pass/fail result
- note that upstream `usage` passthrough still has higher priority than fallback
- note that fallback is now heuristic-based and still approximate
- short list of fixture classes used for calibration

The report must include a concise result summary, not just raw command output.

### Task 5: Final Review And Commit

**Files:**
- Modify: `internal/openai/compat.go`
- Modify: `internal/openai/compat_test.go`
- Modify: `internal/http/handlers_test.go`
- Create: `docs/reports/2026-04-20-token-usage-estimator-validation.md`

- [ ] **Step 1: Self-check for ambiguity before committing**

Before committing, review the implementation against this checklist:

- every coefficient lives in one place
- no handler logic changed usage priority
- no third-party dependency was added
- all fixture expectations are explicit
- empty-string and short-text edge cases are tested
- chat overhead uses prompt signals instead of requiring handler signature changes
- docs report is present under `docs/reports`

If any item is false, fix it before proceeding.

- [ ] **Step 2: Commit the implementation in one logical change**

Run:

```bash
git add internal/openai/compat.go internal/openai/compat_test.go internal/http/handlers_test.go docs/reports/2026-04-20-token-usage-estimator-validation.md
git commit -m "fix: improve heuristic token usage fallback estimation"
```

### Spec Coverage Check

- Shared pure-Go estimator replacing `runeCount * 2`: covered by Task 2.
- Chat/responses reuse with chat-specific overhead: covered by Task 2 and Task 3.
- TDD-first implementation: covered by Task 1 before any production changes.
- Explicit files to modify: covered by File Map and per-task file lists.
- Test commands: covered in every task with exact commands.
- Report under `docs/reports`: covered by Task 4 and Task 5.
- Preserve upstream passthrough priority: covered by constraints and passthrough checks in Task 3 and Task 4.

### Placeholder Scan

The plan contains no unresolved placeholders or deferred implementation steps. The implementation worker should not need additional planning to know which files, tests, commands, and report path to use.
