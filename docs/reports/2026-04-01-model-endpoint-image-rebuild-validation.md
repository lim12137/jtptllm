# 2026-04-01 `/model` 端点与镜像重打包验证

## 背景
- 用户反馈镜像内二进制不是最新版本。
- 具体现象不是泛化的“版本旧”，而是 `/model` 端点只返回 `agent`，未体现当前版本应有的 `fast`、`deepseek`、`qingyuan`。

## 根因结论
- 当前源码中的 `/model` 与 `/v1/models` 实现已经返回 `fast`、`deepseek`、`qingyuan`。
- 旧 `Dockerfile` 直接复制 `dist/linux-${TARGETARCH}/proxy` 到运行镜像，镜像内容依赖外部预编译产物，而不是当前源码现场编译。
- 因此只要 `dist` 未及时重建，或构建上下文携带了旧 `dist`，就会把旧二进制打进镜像，表现为 `/model` 端点仍停留在旧行为。

## 关键代码位置
- `internal/http/handlers.go:92`
  - `/model` 与 `/v1/models` 返回模型列表。
- `internal/http/handlers.go:100`
  - 返回 `fast`。
- `internal/http/handlers.go:101`
  - 返回 `deepseek`。
- `internal/http/handlers.go:102`
  - 返回 `qingyuan`。
- `internal/http/handlers_test.go:91`
  - `TestModelEndpoints` 校验 `/model` 与 `/v1/models`。
- `internal/http/handlers_test.go:125`
  - 明确断言必须包含 `fast`、`deepseek`、`qingyuan`。
- `Dockerfile`
  - 已改为 multi-stage 构建，在镜像构建过程中直接 `go build ./cmd/proxy`，不再复制 `dist` 旧产物。
- `scripts/verify_dockerfile.ps1`
  - 已更新为校验 multi-stage 构建结构，防止回退到复制 `dist` 的旧模式。

## 本次修改
- 将 `Dockerfile` 从“复制 `dist` 里的预编译二进制”改为“在 builder stage 中按目标平台直接编译当前源码，再复制到运行镜像”。
- 更新 `scripts/verify_dockerfile.ps1`，确保 CI/本地校验的是源码编译式 Dockerfile。

## 验证命令
```powershell
powershell -ExecutionPolicy Bypass -File scripts/verify_dockerfile.ps1
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestModelEndpoints -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v
```

## 验证结果摘要
- `scripts/verify_dockerfile.ps1`: 通过。
- `go test ./internal/http -run TestModelEndpoints -v`: 通过。
  - 验证 `/model` 与 `/v1/models` 都包含 `fast`、`deepseek`、`qingyuan`。
- `go test ./... -v`: 全量通过。

## 说明
- 本次未执行实际 `docker build` / `docker run` 验证，因为当前修复目标首先是消除“镜像复用旧 dist 二进制”的根因，并以现有自动化测试确认 `/model` 源码行为正确。
- 若需要补充容器级验收，可继续执行：

```powershell
docker build --no-cache -t jtptllm:dev .
docker run --rm -p 8080:8080 jtptllm:dev
```

随后请求：

```powershell
Invoke-RestMethod http://127.0.0.1:8080/model
```

预期返回模型列表包含 `fast`、`deepseek`、`qingyuan`。
