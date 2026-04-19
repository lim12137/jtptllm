# Codubuddy CLI OpenAI Backend

本项目应作为 `codubuddy CLI` 的 OpenAI 兼容后端接入，而不是作为自定义 CLI RPC 后端接入。

## 接入原则

- 优先使用 `http://127.0.0.1:8022/v1/responses`
- 如 CLI 只支持传统 Chat Completions，则使用 `http://127.0.0.1:8022/v1/chat/completions`
- 模型探测可使用 `GET http://127.0.0.1:8022/v1/models`
- 建议模型名使用 `agent`
- 如 CLI 强制要求 API key，可使用占位值，例如 `dummy`

## 最小配置思路

- `base_url = http://127.0.0.1:8022/v1`
- `model = agent`
- `api_key = dummy`

如果 `codubuddy CLI` 提供 OpenAI 兼容环境变量，可按同等含义设置：

```powershell
$env:OPENAI_BASE_URL = 'http://127.0.0.1:8022/v1'
$env:OPENAI_API_KEY = 'dummy'
```

## 推荐验证顺序

1. 启动代理：`bin\proxy.exe`
2. 访问健康检查：`GET /health`
3. 调用 `GET /v1/models` 确认模型发现可用
4. 先验证 `POST /v1/responses` 的工具调用
5. 再验证 `POST /v1/chat/completions` 的兼容性
6. 运行批量脚本：`powershell -File scripts/codubuddy_toolcall_validation.ps1`

## 为什么优先 `/v1/responses`

- 仓库已实现 OpenAI Responses 风格的 `function_call` 输出
- 对工具调用和后续兼容扩展更自然
- 若 CLI 能识别新版 OpenAI 语义，这条面最稳

## 本仓库已验证的能力

- `tools` / `tool_choice` 入站兼容
- `tool_calls` / `function_call` 出站兼容
- `/v1/chat/completions` 与 `/v1/responses` 双端点
- 文档摘要类 prompt 可触发结构化工具调用

## 已知限制

- 当前工作区未发现可直接执行的 `codubuddy` 本机二进制，因此仓库内验证脚本采用“直接访问 OpenAI 兼容 HTTP”方式代替 CLI 实际进程
- Python 运行环境当前不可用，因此 `skills/api-fullflow-agent/scripts/*.py` 未作为本轮主验证入口
- 若后续确认 `codubuddy CLI` 有专属配置文件格式，可在此文档基础上补具体字段映射
