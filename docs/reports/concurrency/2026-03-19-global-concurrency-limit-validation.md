# 2026-03-19 Global Concurrency Limit Validation

## Scope
- Commit under validation: `bce2f51`
- Goal: validate the HTTP-layer global concurrency gate
- Default global limit: `16`
- Shared scope: `chat` and `responses` use the same gate
- Over-limit behavior: wait in queue, not reject
- Error-path behavior: slot is released even when request handling fails

## Validation Commands
```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run GlobalConcurrencyGate -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v
```

## Implementation Summary
- The global concurrency gate is implemented in the HTTP layer.
- The gate is shared by both `/v1/chat/completions` and `/v1/responses`.
- The server creates a buffered gate with capacity `16`.
- Requests beyond `16` are blocked and wait until an active request releases its slot.
- Request completion releases the slot through `defer`, so normal and error paths both free capacity.
- Existing per-key session pool / queue logic is preserved and not replaced by the global gate.

## Added Tests
- `TestGlobalConcurrencyGateSeventeenthRequestWaits`
  - Verifies the 17th request does not start until one of the first 16 releases its slot.
- `TestGlobalConcurrencyGateSharedByChatAndResponses`
  - Verifies `chat` and `responses` share the same global gate rather than having separate limits.
- `TestGlobalConcurrencyGateReleasesOnError`
  - Verifies an erroring request still releases its slot so the next queued request can proceed.

## Results

### Targeted Gate Tests
Command:
```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run GlobalConcurrencyGate -v
```

Observed result:
```text
=== RUN   TestGlobalConcurrencyGateSeventeenthRequestWaits
--- PASS: TestGlobalConcurrencyGateSeventeenthRequestWaits
=== RUN   TestGlobalConcurrencyGateSharedByChatAndResponses
--- PASS: TestGlobalConcurrencyGateSharedByChatAndResponses
=== RUN   TestGlobalConcurrencyGateReleasesOnError
--- PASS: TestGlobalConcurrencyGateReleasesOnError
PASS
```

Conclusion:
- The 17th request waits.
- `chat` and `responses` share one global limit.
- Error paths release the slot correctly.

### Full `internal/http` Package
Command:
```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v
```

Observed result:
```text
PASS
ok  	github.com/lim12137/jtptllm/internal/http
```

Conclusion:
- The new gate did not break the existing `internal/http` test suite.

### Full Repository Command
Command:
```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v
```

Note:
- This command is included here as the repository-level validation command for the change set.
- It was not re-run in this documentation-only wrap-up step.

## Final Assessment
- Default global HTTP concurrency limit is `16`.
- `chat` and `responses` are governed by the same gate.
- Requests over the limit wait instead of being rejected.
- Error paths release capacity and do not deadlock the queue.
- Validation status for commit `bce2f51`: passed at targeted-gate level and full `internal/http` package level.
