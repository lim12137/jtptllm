# jtptllm Go Proxy

OpenAI 兼容中转代理（Go 版），支持：
- `/v1/chat/completions`
- `/v1/embeddings`
- `/v1/rerank`
- SSE 流式输出
- 10 分钟会话复用
- CORS
- `/model` 与 `/v1/models`
- 内置固定模型映射
- `api.txt` 挂载配置
- `api.md` / `*.md` 接口文档接入配置
- 无配置文件时直接通过启动参数传入凭证

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

### 直接双击启动

双击 `jtptllm-proxy.exe` 即可按默认配置启动，默认监听：

```txt
0.0.0.0:8022
```

默认行为：

- 优先读取可执行文件同目录下的 `api.md`
- 若同目录存在 `大语言模型推理.md`，也会自动识别
- 默认 `api.txt` 也固定写在可执行文件同目录
- 若 `api.txt` 不存在，会自动生成带注释说明的占位文件

### 最小化到系统托盘（仅 Windows 桌面）

双击 `jtptllm-proxy.exe` 首次启动时，会在 exe 同目录自动生成一个
`代理-托盘.lnk` 快捷方式（带 `--tray` 参数）。之后**双击该快捷方式**即可
进入托盘模式：

- 控制台黑窗会闪现一下后自动隐藏
- 系统托盘（任务栏右下角通知区）出现代理图标，鼠标悬停显示监听地址
- 右键托盘图标：「显示控制台 / 退出」
- 选择「退出」会优雅关闭 HTTP 服务再退出进程

> 说明：本程序是 Console 子系统二进制，双击快捷方式启动托盘模式时控制台
> 会有一瞬间的黑窗闪现（之后自动隐藏），这是 Windows 的固有限制。命令行
> 调用 `jtptllm-proxy.exe`（不带 `--tray`）的行为完全不变，仍是阻塞式
> 控制台程序，日志内联输出。

也可以手动用 `--tray` 启动：

```bat
jtptllm-proxy.exe --tray
```

托盘模式仅在 Windows 上生效；Linux/Docker 仍为纯控制台服务（构建标签隔离）。

### 访问鉴权（保护 /v1/* 接口）

默认情况下代理监听 `0.0.0.0`，局域网/公网任何人都能直接调用 `/v1/*`，
白白消耗你的网关额度。开启鉴权后，调用方必须携带正确的 Bearer token：

1. 在 `api.txt`（或命令行）中配置访问密码：

   ```txt
   key: <APP_KEY>
   agentCode: <AGENT_CODE>
   authToken: my-secret-token
   ```

   或启动参数：

   ```bat
   jtptllm-proxy.exe --auth-token my-secret-token
   ```

2. 调用 `/v1/*` 时带上 `Authorization` 头：

   ```bash
   curl http://127.0.0.1:8022/v1/chat/completions \
     -H "Authorization: Bearer my-secret-token" \
     -H "Content-Type: application/json" \
     -d '{"model":"qwen3.6","messages":[{"role":"user","content":"你好"}]}'
   ```

鉴权规则：

- 只保护 `/v1/*` 路径（`/v1/chat/completions`、`/v1/embeddings`、
  `/v1/rerank`、`/v1/models`）；`/health`、`/model` 不鉴权，方便健康检查。
- token 为空时鉴权完全关闭，向后兼容（现有无 token 的调用方零影响）。
- 比较采用常量时间比较（`crypto/subtle`），防止时序侧信道。

### 日志与错误中的 key 脱敏

启动日志、错误响应中涉及网关 `key` 的地方一律脱敏，只显示「前 4 位 + `****`
+ 后 4 位」，例如：

```txt
proxy listening on 0.0.0.0:8022 (appKey masked: FRQ6****4qKO)
auth enabled for /v1/* (token masked: my-s****oken)
```


### 本地运行（不使用配置文件）

```
go run ./cmd/proxy \
  --app-key <APP_KEY> \
  --agent-code <AGENT_CODE> \
  --agent-version <AGENT_VERSION> \
  --host 0.0.0.0 \
  --port 8022
```

### 本地运行（使用 Markdown 接口文档）

当文档描述的是 `compatible-mode/v1/chat/completions` 这类 OpenAI 兼容直连接口时，可直接读取 `.md` 文档中的 `POST` 地址：

```
go run ./cmd/proxy \
  --api-md ..\\docs\\大语言模型推理.md \
  --app-key <APP_KEY> \
  --host 0.0.0.0 \
  --port 8022
```

## API 示例

当上游 agent 调用成功但回复文本为空时，代理会返回 HTTP `502`，错误码为 `empty_agent_response`，可用于调用方故障切换。

## 内置模型

项目内已固化以下模型映射，请求里可直接传模型名或别名，程序会自动映射到固定模型 ID：

- `DeepSeek-V3.2` / `deepseek` -> `rsv-lixirkqxzkpslnqfgmxizjjil-aq`
  - 上下文：`64k`
- `Qwen3.6-27B` / `qwen3.6` -> `rsv-q23123sde`
  - 上下文：`256k`
  - 能力：`chat`、`vision`
- `Qwen3-Reranker-0.6B` / `qwen3-reranker` -> `rsv-11m4dmp2`
  - 能力：`rerank`、`embedding`
  - 向量维度：`1024`
- `bge-m3-1024维` / `bge-m3` -> `rsv-tchvlgrj`
  - 能力：`embedding`
  - 向量维度：`1024`

`/v1/models` 会直接返回这 4 个内置模型，并附带 `context_window` 与 `capabilities` 字段。

向量与重排接口在代理层统一固定为：

- `/v1/embeddings`
  - 默认走 `bge-m3-1024维`
- `/v1/rerank`
  - 默认走 `Qwen3-Reranker-0.6B`

### /v1/chat/completions

```
curl http://127.0.0.1:8022/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.6",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": false
  }'
```

### /v1/embeddings

```
curl http://127.0.0.1:8022/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "input": ["第一段文本", "第二段文本"]
  }'
```

### /v1/rerank

```
curl http://127.0.0.1:8022/v1/rerank \
  -H "Content-Type: application/json" \
  -d '{
    "query": "北京天气",
    "documents": ["北京今天晴，适合出行", "上海今天有雨，注意带伞"],
    "top_n": 2
  }'
```

## 配置项

- `--api-txt`：api.txt 路径（默认 `api.txt`）
- `--api-md`：Markdown 接口文档路径；会自动提取 `POST .../v1/chat/completions` 地址并切换到 compatible 模式
- `--app-key`：直接传入网关凭证 key；如不传则回落到程序内置默认值
- `--agent-code`：直接传入 agentCode，不使用 api.txt 时必填
- `--agent-version`：直接传入 agentVersion，不使用 api.txt 时选填
- `--base-url`：网关 baseUrl 覆盖
- `--default-model`：/model 默认模型名（默认 `agent`）
- `--session-ttl`：会话复用 TTL（秒，默认 `600`，设为 `0` 禁用）
- `--auth-token`：访问 `/v1/*` 接口所需的 Bearer token；为空时鉴权关闭（也可在 api.txt 中写 `authToken:`）
- `--tray`：最小化到系统托盘（仅 Windows 桌面，见上文「最小化到系统托盘」）
- `--host` / `--port`：监听地址（默认 `0.0.0.0:8022`）

说明：

- `api.txt` 对应的是 agent 网关模式，需要 `key + agentCode (+ agentVersion)`，程序内部会走 `createSession -> run -> deleteSession`。
- `api.md` 对应的是文档化的 OpenAI 兼容模式；当前会自动识别 `POST .../compatible-mode/v1/chat/completions`，并直接透传 `/v1/chat/completions` 请求。
- 程序内已固化默认 `APP_KEY`；如果 `md` 文档里写了 `key:`，则以 `md` 中的值覆盖内置值。
- 启动时会优先使用命令行直传参数；缺失字段再从配置文件补齐，因此支持“参数覆盖文件”的混合模式。
- 如果指定的 `api.txt` 不存在，程序会自动生成一个最小占位文件：
  - `# auto-generated placeholder api.txt`
  - `# fill APP_KEY and agentCode before using agent gateway mode`
  - `key:`
  - `agentCode:`

## Actions

- `go-test`：所有 push/PR 触发 `go test ./...`
- `docker-build`：push 到 main/master 或手动触发构建并推送 GHCR
