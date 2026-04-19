# Repository Guidelines

## 必读/流程
- 所有任务必须使用子代理执行。主代理只做分析、拆分、分配、调度、判断与汇总，不直接承担具体实现。
- 具体实现、代码修改、测试执行、报告落盘、文档更新必须由子代理执行。
- 简单任务使用 `gpt5.4-mini`。
- 非简单任务优先使用 `gpt-5.3-codex`，默认使用 high reasoning。
- 主代理必须先判断任务属于分析、规划、实现、调试、评审、QA 还是发布，再按任务意图优先路由对应的 ce 或 gstack 系列技能。
- 分析、规划、方案、复盘类任务优先使用 ce 系列技能。
- 调试、评审、QA、健康检查、设计审查、发布/ship 等流程优先使用 gstack 系列技能。
- 如未使用对应技能，必须在回复中明确说明原因。
- 不要着急，子代理启动后至少等待 10 分钟，再主动索要阶段性进展。
- 若未到 10 分钟，除非出现明确失败、熔断、用户追问，或主任务被阻塞，否则不要中断子代理索要进度。
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
- Go 本地工具链已安装到 D 盘，环境变量已写入用户级别：
  - `GOROOT=D:\go`
  - `GOPATH=D:\gopath`
  - `PATH` 包含 `D:\go\bin`
- 如果当前终端 `go` 不可用，手动加载：
  - `export GOROOT=/d/go GOPATH=/d/gopath PATH="/d/go/bin:$PATH"`
  - 或在 PowerShell 中：`$env:GOROOT='D:\go'; $env:GOPATH='D:\gopath'; $env:Path='D:\go\bin;' + $env:Path`

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
