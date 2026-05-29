# 空回复故障切换测试报告

日期：2026-05-29

## 变更目标

为 Go 代理新增空回复保护：
- 当上游 agent 调用成功，但提取出的回复文本为空时，不再返回 200 空内容。
- 统一返回 HTTP `502`。
- 错误体增加稳定错误码 `empty_agent_response`，供调用方判断并执行故障切换。

## 涉及接口

- `POST /v1/chat/completions`
- `POST /v1/responses`

## 测试命令

```powershell
go test ./...
```

## 测试结果摘要

- 未能在当前环境执行自动化测试。
- 原因：系统未安装 `go` 命令，执行时报错 `The term 'go' is not recognized as a name of a cmdlet...`
- 已补充单元测试代码，覆盖以下场景：
  - `/v1/chat/completions` 非流式请求在空回复时返回 `502`
  - `/v1/responses` 非流式请求在空回复时返回 `502`
  - 错误体中的 `error.code` 与 `error.type` 均为 `empty_agent_response`

## 人工检查结论

- 非流式返回路径已在写出 OpenAI 兼容响应前增加空字符串判断。
- 普通网关异常仍保持 `agent_gateway_error`。
- 新增错误码为稳定字符串，适合上层按码做故障切换。

## 后续建议

在具备 Go 环境后执行：

```powershell
go test ./...
```
