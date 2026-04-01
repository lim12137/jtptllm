# 2026-04-01 `/v1/models` Query Split Fix

## 目标

- 删除非标准接口 `/model`，不再注册该路由。
- `GET /v1/models` 改为从请求 query 中读取 `model=` 原始字符串。
- 将 `model` 按 `*` 分隔为多个模型名返回。
- 示例：`/v1/models?model=1*2*3` 返回 `1`、`2`、`3` 三个模型。

## TDD 过程

### RED

先修改 `internal/http/handlers_test.go` 的 `TestModelEndpoints`：

- 去掉固定模型列表断言。
- 改为断言 `GET /v1/models?model=1*2*3` 返回 3 个模型，顺序为 `1,2,3`。

先行失败验证命令：

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestModelEndpoints -v
```

失败摘要：

```text
=== RUN   TestModelEndpoints
    handlers_test.go:98: /model status=200
--- FAIL: TestModelEndpoints (0.00s)
FAIL
```

说明当时实现仍保留 `/model` 路由，且还未满足最终路由收口要求。

### GREEN

最小实现修改：

- `internal/http/handlers.go`
  - 删除 `/model` 路由注册。
  - `handleModels` 改为读取 `r.URL.Query().Get("model")`。
  - 用 `*` 分隔并逐项生成 `{id, object}` 模型项。
  - 当 query 未传 `model` 时，回退为 `defaultModel` 单项列表。

- `internal/http/handlers_test.go`
  - 仅保留 `/v1/models?model=1*2*3` 的返回断言。

## 验证命令

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestModelEndpoints -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v
```

## 验证结果摘要

- `go test ./internal/http -run TestModelEndpoints -v`：PASS
- `go test ./... -v`：PASS

## 结果

- `/model` 已从服务路由中移除。
- `/v1/models?model=1*2*3` 返回 3 个模型项：`1`、`2`、`3`。
- 现有全量 Go 测试通过。
