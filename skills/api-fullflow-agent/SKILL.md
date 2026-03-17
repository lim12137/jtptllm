---
name: api-fullflow-agent
description: 当你需要基于 api.txt（或 APP_KEY/agentCode/agentVersion 这 3 个参数）快速搭建并运行“createSession -> run -> (feedback) -> deleteSession”全流程的 API 智能体调用器，并同时提供 OpenAI 旧接口 /v1/chat/completions 与新接口 /v1/responses 兼容代理时使用。
---

# API Fullflow Agent

## Overview

本 skill 提供一套可直接运行的“智能体网关 API”全流程调用器（含流式/非流式），并内置一个 OpenAI 协议兼容代理服务，方便把你的网关能力接入任何 OpenAI SDK/工具链。

## Quick Start（你只需要这 3 个）

优先使用 `api.txt`，里面只要有：`key`、`agentCode`、`agentVersion`。

如果没有 `api.txt`，就询问用户提供这 3 个值（或让用户设置环境变量）：
- `APP_KEY`（或 `key`）
- `agentCode`
- `agentVersion`（可选；若接口强制要求则必填）

## What To Run（脚本入口）

脚本都在 `scripts/`：

### 1) 一次性全流程调用（CLI）

非流式：

```bash
python skills/api-fullflow-agent/scripts/run_fullflow.py --api-txt api.txt --text "你好"
```

流式：

```bash
python skills/api-fullflow-agent/scripts/run_fullflow.py --api-txt api.txt --text "你好" --stream
```

默认 Base URL 已按文档写死在代码中；如果要覆盖：

```bash
python skills/api-fullflow-agent/scripts/run_fullflow.py --api-txt api.txt --base-url "http://xxx/agent/api" --text "你好"
```

### 2) 启动 OpenAI 兼容代理（推荐）

启动服务：

```bash
python skills/api-fullflow-agent/scripts/openai_proxy_server.py --api-txt api.txt --port 8000
```

然后你可以用 curl（或 OpenAI SDK）调用：

旧接口（Chat Completions）：

```bash
curl http://127.0.0.1:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"agent\",\"messages\":[{\"role\":\"user\",\"content\":\"你好\"}]}"
```

新接口（Responses）：

```bash
curl http://127.0.0.1:8000/v1/responses \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"agent\",\"input\":\"你好\"}"
```

流式：把请求体里加 `\"stream\": true`，并使用能显示 SSE 的客户端读取即可。

## How It Works（流程）

1. 从 `api.txt` 或环境变量读取 `APP_KEY/agentCode/agentVersion`。
2. `createSession` 获取 `sessionId`（文档里叫 `uniqueCode`）。
3. 调用 `run`：
   - 非流式：直接返回 JSON，从中提取最终文本。
   - 流式：解析 `data: {...}` 行，抽取增量文本并转成 OpenAI SSE。
4. 可选调用 `feedback`（需要 requestId/taskId；脚本会尽力从返回/流里提取）。
5. finally 里调用 `deleteSession` 清理会话。

## Reference

接口字段/路径/错误码等在 [api_reference.md](references/api_reference.md)。
