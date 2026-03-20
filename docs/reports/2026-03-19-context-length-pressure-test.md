# Context Length Pressure Test

**Date:** 2026-03-19  
**Target:** `http://127.0.0.1:8022/v1/chat/completions`  
**Proxy Mode:** Go debug proxy (`go run ./cmd/proxy`)  
**Model:** `deepseek`  
**Request Shape:** non-tool, non-stream, single user message

## Baseline

- Health check:
  - Command: `Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8022/health`
  - Result: `200` with `{"ok":true}`
- Go debug proxy confirmation:
  - Evidence from `bin\logs\proxy_8022.err`: `proxy server starting on :8022`

## Test Method

- Length unit: user-message payload character count (`A` repeated N times)
- Length tiers:
  - `512`, `1024`, `2048`, `4096`, `8192`, `16384`, `24576`, `32768`, `49152`, `65536`, `81920`
- Per-tier repetitions:
  - 3 runs each
- Success criteria:
  - HTTP `200`
  - `choices[0].message.content` is non-empty text
- Request body pattern:
  - `model=deepseek`
  - `stream=false`
  - no `tools`
  - prompt prefix: `你是长度压力测试助手。请只回复 OK。`

## Command Summary

Pressure test was executed with an inline PowerShell loop equivalent to:

```powershell
$sizes=@(512,1024,2048,4096,8192,16384,24576,32768,49152,65536,81920)
foreach($size in $sizes){
  1..3 | ForEach-Object {
    $payloadText = 'A' * $size
    $body = @{
      model='deepseek'
      stream=$false
      messages=@(@{ role='user'; content="你是长度压力测试助手。请只回复 OK。`n$payloadText" })
    } | ConvertTo-Json -Depth 6 -Compress
    Invoke-WebRequest -UseBasicParsing -SkipHttpErrorCheck `
      -Uri 'http://127.0.0.1:8022/v1/chat/completions' `
      -Method Post `
      -Headers @{ 'Content-Type'='application/json'; 'x-client-id'='ctx-pressure-test' } `
      -Body $body
  }
}
```

## Result Summary

| Length | Runs | Success | Fail | Status | Latency ms (min-avg-max) | Error |
| --- | ---: | ---: | ---: | --- | --- | --- |
| 512 | 3 | 0 | 3 | 502 | 1193-1588-2376 | `upstream_run_failed` |
| 1024 | 3 | 0 | 3 | 502 | 1188-1197-1208 | `upstream_run_failed` |
| 2048 | 3 | 0 | 3 | 502 | 1201-1202-1203 | `upstream_run_failed` |
| 4096 | 3 | 0 | 3 | 502 | 1204-1255-1285 | `upstream_run_failed` |
| 8192 | 3 | 0 | 3 | 502 | 1195-1200-1205 | `upstream_run_failed` |
| 16384 | 3 | 0 | 3 | 502 | 1197-1198-1200 | `upstream_run_failed` |
| 24576 | 3 | 0 | 3 | 502 | 1208-1216-1227 | `upstream_run_failed` |
| 32768 | 3 | 0 | 3 | 502 | 1216-1218-1220 | `upstream_run_failed` |
| 49152 | 3 | 0 | 3 | 502 | 1234-1236-1239 | `upstream_run_failed` |
| 65536 | 3 | 0 | 3 | 502 | 1231-1240-1246 | `upstream_run_failed` |
| 81920 | 3 | 0 | 3 | 502 | 1241-1249-1261 | `upstream_run_failed` |

## Observations

- No tier produced a valid text response.
- Failure started at the very first tier (`512`) and remained stable through `80k`.
- Latency stayed roughly flat around `1.2s`, which suggests the current blocker is not context-size growth.
- Because all tiers failed with the same `502 upstream_run_failed`, there was no meaningful threshold transition to densify around.

## Log Evidence

- During test runs, `bin\logs\proxy_8022.err` continuously recorded `IOLOG dir=in` request entries.
- No matching successful `IOLOG dir=out` response entries were observed for this pressure-test batch.
- Current evidence indicates the failure occurs on the upstream run path before a valid completion payload is returned.

## Conclusion

- Maximum passing tier:
  - None
- Recommended production golden length:
  - Not available under the current upstream state
- Conservative production recommendation:
  - Do not use current results to set a production context limit
  - First fix the upstream `upstream_run_failed` baseline on a tiny request (`512`)
  - After baseline recovery, re-run the same tiered test to derive a real golden length

## Risk Notes

- This round measures current end-to-end availability, not actual model context ceiling.
- Because even `512` fails, any inferred “80k upper bound” would be technically invalid.
- If Cherry Studio real traffic succeeds while this direct API test fails, the next debugging step should compare the exact request shape differences:
  - model name
  - headers
  - message structure
  - tool/tool_choice fields

## Sub-512 Follow-Up

- Follow-up date:
  - `2026-03-19`
- Goal:
  - verify whether the direct API path recovers below `512`
- Additional tiers:
  - `64`, `128`, `256`, `384`, `512`
- Repetitions:
  - 3 runs each
- Request shape:
  - unchanged from the main test
  - `model=deepseek`
  - `stream=false`
  - no `tools`
  - single user message

### Sub-512 Result Summary

| Length | Runs | Success | Fail | Status | Latency ms (min-avg-max) | Error |
| --- | ---: | ---: | ---: | --- | --- | --- |
| 64 | 3 | 0 | 3 | 502 | 1199-1259-1363 | `upstream_run_failed` |
| 128 | 3 | 0 | 3 | 502 | 79-836-1216 | mixed: `upstream_run_failed` / `当前会话正在处理上个请求,请稍后` |
| 256 | 3 | 0 | 3 | 502 | 82-827-1203 | mixed: `upstream_run_failed` / `当前会话正在处理上个请求,请稍后` |
| 384 | 3 | 0 | 3 | 502 | 1206-1212-1221 | `upstream_run_failed` |
| 512 | 3 | 0 | 3 | 502 | 1217-2225-4210 | `upstream_run_failed` |

### Sub-512 Conclusion

- The direct API path does **not** recover below `512`.
- Since even `64` still fails and no valid text response appears, the current direct API chain is not usable for context-length evaluation.
- A small subset of runs hit a session-busy upstream error (`当前会话正在处理上个请求,请稍后`), but this does not change the overall conclusion: the baseline path remains unavailable.


