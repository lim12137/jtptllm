# jtptllm Go Proxy

OpenAI 兼容中转代理（Go 版），支持：
- `/v1/chat/completions` 与 `/v1/responses`
- SSE 流式输出
- 10 分钟会话复用
- CORS
- `/model` 与 `/v1/models`
- `api.txt` 挂载配置

## 运行

### 准备 api.txt

```
key: <APP_KEY>
agentCode: <AGENT_CODE>
agentVersion: <AGENT_VERSION>
```

### Docker（推荐）

```
docker run --rm -p 8022:8022 \
  -v $(pwd)/api.txt:/app/api.txt \
  ghcr.io/lim12137/jtptllm:latest
```

### 本地运行

```
go run ./cmd/proxy --api-txt api.txt --host 0.0.0.0 --port 8022
```

## API 示例

### /v1/chat/completions

```
curl http://127.0.0.1:8022/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "agent",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": false
  }'
```

### /v1/responses

```
curl http://127.0.0.1:8022/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "agent",
    "input": "你好",
    "stream": false
  }'
```

## 配置项

- `--api-txt`：api.txt 路径（默认 `api.txt`）
- `--base-url`：网关 baseUrl 覆盖
- `--default-model`：/model 默认模型名（默认 `agent`）
- `--session-ttl`：会话复用 TTL（秒，默认 `600`，设为 `0` 禁用）
- `--host` / `--port`：监听地址（默认 `0.0.0.0:8022`）

## Actions

- `go-test`：所有 push/PR 触发 `go test ./...`
- `docker-build`：push 到 main/master 或手动触发构建并推送 GHCR