# 2026-04-20 Debug Mode Raw Content Retention Validation

## Status

- Completed.
- After explicit "code landed" confirmation, package-level and full test suite validations were executed and recorded below.

## Context Collection (Read-only)

### Command

```powershell
Get-ChildItem -Path docs/reports -File | Select-Object -ExpandProperty Name
```

### Result summary

- Existing report naming pattern confirmed: `YYYY-MM-DD-<topic>.md`.

### Command

```powershell
Get-ChildItem -Path docs -Directory | Select-Object -ExpandProperty Name
```

### Result summary

- Confirmed report root directory exists: `docs/reports`.

### Command

```powershell
Get-Content -Path docs/reports/2026-04-20-debug-bat-log-persistence-fix.md
```

### Result summary

- Confirmed current report structure convention includes sections like `Summary` / `Validation` with command and result snippets.

## Landing Check

### Command

```powershell
git diff --name-only
```

### Result summary

- Implementation landed and included:
  - `internal/openai/compat.go`
  - `internal/openai/compat_test.go`

## Validation

### 1. Initial attempt before loading usable Go binary

Command:

```powershell
go test ./internal/openai -v
```

Result summary:

- Failed because `go` was not available in current shell PATH.
- Error: `The term 'go' is not recognized as a name of a cmdlet...`

### 2. Confirm usable Go toolchain path

Command:

```powershell
& 'D:\go_install\go\bin\go.exe' version
```

Result summary:

- Passed.
- Output: `go version go1.23.0 windows/amd64`

### 3. Related package test: openai

Command:

```powershell
& 'D:\go_install\go\bin\go.exe' test ./internal/openai -v
```

Result summary:

- Passed.
- New debug-mode behavior tests passed:
  - `TestNormalizeAssistantHistoryContentMalformedToolCallStrippedInNormalMode`
  - `TestNormalizeAssistantHistoryContentMalformedToolCallPreservedInDebugMode`
- Package result: `ok github.com/lim12137/jtptllm/internal/openai`

### 4. Related package test: http

Command:

```powershell
& 'D:\go_install\go\bin\go.exe' test ./internal/http -v
```

Result summary:

- Passed.
- Package result: `ok github.com/lim12137/jtptllm/internal/http`
- Expected stream error log lines were present in tests and did not affect pass status.

### 5. Full regression run

Command:

```powershell
& 'D:\go_install\go\bin\go.exe' test ./... -v
```

Result summary:

- Passed.
- All packages green:
  - `internal/config`
  - `internal/gateway`
  - `internal/http`
  - `internal/openai`
  - `internal/session`
