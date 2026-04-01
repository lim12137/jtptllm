# 2026-04-01 `/model` Endpoint And Image Freshness Fix

## Summary
- 验收焦点：`/model` 端点不应再表现为旧镜像中的“只有 `agent`”。
- 当前源码在修复前的真实行为：
  - `/model`：`404 Not Found`
  - `/v1/models`：返回 `fast`、`deepseek`、`qingyuan`
- 镜像链路不一致的根因：
  - 旧 `Dockerfile` 直接 `COPY dist/linux-${TARGETARCH}/proxy /app/proxy`
  - 这会把工作区已有的 `dist` 产物直接打进镜像，而不是在构建镜像时基于当前源码重新编译
  - 历史代码中确实存在只暴露默认模型 `agent` 的旧实现，因此镜像里出现“只有 `agent`”与“当前源码不一致”可以由旧二进制被打包解释
- 本次最小修复：
  - `Dockerfile` 改为 multi-stage，镜像构建时直接编译 `./cmd/proxy`
  - `Handler()` 新增 `/model` 路由，直接复用 `handleModels`

## TDD Record
- 先修改 `internal/http/handlers_test.go`，将 `/model` 纳入模型列表断言，要求其返回 `fast`、`deepseek`、`qingyuan`
- 失败验证结果：

```text
=== RUN   TestModelEndpoints
    handlers_test.go:99: /model status=404
--- FAIL: TestModelEndpoints (0.00s)
FAIL
```

- 随后做最小生产修复：
  - `internal/http/handlers.go`
  - `Dockerfile`
  - `scripts/verify_dockerfile.ps1`

## Commands
```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify_dockerfile.ps1
```

```powershell
$env:GOCACHE = (Join-Path (Get-Location) '.gocache')
New-Item -ItemType Directory -Force -Path $env:GOCACHE | Out-Null
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/http -run TestModelEndpoints -v
```

```powershell
$env:GOCACHE = (Join-Path (Get-Location) '.gocache')
New-Item -ItemType Directory -Force -Path $env:GOCACHE | Out-Null
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./... -v
```

```powershell
docker version --format '{{.Server.Version}}'
```

## Results
- `scripts/verify_dockerfile.ps1`: PASS
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestModelEndpoints -v`: PASS
- `C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v`: PASS
- `docker version --format '{{.Server.Version}}'`: FAIL, current machine has no running Docker daemon (`//./pipe/dockerDesktopLinuxEngine` not found), so image runtime verification could not be executed in this session

## Notes
- 当前源码里的第三个模型名为 `qingyuan`，不是 `qinyuan`。本次修复按仓库现有源码约定验证 `qingyuan`。
- 如果外部调用方已经依赖 `qinyuan` 这个拼写，需要单独做兼容别名变更；本次未扩 scope。
