# Go 代理服务设计（OpenAI 兼容）

## 目标

构建一个更小镜像、启动更快的 Go 版本中转代理，能力与现有 Python 版本一致：
- `/v1/chat/completions` 与 `/v1/responses`
- 流式 SSE
- 10 分钟会话复用
- CORS
- `/model` 与 `/v1/models`
- 通过挂载 `api.txt` 提供配置
- 支持 `ghcr.io/lim12137/jtptllm` 的多架构（amd64/arm64）构建

## 方案选择

选用 **Go 标准库 `net/http`** 实现服务：
- 镜像体积更小（distroless/scratch）
- 启动更快
- 依赖最少、稳定性高

## 架构与数据流

### 核心模块

1. **配置加载**
   - 读取 `/app/api.txt`
   - 支持 `key/agentCode/agentVersion`，允许全角冒号
   - 允许 `--base-url` 覆盖

2. **Gateway Client**
   - `createSession`
   - `run`（非流式 + 流式）
   - `deleteSession`
   - `feedback`（保留接口）

3. **SessionManager**
   - 以 `x-agent-session` / `x-client-id` / 客户端 IP 作为 key
   - 10 分钟空闲 TTL
   - 允许 `x-agent-session-reset` 强制新建
   - 允许 `x-agent-session-close` 主动释放

4. **OpenAI 兼容层**
   - `/v1/chat/completions`：非流式 + SSE
   - `/v1/responses`：非流式 + SSE
   - `/model` / `/v1/models`
   - CORS 全放行

### 流式处理

网关流式读入后，做两件事：
1. 解析 SSE 行 `data: {json}`
2. 将网关增量归一成 OpenAI SSE 事件

若网关输出是“全量快照”，使用“前缀差分”生成 delta。

## 运行参数

- `--api-txt /app/api.txt`
- `--host 0.0.0.0`
- `--port 8022`
- `--session-ttl 600`

## Docker 与 CI

### Dockerfile
- 多阶段构建：
  - build: `golang:1.22`
  - runtime: `gcr.io/distroless/static`
- 镜像入口：Go 二进制

### GitHub Actions
- 构建并推送 `ghcr.io/lim12137/jtptllm`
- 平台：`linux/amd64`, `linux/arm64`
- 触发：`push main` + `workflow_dispatch`

## 测试策略

- 单元测试：配置解析、session 复用、增量合并逻辑
- 冒烟测试：本地请求 `/health`、非流式、流式

## 交付物

- Go 代码（`cmd/proxy` + `internal/...`）
- `Dockerfile`
- `.dockerignore`
- GitHub Actions workflow

