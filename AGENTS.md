# Repository Guidelines

## 必读/流程
- 所有任务必须使用子代理执行，主代理仅负责拆分、调度与汇总。子代理优先用gpt-5.2-codex high 模型。
- 子代理执行超过 30 分钟未产出结果时，必须回询进度与阻塞原因，必要时拆解细化任务。
- 并发测试必须落盘为 `docs` 下的 `*.md` 报告，报告包含测试命令与结果摘要。
## Project Structure & Module Organization
- `cmd/proxy/main.go`: application entrypoint, starts the HTTP proxy server.
- `internal/config`: parses `api.txt` and runtime config.
- `internal/gateway`: upstream gateway client (`createSession` / `run` / `deleteSession`).
- `internal/session`: session reuse and TTL lifecycle.
- `internal/openai`: OpenAI compatibility mapping and stream helpers.
- `internal/http`: route handlers, CORS, request/response wiring.
- `scripts`: local smoke and operational scripts (for example `codex_toolcall_smoke.ps1`).
- `docs/plans` and `docs/reports`: design docs, implementation plans, and validation reports.
- `.github/workflows/go-test.yml`: CI test workflow.

## Build, Test, and Development Commands
- Build binary:
  - `go build -o bin/proxy.exe ./cmd/proxy`
- Run all tests:
  - `go test ./... -v`
- Run one package:
  - `go test ./internal/http -v`
- Smoke test tool-call flow (proxy must already run):
  - `powershell -File scripts/codex_toolcall_smoke.ps1`
- If `go` is not on `PATH`, use the local toolchain:
  - `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v`

## Coding Style & Naming Conventions
- Language: Go 1.22. Keep code `gofmt`-clean before commit.
- Package names are short, lowercase (`config`, `gateway`, `openai`).
- Exported symbols use `PascalCase`; internal helpers use `camelCase`.
- Keep handlers thin; push parsing/mapping logic into `internal/openai` and `internal/gateway`.
- Prefer small, focused files and tests in the same package (`*_test.go`).

## Testing Guidelines
- Test framework: Go `testing` package.
- Test naming: `Test<Behavior>` (examples: `TestHealth`, `TestIOLoggingEnabled`).
- Add/adjust tests for every behavior change, including error paths and compatibility mapping.
- Minimum pre-PR baseline: `go test ./... -v` passes locally.

## Commit & Pull Request Guidelines
- Follow Conventional Commits style seen in history: `feat: ...`, `fix: ...`, `docs: ...`, `chore: ...`, `ci: ...`.
- Keep commits scoped to one logical change with matching tests.
- PRs should include:
  - concise summary and motivation,
  - key files changed,
  - test evidence (commands + pass/fail),
  - linked plan/report docs when behavior or protocol changes.

## Security & Configuration Tips
- Never commit secrets. `api.txt` is local runtime config and should stay environment-specific.
- For debugging IO logs, enable `PROXY_LOG_IO=1`; disable it in normal runs to avoid sensitive payload logging.
