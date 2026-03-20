# 主口径上下文长度测试报告

## 基本信息

- 日期：2026-03-19
- 目标 endpoint：`http://127.0.0.1:8022/v1/chat/completions`
- 主口径：`model=deepseek`、`stream=true`、带 `tools`、`tool_choice=auto`、多轮 `messages`、保留最小工具调用闭环
- 约束：不改代码、不提交
- 代理健康检查：`GET /health` 返回 `{"ok":true}`

## 请求模板摘要

本次模板先从 `bin\logs\proxy_8022.err` 的真实成功窗口请求提炼，参考日志：

- `bin\logs\proxy_8022.err:74`：真实窗口 `dir=in`，可见 `model=deepseek`、`stream=true`、`tool_choice=auto`、带 `tools`、多轮 `messages`
- `bin\logs\proxy_8022.err:75`：对应 `dir=out`，返回 `finish_reason=tool_calls`
- `bin\logs\proxy_8022.err:76`：真实窗口进入工具回填后的下一轮请求，证明窗口链路依赖多轮闭环而不是单轮请求

在此基础上，构造最小可复现模板，保留 1 个工具和 5 条消息：

1. 旧 `user`：`你好`
2. 旧 `assistant`：`你好，我是测试助手。`
3. 新 `user`：固定测试指令 + 长度填充
4. `assistant.tool_calls`：1 个 `builtin_web_search`
5. `tool`：极短模拟工具结果

工具定义最小化为 1 个函数工具：`builtin_web_search`。

## 命令摘要

测试通过 PowerShell 循环调用 `Invoke-WebRequest` 执行，核心请求骨架如下：

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
  -Headers @{ 'Content-Type'='application/json'; 'x-client-id'='mainline-probe-<len>-<n>' } `
  -Body $body `
  -TimeoutSec 180
```

长度档位：`512, 1k, 2k, 4k, 8k, 16k, 24k, 32k, 48k, 64k, 80k`

每档执行 3 次，记录：

- HTTP 状态
- 是否返回有效 assistant 文本或有效 `tool_calls`
- 延迟
- 错误摘要

有效性判定：SSE 流中出现非空 assistant `content`，或出现合法 `tool_calls`，均记为成功。

## 结果表

| 档位 | 成功率 | HTTP 状态 | 有效输出类型 | 延迟范围 | 错误摘要 |
| --- | --- | --- | --- | --- | --- |
| `512` | `3/3` | 全部 `200` | `content, content, content` | `1765-1896ms` | 无 |
| `1k` | `3/3` | 全部 `200` | `content, content, tool_calls` | `1772-3243ms` | 无 |
| `2k` | `3/3` | 全部 `200` | `content, content, content` | `1681-1764ms` | 无 |
| `4k` | `3/3` | 全部 `200` | `content, tool_calls, tool_calls` | `1674-4593ms` | 无 |
| `8k` | `3/3` | 全部 `200` | `content, content, content` | `1723-1966ms` | 无 |
| `16k` | `3/3` | 全部 `200` | `content, content, content` | `1832-1927ms` | 无 |
| `24k` | `3/3` | 全部 `200` | `content, content, content` | `1837-1944ms` | 无 |
| `32k` | `3/3` | 全部 `200` | `content, content, tool_calls` | `1993-3538ms` | 无 |
| `48k` | `3/3` | 全部 `200` | `content, content, content` | `1933-2243ms` | 无 |
| `64k` | `3/3` | 全部 `200` | `content, content, tool_calls` | `2175-3474ms` | 无 |
| `80k` | `3/3` | 全部 `200` | `content, content, tool_calls` | `1968-3684ms` | 无 |

## 观察

- 在本次“主口径”下，`512` 到 `80k` 全部 `3/3` 成功，没有出现 HTTP 错误，也没有出现无输出直接结束。
- `80k` 档位仍然可用，说明此前 `stream=false + 无 tools + 单轮 messages` 的失败结论，不适用于真实聊天窗口转接链路。
- 输出类型存在波动：同一长度下，有时直接返回 assistant 文本，有时先返回 `tool_calls`。这符合 `tool_choice=auto` 的行为特征，因此本次以“有效文本或有效 `tool_calls`”作为成功标准。
- 延迟总体稳定在约 `1.7s - 3.7s`，少数波动到 `4.6s`，但未随长度增加出现明显灾难性恶化。

## 结论

### 已验证上限

- 在当前代理、当前主口径、当前最小闭环模板下，已实测验证到 `80k`，且 `80k` 为 `3/3` 成功。
- 因为用户要求的上限即 `80k`，本次没有继续上探更高档位。

### 推荐的主口径生产黄金长度

建议主口径生产黄金长度取：`64k`。

原因：

- `64k` 已在本次测试中 `3/3` 成功。
- 相比 `80k`，`64k` 为真实业务中的额外系统提示、工具 schema、历史消息抖动留出更稳妥余量。
- `32k` 以上开始可见输出模式和延迟有一定波动，但没有失败；因此黄金长度宜取“已验证稳定且保留缓冲”的保守值，而不是直接贴着本次验证上限运行。

补充判断：

- 如果业务目标是“尽量吃满上下文”，当前可操作结论是：`80k` 在本模板下可用。
- 如果业务目标是“生产默认值”，`64k` 比 `80k` 更稳，`48k` 则更保守。

## 可判定性说明

本次可以判定：

- 错误口径不是主链路上限依据。
- 主口径在当前环境下可稳定工作到 `80k`。
- 生产黄金长度可保守建议为 `64k`。

未覆盖项：

- 本次模板是“从真实窗口提炼后的最小闭环模板”，不是完整真实窗口全量工具集合。
- 因此本报告给出的是当前主口径的可用生产建议，不等于所有更复杂工具集、更多历史消息、更多系统提示下的绝对硬上限。


