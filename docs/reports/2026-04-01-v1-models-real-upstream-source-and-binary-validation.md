# 2026-04-01 `/v1/models` 真实上游源码与二进制验收

## 范围

- 使用仓库根目录实际 `api.txt` 配置连接真实上游
- 验证源码链路：`go run ./cmd/proxy` 后请求 `GET /v1/models`
- 验证二进制链路：重新构建 `bin/proxy.exe` 后启动，再请求 `GET /v1/models`
- 确认 `/model` 非标接口仍不可用
- 记录真实上游 `has-model?` 探测的原始回包证据

## 实际配置来源

- 配置文件：仓库根目录 `api.txt`
- 解析出的关键值：
  - `baseUrl`: `http://10.54.102.36:80/xlm-gateway--vinrl/sfm-api-gateway/gateway/agent/api`
  - `agentCode`: `d4f0f032-389e-4709-9a9b-0ac1f35ff3ba`
  - `agentVersion`: `1773710606282`

## 真实上游直接验证

先绕过代理，直接对上游执行：

1. `POST /createSession`
2. `POST /run`，请求体中 `message.text = "has-model?"`
3. `POST /deleteSession`

执行命令：内联 PowerShell 脚本，使用 `api.txt` 中的鉴权和 agent 参数直接请求上游。

结果摘要：

- `createSession` 成功，拿到真实 `sessionId`
- `run` 成功
- `deleteSession` 成功
- 上游原始回包已保存到：
  - `.codex_tmp/real-upstream-validation/upstream-run-response.json`
  - `.codex_tmp/real-upstream-validation/upstream-models-raw.txt`

原始回包中的关键信息：

```json
{
  "data": {
    "message": {
      "content": [
        {
          "type": "text",
          "text": {
            "value": "deepseek，fast，qingyuan"
          }
        }
      ]
    }
  }
}
```

说明：

- 真实上游返回的是字符串 `deepseek，fast，qingyuan`
- 分隔符是中文逗号 `，`
- 这正是 `/v1/models` 需要兼容解析的原始来源

## 源码链路验证

启动命令：

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe run ./cmd/proxy
```

验证请求：

```powershell
Invoke-RestMethod -Uri 'http://127.0.0.1:8022/v1/models' -TimeoutSec 60
Invoke-WebRequest -Uri 'http://127.0.0.1:8022/model' -UseBasicParsing -TimeoutSec 10
```

结果摘要：

- `/v1/models`：HTTP 200
- `/model`：HTTP 404
- 返回 JSON：

```json
{
  "data": [
    { "id": "deepseek", "object": "model" },
    { "id": "fast", "object": "model" },
    { "id": "qingyuan", "object": "model" }
  ],
  "object": "list"
}
```

源码链路结论：

- 代理确实会用真实上游配置发起 `has-model?` 探测
- 能将真实上游返回的中文逗号分隔字符串解析为标准 OpenAI models JSON

## 二进制链路验证

构建命令：

```powershell
C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe build -o .\bin\proxy.exe ./cmd/proxy
```

启动命令：

```powershell
.\bin\proxy.exe
```

验证请求：

```powershell
Invoke-RestMethod -Uri 'http://127.0.0.1:8022/v1/models' -TimeoutSec 60
Invoke-WebRequest -Uri 'http://127.0.0.1:8022/model' -UseBasicParsing -TimeoutSec 10
```

结果摘要：

- `/v1/models`：HTTP 200
- `/model`：HTTP 404
- 返回 JSON 与源码链路一致：

```json
{
  "data": [
    { "id": "deepseek", "object": "model" },
    { "id": "fast", "object": "model" },
    { "id": "qingyuan", "object": "model" }
  ],
  "object": "list"
}
```

二进制链路结论：

- 重新构建后的 `bin/proxy.exe` 与当前源码行为一致
- 二进制没有回退到旧的 `/model` 行为
- 二进制也能正确解析真实上游返回的模型字符串

## 结论

- 真实上游原始字符串：`deepseek，fast，qingyuan`
- `/v1/models` 在源码链路和二进制链路下均返回：
  - `deepseek`
  - `fast`
  - `qingyuan`
- `/model` 在两条链路下均为 `404`
- 当前仓库中的源码、构建产物和真实上游行为已经对齐
