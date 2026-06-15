# 2026-06-14 真实工具调用往返测试报告

## 背景

针对 `Qwen3.6-27B` 的本地 tool shim，验证不依赖 `/v1/responses`、不依赖 agent 执行器时，是否能够完成一次实际的 OpenAI 兼容工具调用往返：

1. 第一轮请求返回 `tool_calls`
2. 调用方本地执行工具
3. 第二轮把 `assistant.tool_calls + tool` 消息回传
4. 模型基于工具结果返回最终答案

## 测试命令

```powershell
& 'D:\GO\bin\go.exe' test ./internal/http ./internal/openai
& 'D:\GO\bin\go.exe' build ./...
```

## 覆盖用例

- `TestCompatibleChatCompletionsLocalToolShim`
  - 验证第一轮可返回 `tool_calls`
- `TestCompatibleChatCompletionsLocalToolShimNoTool`
  - 验证无需工具时直接返回最终文本
- `TestCompatibleChatCompletionsLocalToolShimStream`
  - 验证流式场景可返回 `tool_calls` SSE
- `TestCompatibleChatCompletionsRealToolRoundTrip`
  - 验证真实两轮工具调用往返
  - 第一轮返回 `get_weather`
  - 本地注入工具结果 `北京当前晴，26C`
  - 第二轮返回最终答案 `北京当前晴，26C。`

## 结果摘要

- `go test` 通过
- `go build` 通过
- 已验证当前项目可以承接“兼容模式下的真实工具调用往返测试”
- 当前实现仍是“请求级循环”：
  - 每一轮由调用方重新发起 `/v1/chat/completions`
  - 服务端负责把 `tools` 转成纯聊天规划，再回转为 OpenAI 风格响应
- 这已经足够支持连续工具调用测试，只要调用方继续按 OpenAI 规范把中间 `tool` 消息带回即可

## 当前边界

- 服务端当前没有内建工具注册表，也不会在进程内直接执行工具
- 连续多轮工具调用依赖客户端/测试代码充当 orchestrator
- 这次验证的重点是：兼容协议、两轮往返、最终答案回收链路可用
