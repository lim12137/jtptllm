# Model Marker Injection Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Inject strict `**model = fast|deepseek**` markers into prompt/input based on request `model` field, expose only these models in `/v1/models`, and remove `/model` endpoint.

**Architecture:** Add a small prompt injection helper in `internal/openai/compat.go` and call it from `ParseChatRequest` / `ParseResponsesRequest` before tool system prefix. Update HTTP routing to remove `/model` and return only `fast`/`deepseek` from `/v1/models`. Update tests accordingly.

**Tech Stack:** Go 1.22, net/http, testing package.

---

### Task 1: Add Prompt Injection Helper

**Files:**
- Modify: `internal/openai/compat.go`
- Test: `internal/openai/compat_test.go`

**Step 1: Write the failing tests**

Add tests to ensure `fast/deepseek` injects marker and replaces existing marker, and other models do not inject.

```go
func TestParseChatRequestInjectsModelFast(t *testing.T) {
	payload := map[string]any{
		"model": "fast",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	parsed := ParseChatRequest(payload)
	if !strings.HasPrefix(parsed.Prompt, "**model = fast**\n") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseChatRequestReplacesExistingMarker(t *testing.T) {
	payload := map[string]any{
		"model": "deepseek",
		"messages": []any{map[string]any{"role": "user", "content": "**model = fast**\nhello"}},
	}
	parsed := ParseChatRequest(payload)
	if strings.Contains(parsed.Prompt, "**model = fast**") {
		t.Fatalf("old marker still present: %q", parsed.Prompt)
	}
	if !strings.HasPrefix(parsed.Prompt, "**model = deepseek**\n") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseResponsesRequestInjectsModelDeepseek(t *testing.T) {
	payload := map[string]any{
		"model": "deepseek",
		"input": "hi",
	}
	parsed := ParseResponsesRequest(payload)
	if !strings.HasPrefix(parsed.Prompt, "**model = deepseek**\n") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseResponsesRequestNoInjectionForOtherModel(t *testing.T) {
	payload := map[string]any{
		"model": "agent",
		"input": "hi",
	}
	parsed := ParseResponsesRequest(payload)
	if strings.Contains(parsed.Prompt, "**model =") {
		t.Fatalf("unexpected marker: %q", parsed.Prompt)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/openai -v`
Expected: FAIL due to missing injection logic.

**Step 3: Implement minimal code**

Add helper in `internal/openai/compat.go`:
- `injectModelMarker(model, prompt string) string`
- If model not `fast/deepseek` -> return prompt
- Remove existing lines matching `^\*\*model\s*=.*\*\*$` (multiline)
- Prepend `**model = <model>**\n` to remaining prompt

Call it from `ParseChatRequest` and `ParseResponsesRequest` before `prependSystemPrefix`.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/openai -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/openai/compat.go internal/openai/compat_test.go
git commit -m "feat: inject model marker into prompts"
```

### Task 2: Update Model Endpoints

**Files:**
- Modify: `internal/http/handlers.go`
- Modify: `internal/http/handlers_test.go`

**Step 1: Write the failing tests**

Update `TestModelEndpoints` to assert:
- `/model` returns 404
- `/v1/models` returns list with exactly `fast` and `deepseek`

```go
req := httptest.NewRequest(http.MethodGet, "/model", nil)
rec := httptest.NewRecorder()
srv.Handler().ServeHTTP(rec, req)
if rec.Code != http.StatusNotFound {
	t.Fatalf("/model status=%d", rec.Code)
}

req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
rec = httptest.NewRecorder()
srv.Handler().ServeHTTP(rec, req)
if rec.Code != http.StatusOK {
	t.Fatalf("/v1/models status=%d", rec.Code)
}
// decode and assert data has ids: fast, deepseek (any order), length==2
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/http -v`
Expected: FAIL (current /model exists and models list only default).

**Step 3: Implement minimal code**

- Remove `/model` route and `handleModel` handler.
- Update `handleModels` to return list with ids `fast` and `deepseek`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/http -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/http/handlers.go internal/http/handlers_test.go
git commit -m "feat: expose fast/deepseek models only"
```

### Task 3: Full Test Sweep

**Files:**
- None

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: PASS.

**Step 2: Commit (only if any changes needed)**

```bash
git add -A
git commit -m "chore: fix tests"  # only if required
```
