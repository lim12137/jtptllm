# 2026-04-20 `<tool_call>...</tool_call>` 协议 80k/120k 定向补测

## 目标

- 只补测 `<tool_call>...</tool_call>` 协议在约 `80k` 和 `120k` 长上下文下的遵从情况。
- 每个长度至少 `3` 次。
- 同时记录 HTTP 返回证据与 raw 证据。

## 使用方式

- 复用现有脚本：`scripts/protocol_adherence_experiment.ps1`
- 实际执行命令：

```powershell
& '.\scripts\protocol_adherence_experiment.ps1' `
  -BaseUrl 'http://127.0.0.1:8022' `
  -Model 'agent' `
  -RunsPerCell 3 `
  -RestartProxy:$true
```

## 说明

- 脚本会覆盖三种协议和三个长度档位，我只提取其中 `protocol_id = "xml"` 且 `context_tier in ("ctx80k","ctx120k")` 的结果。
- 本轮完整实验请求 `27/27` 全部跑完，脚本在最终汇总 markdown 阶段抛出 `Argument types do not match`，但 raw/http/summary 证据均已落盘。
- 本次补测主证据目录使用最新一轮完整产物：
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016`

## 汇总结论

| 长度档 | 轮次 | raw 观测 | HTTP 观测 | 判定 |
| --- | --- | --- | --- | --- |
| `80k` | `3/3` | `observed.txt` 为空，仅含换行 | 全部返回结构化 `tool_calls` SSE，`finish_reason="tool_calls"` | 结构化成功 |
| `120k` | `3/3` | `observed.txt` 为空，仅含换行 | 全部返回结构化 `tool_calls` SSE，`finish_reason="tool_calls"` | 结构化成功 |

补充解释：

- 如果按脚本的 raw 分类口径，这 6 次都被记为 `no_protocol`，因为代理 IOLOG 中用于分类的 `observed_text` 为空，没有保留下游可见的 `<tool_call>...</tool_call>` 文本块。
- 但从客户端真正收到的 HTTP SSE 看，6 次全部是结构化 `tool_calls` 成功，没有自然语言污染，也没有截断。
- 所以这次面向“用户实际收到什么”的结论，应判为 **结构化成功**。

## Summary JSON 摘要

来源：`docs/reports/protocol-adherence/raw/2026-04-20_001016/summary.json`

| protocol_id | context_tier | samples | adherence_count | natural_language_contamination_count | truncation_incomplete_count | no_protocol_count |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| `xml` | `ctx80k` | `3` | `0` | `0` | `0` | `3` |
| `xml` | `ctx120k` | `3` | `0` | `0` | `0` | `3` |

## Raw 证据

`observed.txt` 全部为空白，仅含换行：

- `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-01-observed.txt`
- `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-02-observed.txt`
- `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-03-observed.txt`
- `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-01-observed.txt`
- `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-02-observed.txt`
- `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-03-observed.txt`

解释：

- raw 侧没有看到泄漏出来的 `<tool_call>...</tool_call>` 文本。
- 这也意味着没有“前后夹带自然语言”的污染证据。
- 同时没有只出现开标签、不出现闭标签的截断证据。

## HTTP 证据

### 80k

`xml-ctx80k-01-http-response.txt`

```text
data: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"location\":\"Paris\",\"unit\":\"c\"}","name":"get_weather"},"id":"call_aqrez7p3d07q","index":0,"type":"function"}]},"finish_reason":null,"index":0}],"created":1776615160,"id":"chatcmpl_dft5e69fjtal","model":"agent","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{},"finish_reason":"tool_calls","index":0}],"created":1776615160,"id":"chatcmpl_dft5e69fjtal","model":"agent","object":"chat.completion.chunk"}
data: [DONE]
```

`xml-ctx80k-02-http-response.txt`

```text
data: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"location\":\"Paris\",\"unit\":\"c\"}","name":"get_weather"},"id":"call_zlbrhcnsbifm","index":0,"type":"function"}]},"finish_reason":null,"index":0}],"created":1776615166,"id":"chatcmpl_uiuwcyifmwok","model":"agent","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{},"finish_reason":"tool_calls","index":0}],"created":1776615166,"id":"chatcmpl_uiuwcyifmwok","model":"agent","object":"chat.completion.chunk"}
data: [DONE]
```

`xml-ctx80k-03-http-response.txt`

```text
data: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"location\":\"Paris\",\"unit\":\"c\"}","name":"get_weather"},"id":"call_z8nnur4769cs","index":0,"type":"function"}]},"finish_reason":null,"index":0}],"created":1776615171,"id":"chatcmpl_wdxhmnqjwezm","model":"agent","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{},"finish_reason":"tool_calls","index":0}],"created":1776615171,"id":"chatcmpl_wdxhmnqjwezm","model":"agent","object":"chat.completion.chunk"}
data: [DONE]
```

### 120k

`xml-ctx120k-01-http-response.txt`

```text
data: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"location\":\"Paris\",\"unit\":\"c\"}","name":"get_weather"},"id":"call_v4sb5quvveen","index":0,"type":"function"}]},"finish_reason":null,"index":0}],"created":1776615178,"id":"chatcmpl_hdmcp1iz3gwv","model":"agent","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{},"finish_reason":"tool_calls","index":0}],"created":1776615178,"id":"chatcmpl_hdmcp1iz3gwv","model":"agent","object":"chat.completion.chunk"}
data: [DONE]
```

`xml-ctx120k-02-http-response.txt`

```text
data: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"location\":\"Paris\",\"unit\":\"c\"}","name":"get_weather"},"id":"call_bcdllmaqx0li","index":0,"type":"function"}]},"finish_reason":null,"index":0}],"created":1776615185,"id":"chatcmpl_dh3506a8un1f","model":"agent","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{},"finish_reason":"tool_calls","index":0}],"created":1776615185,"id":"chatcmpl_dh3506a8un1f","model":"agent","object":"chat.completion.chunk"}
data: [DONE]
```

`xml-ctx120k-03-http-response.txt`

```text
data: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"location\":\"Paris\",\"unit\":\"c\"}","name":"get_weather"},"id":"call_305toghiqav5","index":0,"type":"function"}]},"finish_reason":null,"index":0}],"created":1776615193,"id":"chatcmpl_a1gxwcpj9rfv","model":"agent","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{},"finish_reason":"tool_calls","index":0}],"created":1776615193,"id":"chatcmpl_a1gxwcpj9rfv","model":"agent","object":"chat.completion.chunk"}
data: [DONE]
```

## 证据文件索引

- Summary:
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/summary.json`
- Proxy raw snapshot:
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/proxy_8022.err.snapshot`
- 80k:
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-01-prompt.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-01-http-response.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-01-observed.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-02-prompt.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-02-http-response.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-02-observed.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-03-prompt.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-03-http-response.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx80k-03-observed.txt`
- 120k:
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-01-prompt.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-01-http-response.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-01-observed.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-02-prompt.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-02-http-response.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-02-observed.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-03-prompt.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-03-http-response.txt`
  - `docs/reports/protocol-adherence/raw/2026-04-20_001016/xml-ctx120k-03-observed.txt`

## 最终结论

- `<tool_call>` 在 `80k`：**结构化成功**
- `<tool_call>` 在 `120k`：**结构化成功**
- 本轮未见文本污染。
- 本轮未见截断。
- 需要注意的是，按脚本 raw 分类口径它会显示为 `no_protocol`，原因不是失败，而是代理已经把 XML 工具调用吃掉并规范化成 HTTP 层的 `tool_calls` 了。
