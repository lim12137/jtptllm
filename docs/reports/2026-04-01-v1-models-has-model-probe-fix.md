# 2026-04-01 `/v1/models` Has-Model Probe Fix

## 目标

- 删除非标接口 `/model`
- 仅保留 `GET /v1/models`
- `GET /v1/models` 不再读取客户端 query
- 服务端内部固定向上游发送 `text = "has-model?"`
- 将上游返回的模型字符串解析为 OpenAI 标准 `/v1/models` JSON

## TDD

先修改 `internal/http/handlers_test.go` 中的 `TestModelEndpoints`：

- 断言 `GET /model` 返回 `404`
- 断言 `GET /v1/models` 返回标准 JSON
- 断言上游收到的 `RunRequest.Text` 为 `has-model?`
- 用上游返回字符串 `1*2*3` 作为测试输入，最终应解析为三个模型：`1`、`2`、`3`

首次失败命令：

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestModelEndpoints -v
```

失败摘要：

```text
=== RUN   TestModelEndpoints
    handlers_test.go:129: /v1/models models len=1
--- FAIL: TestModelEndpoints (0.00s)
```

## 实现

修改 `internal/http/handlers.go`：

- `Handler()` 不注册 `/model`
- `handleModels()` 内部调用 `gateway.Run(...)`
- 固定发送：

```text
text = has-model?
```

- 从上游非流式响应中提取字符串
- 按 `*` 分隔，兼容空白和换行
- 组装为 OpenAI 标准模型列表 JSON

## 验证

目标测试：

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestModelEndpoints -v
```

结果：PASS

全量测试：

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v
```

结果：PASS

## 结果

- `/model` 已删除
- `/v1/models` 现在会内部探测上游 `has-model?`
- 上游返回字符串 `1*2*3` 时，接口返回三个标准模型对象：`1`、`2`、`3`
