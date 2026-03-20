# Non-stream Tiny Failure Analysis

## 目标与范围

- 日期：2026-03-19
- endpoint：`http://127.0.0.1:8022/v1/chat/completions`
- 目标：用 10 字节以下最小输入复现并解释为什么 `stream=false` 路径无法通过。
- 限制：本次仅做复现与分析，不改代码，不提交。

## 最小复现命令

### 1. `deepseek` + `stream=false` + 无 tools + 极小输入

```powershell
curl.exe -sS -D - -X POST "http://127.0.0.1:8022/v1/chat/completions" ^
  -H "Content-Type: application/json" ^
  -H "x-session-key: cid:tiny-ns-deepseek" ^
  --data-binary "{\"model\":\"deepseek\",\"stream\":false,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}"
```

### 2. 对照：`deepseek` + `stream=true` + 其余尽量相同

```powershell
curl.exe -sS -N -D - -X POST "http://127.0.0.1:8022/v1/chat/completions" ^
  -H "Content-Type: application/json" ^
  -H "x-session-key: cid:tiny-s-deepseek" ^
  --data-binary "{\"model\":\"deepseek\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}"
```

### 3. 可选区分模型影响：`agent` + `stream=false`

```powershell
curl.exe -sS -D - -X POST "http://127.0.0.1:8022/v1/chat/completions" ^
  -H "Content-Type: application/json" ^
  -H "x-session-key: cid:tiny-ns-agent" ^
  --data-binary "{\"model\":\"agent\",\"stream\":false,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}"
```

## 响应摘要

### 1. `deepseek` non-stream 极小输入

- HTTP 状态：`502`
- 原始响应：

```json
{"error":{"code":"upstream_run_failed","message":"upstream run failed","type":"upstream_error"}}
```

### 2. `deepseek` stream 极小输入

- HTTP 状态：`200`
- 原始响应为 SSE，首段内容正常返回 assistant 文本，开头如下：

```text
data: {"choices":[{"delta":{"role":"assistant"},...}]}

data: {"choices":[{"delta":{"content":"Hello"},...}]}
```

- `stream_output` 日志汇总出的完整文本开头：

```text
Hello! 👋 I'm DeepSeek, an AI assistant created by DeepSeek.
```

### 3. `agent` non-stream 极小输入

- HTTP 状态：`502`
- 原始响应：

```json
{"error":{"code":"upstream_run_failed","message":"upstream run failed","type":"upstream_error"}}
```

## 日志证据

`bin\logs\proxy_8022.err` 中对应时间段日志如下。

```text
2026/03/19 06:41:51 IOLOG {"dir":"in","model":"deepseek","path":"/v1/chat/completions","payload":{"messages":[{"content":"hi","role":"user"}],"model":"deepseek","stream":false},"prompt":"**model = deepseek**\nuser: hi","session_id":"b7a53a39-db23-4efe-87e9-9ce6caa1ef4a","session_key":"cid:tiny-ns-deepseek","stream":false}
2026/03/19 06:41:56 IOLOG {"dir":"in","model":"deepseek","path":"/v1/chat/completions","payload":{"messages":[{"content":"hi","role":"user"}],"model":"deepseek","stream":true},"prompt":"**model = deepseek**\nuser: hi","session_id":"07763a4e-af0d-4c62-9018-b4ee6ff2665f","session_key":"cid:tiny-s-deepseek","stream":true}
2026/03/19 06:41:59 IOLOG {"dir":"out","model":"deepseek","path":"/v1/chat/completions","session_id":"07763a4e-af0d-4c62-9018-b4ee6ff2665f","session_key":"cid:tiny-s-deepseek","stream":true,"stream_output":"Hello! 👋 I'm DeepSeek, an AI assistant created by DeepSeek. I'm here to help you with a wide variety of questions and tasks...."}
2026/03/19 06:42:00 IOLOG {"dir":"in","model":"agent","path":"/v1/chat/completions","payload":{"messages":[{"content":"hi","role":"user"}],"model":"agent","stream":false},"prompt":"user: hi","session_id":"25f17598-dd52-4123-9c4b-dd98f8c0fc12","session_key":"cid:tiny-ns-agent","stream":false}
```

关键信号：

- 两次 `stream=false` 请求都只有 `dir=in`，没有对应 `dir=out`。
- `deepseek stream=true` 有完整 `dir=out`，说明同样的极小输入和同一 endpoint 下，stream 路径本身可以成功走通。
- 因为代理在 non-stream 成功时会记录 `dir=out`，所以缺失 `dir=out` 说明失败发生在构造 OpenAI non-stream 输出之前。

## 代码路径分析

### 1. `chat/completions` 的 non-stream 分支

`internal/http/handlers.go:115`

```go
if !parsed.Stream {
    _, runResp, err := s.gateway.Run(r.Context(), gateway.RunRequest{
        SessionID: sessionID,
        Text:      parsed.Prompt,
        Stream:    false,
        Delta:     false,
    })
    if err != nil {
        writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
        return
    }
    if msg, ok := gatewayRunError(runResp); ok {
        writeJSON(w, stdhttp.StatusBadGateway, openaiUpstreamError(msg))
        return
    }
    text := extractGatewayTextFromNonStream(runResp)
    respPayload := openai.BuildChatCompletionResponseFromText(text, parsed.Model)
    ...
}
```

这里 non-stream 会直接调用：

- `gateway.Run(... Stream:false, Delta:false)`
- 返回后先检查 `err`
- 再调用 `gatewayRunError(runResp)`
- 只要命中该判定，就直接返回 `502`

### 2. 上游调用层差异

`internal/gateway/client.go:88`

```go
func (c *Client) Run(ctx context.Context, req RunRequest) (*http.Response, map[string]any, error) {
    ...
    if req.Stream {
        return c.postStream(ctx, "/run", payload)
    }
    var out map[string]any
    if err := c.postJSON(ctx, "/run", payload, &out); err != nil {
        return nil, nil, err
    }
    return nil, out, nil
}
```

结论：

- `stream=true` 走 `postStream`，按 SSE 读取。
- `stream=false` 走 `postJSON`，把 `/run` 响应解成 JSON map，再由上层判定是否失败。

### 3. non-stream 被判失败的具体位置

`internal/http/handlers.go:449`

```go
func gatewayRunError(runResp map[string]any) (string, bool) {
    if runResp == nil {
        return "", false
    }
    if ok, has := runResp["success"].(bool); has && !ok {
        if msg := extractGatewayErrorMessage(runResp); msg != "" {
            return msg, true
        }
        return "upstream run failed", true
    }
    if msg := extractGatewayErrorMessage(runResp); msg != "" {
        return msg, true
    }
    if data, ok := runResp["data"].(map[string]any); ok {
        if _, has := data["error"]; has {
            return "upstream run failed", true
        }
    }
    return "", false
}
```

从最终对外响应看：

```json
{"error":{"code":"upstream_run_failed","message":"upstream run failed","type":"upstream_error"}}
```

这说明：

- `gateway.Run(... Stream:false)` 没有触发 HTTP 传输层错误，否则会走 `err != nil` 分支。
- 代理拿到了一个可解析 JSON 的 `runResp`。
- 但该 `runResp` 被 `gatewayRunError(runResp)` 判定为失败，于是直接映射成 `502 upstream_run_failed`。

## 与 `stream=true` 的最小对照结论

在请求体几乎完全相同、输入只有 `hi` 的前提下：

- `deepseek + stream=false` 失败
- `deepseek + stream=true` 成功
- `agent + stream=false` 也失败

因此可以排除：

- 不是输入过长导致
- 不是极小输入内容本身导致
- 不是 `deepseek` 模型特有问题

更接近的事实是：

- 失败集中发生在 `/run` 的 non-stream 语义或返回结构上
- 当前代理对 non-stream `/run` 返回体的成功/失败判定，与上游实际返回不兼容

## 最可能 Root Cause

最可能的根因不是 prompt 长度，也不是是否带 tools，而是：

- 代理的 non-stream 路径依赖 `/run` 返回一个可被 `gatewayRunError` 视为成功的 JSON 结构。
- 当前上游在 `stream=false` 时返回了一个 JSON，但其中包含了会触发 `gatewayRunError` 的字段形态，例如：
  - `success=false`
  - 可提取的错误字段
  - 或 `data.error`
- 同一输入在 `stream=true` 下可以正常产出文本，说明模型推理能力和基础会话链路没有问题；问题集中在 non-stream `/run` 返回格式或成功标志，与当前代理解析逻辑不匹配。

## 结论

本次最小复现表明：

- `stream=false` 路径无法通过，与输入是否极小无关。
- 该问题至少影响 `deepseek` 和 `agent` 两种模型，不是单模型问题。
- 失败发生在代理 `chat/completions` non-stream 分支拿到上游 `/run` JSON 之后、构造 OpenAI 输出之前。
- 最可能 root cause 是：上游 non-stream `/run` 返回体被当前代理的 `gatewayRunError` 判定为失败，导致统一映射成 `502 upstream_run_failed`。

## 修复后验证

最小 non-stream 请求（deepseek + stream=false + hi）：

```powershell
curl.exe -sS -D - -X POST "http://127.0.0.1:8022/v1/chat/completions" ^
  -H "Content-Type: application/json" ^
  -H "x-session-key: cid:tiny-ns-deepseek" ^
  --data-binary "{\"model\":\"deepseek\",\"stream\":false,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}"
```

结果：HTTP 200，返回 `chat.completion` JSON，不再是 502（示例：`"object":"chat.completion"`）。

定向测试：`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./internal/http -v` PASS。

全量测试：`C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe test ./... -v` PASS。


