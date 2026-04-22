# jtptllm

Agent 转 LLM 代理服务。将内部 Agent Gateway API 转换为 OpenAI 兼容接口。

## 快速启动

### Docker 运行（推荐）

镜像：`ghcr.io/lim12137/jtptllm:latest`

#### 方式一：挂载 api.txt

```bash
# 1. 准备 api.txt（包含凭证信息）
cat > api.txt <<EOF
key: YOUR_APP_KEY
agentCode: YOUR_AGENT_CODE
agentVersion: 1.0
EOF

# 2. 运行容器，挂载 api.txt
docker run -d \
  --name jtptllm \
  -p 8022:8022 \
  -v $(pwd)/api.txt:/app/api.txt:ro \
  ghcr.io/lim12137/jtptllm:latest
```

#### 方式二：环境变量

```bash
docker run -d \
  --name jtptllm \
  -p 8022:8022 \
  -e AGENT_APP_KEY=YOUR_APP_KEY \
  -e AGENT_AGENT_CODE=YOUR_AGENT_CODE \
  -e AGENT_AGENT_VERSION=1.0 \
  -e AGENT_BASE_URL=http://10.54.102.36:80/xlm-gateway--vinrl/sfm-api-gateway/gateway/agent/api \
  ghcr.io/lim12137/jtptllm:latest
```

#### 可选环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PROXY_ADDR` | `:8022` | 监听地址 |
| `PROXY_HTTP_LIMIT` | `16` | 全局并发 HTTP 限制 |

### 本地运行

```bash
# 需要 Go 1.22+
go build -o proxy ./cmd/proxy
./proxy
```

## api.txt 格式

```
key: YOUR_APP_KEY
agentCode: YOUR_AGENT_CODE
agentVersion: 1.0
baseUrl: http://10.54.102.36:80/xlm-gateway--vinrl/sfm-api-gateway/gateway/agent/api
```

> **注意：** `api.txt` 包含密钥信息，已在 `.gitignore` 中排除，请勿提交到仓库。Docker 部署时通过挂载或环境变量提供。
