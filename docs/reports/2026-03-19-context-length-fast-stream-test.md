# 主口径上下文长度测试报告（fast）

## 基本信息

- 日期：2026-03-19
- 目标 endpoint：`http://127.0.0.1:8022/v1/chat/completions`
- 口径：沿用 deep 主链路模板，仅将 `model` 改为 `fast`
- 关键参数：`stream=true`、带 `tools`、`tool_choice=auto`、多轮 `messages`、保留最小工具调用闭环
- 约束：本次仅落盘报告，不改代码

## 请求模板摘要（与 deep 主链路一致，仅替换 model）

- `model=fast`
- `stream=true`
- `tool_choice=auto`
- `tools`：1 个 `builtin_web_search` 函数工具
- `messages`（5 条，含最小工具闭环）：
  1) `user`：你好
  2) `assistant`：你好，我是测试助手。
  3) `user`：固定测试指令 + 长度填充 `$payloadText`
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
  model = 'fast'
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
  -Headers @{ 'Content-Type'='application/json'; 'x-client-id'='fast-mainline-<len>-<n>' } `
  -Body $body `
  -TimeoutSec 180
```

## 实测档位与结果

- 档位：`32k / 48k / 64k / 80k / 160k / 200k`
- 每档：`2/2` 成功
- HTTP 状态：全部 `200`
- SSE：均返回有效 assistant `content`
- 耗时区间（约）：
  - `32k`：`1872-1950ms`
  - `48k`：`2048-2102ms`
  - `64k`：`2227-2509ms`
  - `80k`：`2229-2290ms`
  - `160k`：`2.8-2.9s`
  - `200k`：`3.2-3.6s`

说明：本轮高位点复核已验证 `160k/200k`，未补全所有中间阶梯。

## 结论

- 已验证上限至少 `200k`。
- 生产保守黄金长度建议：`64k`。
