# deepseek 1M 单次上下文长度测试报告

## 基本信息

- 日期：2026-03-19
- 代理：`http://127.0.0.1:8022`
- endpoint：`/v1/chat/completions`
- 口径：`model=deepseek`、`stream=true`、带 `tools`、`tool_choice=auto`、多轮 `messages` 最小工具闭环
- 本次规模：`1,000,000` 字符级 payload 的单次测试

## 请求模板摘要

- `model=deepseek`
- `stream=true`
- `tool_choice=auto`
- `tools`：1 个 `builtin_web_search` 函数工具
- `messages`（5 条，含最小工具闭环）：
  1) `user`：你好
  2) `assistant`：你好，我是测试助手。
  3) `user`：固定测试指令 + 1,000,000 字符级长度填充 `$payloadText`
  4) `assistant.tool_calls`：1 个 `builtin_web_search`
  5) `tool`：极短模拟工具结果

## 关键命令（PowerShell 片段）

```powershell
$tool = @{
  type = 'function'
  function = @{
    name = 'builtin_web_search'
    description = 'Web search tool'
    parameters = @{
      type = 'object'
      properties = @{ additionalContext = @{ type = 'string' } }
    }
  }
}

$bodyObj = @{
  model = 'deepseek'
  stream = $true
  tool_choice = 'auto'
  tools = @($tool)
  messages = @(
    @{ role = 'user'; content = '你好' },
    @{ role = 'assistant'; content = '你好，我是测试助手。' },
    @{ role = 'user'; content = "请基于已有上下文继续，但这次如果不需要工具就直接回答 OK。`n$payloadText" },
    @{ role = 'assistant'; content = ''; tool_calls = @(@{ id = 'call_1'; type = 'function'; function = @{ name = 'builtin_web_search'; arguments = '{}' } }) },
    @{ role = 'tool'; tool_call_id = 'call_1'; content = '[{"type":"text","text":"示例搜索结果：无需继续调用工具，可直接总结。"}]' }
  )
}

$body = $bodyObj | ConvertTo-Json -Depth 12 -Compress
Invoke-WebRequest -UseBasicParsing -SkipHttpErrorCheck `
  -Uri 'http://127.0.0.1:8022/v1/chat/completions' `
  -Method Post `
  -Headers @{ 'Content-Type'='application/json'; 'x-client-id'='deepseek-1m-single' } `
  -Body $body `
  -TimeoutSec 180
```

## 结果

- HTTP：`200`
- SSE：未出现有效 `content` 或 `tool_calls`
- 耗时：约 `5.1s`
- 失败形态：起始 `role=assistant` 后直接空 `delta` + `finish_reason:"stop"`
- 归类：`empty_200`

## 结论

`deepseek` 1M 单次测试未通过，失败不是 5xx/超时，而是空成功返回；因此先前 200k 单次通过并不能外推到 1M。
