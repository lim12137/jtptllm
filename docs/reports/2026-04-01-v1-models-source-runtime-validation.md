# 2026-04-01 `/v1/models` 多分隔兼容与源码真实请求验证

## 范围

- 只保留标准接口 `GET /v1/models`
- `/model` 非标接口保持不存在
- 兼容上游模型字符串分隔符：
  - `*`
  - 英文逗号 `,`
  - 中文逗号 `，`
  - 换行
  - 空白混排（空格、Tab，以及其他 `unicode.IsSpace` 可识别空白）
- 本报告只覆盖源码级真实起服验证；二进制实测按最新要求留到后续

## TDD

### RED

先修改 `internal/http/handlers_test.go`，把模型探测返回值改为混合分隔：

```text
1*2,3，4
5 6\t7
```

执行命令：

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestModelEndpoints -v
```

失败摘要：

```text
/v1/models models len=6
```

说明：实现当时未兼容中文逗号 `，`。

### GREEN

最小修复：更新 `splitModelList`，在保留现有分隔逻辑基础上新增：

- `unicode.IsSpace(r)` 统一处理空白
- 显式支持中文逗号 `，`

## 自动化测试

执行命令：

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -run TestModelEndpoints -v
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v
```

结果摘要：

- `TestModelEndpoints`：PASS
- `go test ./... -v`：PASS

## 源码真实起服验证

### 最小本地伪上游

为避免改动业务逻辑，复用仓库现有 `api.txt` 配置方式，在临时目录 `.codex_tmp/models-source-test` 中准备：

- `api.txt`
  - `baseUrl: http://127.0.0.1:18081`
- `stub-server.ps1`
  - `/createSession` 返回固定 `stub-session`
  - `/run` 返回混合分隔字符串：`1*2,3，4\n5 6\t7`
  - `/deleteSession` 返回成功

### 启动命令

启动本地伪上游：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .codex_tmp\models-source-test\stub-server.ps1 -LogPath .codex_tmp\models-source-test\stub-run-body.json
```

基于当前源码真实起服：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "$env:GOCACHE='C:\Users\Administrator\Desktop\人工智能项目\低代码智能体\api调用\.gocache'; & 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' run ..\..\cmd\proxy"
```

说明：源码服务工作目录为 `.codex_tmp/models-source-test`，因此读取的是临时 `api.txt`，不影响仓库根目录现有配置。

### 真实 HTTP 请求

请求标准模型接口：

```powershell
Invoke-WebRequest -Uri 'http://127.0.0.1:8022/v1/models' -UseBasicParsing -TimeoutSec 20
```

实际响应摘要：

```json
{
  "data": [
    { "id": "1", "object": "model" },
    { "id": "2", "object": "model" },
    { "id": "3", "object": "model" },
    { "id": "4", "object": "model" },
    { "id": "5", "object": "model" },
    { "id": "6", "object": "model" },
    { "id": "7", "object": "model" }
  ],
  "object": "list"
}
```

验证非标接口不存在：

```powershell
Invoke-WebRequest -Uri 'http://127.0.0.1:8022/model' -UseBasicParsing -TimeoutSec 5
```

结果摘要：

- `/v1/models`：HTTP 200，返回 7 个模型对象
- `/model`：HTTP 404

### 上游探测确认

读取本地伪上游记录的 `/run` 请求体：

```json
{"delta":false,"message":{"attachments":[],"metadata":{},"text":"has-model?"},"sessionId":"stub-session","stream":false}
```

确认点：

- `/v1/models` 不是读取客户端 query
- 服务端内部固定向上游发送 `text: "has-model?"`
- 之后将上游返回的混合分隔字符串解析为标准 OpenAI models JSON

## 结论

- 多分隔格式兼容已完成
- `/model` 非标接口已移除
- 自动化测试通过
- 基于当前源码启动的真实服务，已通过真实 HTTP 请求验证 `/v1/models`
- 二进制实测未执行，按最新要求留到后续
