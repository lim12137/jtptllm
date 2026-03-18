# Toolcall Failure Investigation (Phase 1)

Date: 2026-03-18

## Environment & Status

- Repo status: `master...origin/master [ahead 11]` (no staged changes)
- Working dir: `C:\Users\Administrator\Desktop\人工智能项目\低代码智能体\api调用`
- Logs:
  - `proxy_8022.log` exists, empty
  - `proxy_8022.err` tail: `2026/03/18 15:45:13 proxy server starting on :8022`
- Port 8022:
  - `netstat -ano | findstr :8022` shows LISTENING
  - PID 33856 → `proxy.exe`

## Repro Steps

1) Run toolcall smoke script:
```powershell
powershell -File scripts/codex_toolcall_smoke.ps1
```
Output summary:
- `/v1/chat/completions` → `Status: 200`
  - Content: `message.content=""`, no `tool_calls`, `finish_reason="stop"`
- `/v1/responses` → `Status: 200`
  - Content: `output_text=""`, no `function_call` items

2) Health check (direct):
```powershell
Invoke-WebRequest http://127.0.0.1:8022/health -UseBasicParsing
```
Result: `HTTP 200` with body `{"ok":true}`

## Evidence Summary

- Proxy is running (`proxy.exe` listening on 8022).
- Health endpoint OK.
- Toolcall smoke request uses `tools` + `tool_choice`, but responses contain **empty content** and **no tool_calls/function_call**.
- No IO log output captured in `proxy_8022.log` (file is empty); `proxy_8022.err` only shows startup line.

## Initial Layer Assessment (Phase 1 only)

Current evidence suggests the “tool call failure with no feedback” likely occurs **before toolcall parsing/output**:
- HTTP handler returns 200 with empty content (no tool_calls).
- No visible upstream/toolcall mapping evidence in logs (IO logging not enabled).

Next debugging focus (Phase 2): verify upstream responses and OpenAI mapping path with IO logs enabled and/or targeted request logging to pinpoint whether failure is in HTTP handler, OpenAI mapping, gateway, or upstream model output.

---

## Phase 2: Pattern Comparison & Differences

### 1) Smoke script request shape

`scripts/codex_toolcall_smoke.ps1`:
- Endpoints: `POST /v1/chat/completions`, `POST /v1/responses`
- `stream`: not set (defaults to false)
- Uses `tools` + `tool_choice` (function tool)
- Model: `"agent"`

### 2) Raw HTTP response (script-equivalent)

`POST /v1/chat/completions` (non-stream):
- Status: 200
- Body: `message.content=""`, **no** `tool_calls`, `finish_reason="stop"`

`POST /v1/responses` (non-stream):
- Status: 200
- Body: `output_text=""`, **no** `function_call` item

Conclusion: **server response is empty**, not a client parsing issue.

### 3) Expected behavior from prior report

From `docs/reports/2026-03-18-codex-toolcall-smoke.md`:
- Some runs returned **tool_calls/function_call** when upstream responded.
- Failures were linked to **upstream `/agent/api/run` 500** or missing toolcall outputs.

### 4) IOLOG evidence with PROXY_LOG_IO=1

`proxy_8022.err` tail (with IO logging enabled):
- `IOLOG dir=in` shows prompt includes `tc_protocol` and tool metadata.
- `IOLOG dir=out` shows gateway error content:
  - `脚本节点执行失败 ... IndentationError ...`

### 5) Phase 2 Layer定位

- HTTP handler is receiving tools + tool_choice and injecting protocol correctly.
- Gateway/upstream returns an **error payload** (IndentationError in upstream script node).
- Proxy still returns 200 with empty content, so tool_calls never produced.

## Phase 2 Output

- 空响应是**服务端真实响应为空**（非脚本解析问题）。
- 初步根因假设（仅假设，未修复）：
  1) 上游网关脚本节点报 `IndentationError` 导致无有效输出。
  2) 代理对上游错误未映射为显式失败（200 + 空内容），导致“无反馈直接结束”。

---

## Phase 3: Minimal Verification

### Experiment 1: Extract IndentationError evidence

Command:
```powershell
Get-Content -Tail 400 proxy_8022.err | Select-String -Pattern "IndentationError|Traceback|File"
```

Key lines (de-identified, no file path present):
- `IndentationError: expected an indented block after function definition on line 1`
- `Line 9: ... at statement: "match = re.search(r'model\\s*=\\s*(\\S+)', params.input)"`

Result: No file path/line number from the upstream script is included in the error payload, only a line index (Line 9) and statement text. File location cannot be determined from proxy logs alone.

### Experiment 2: No-tools request

Command:
```powershell
Invoke-WebRequest -Method Post -Uri http://127.0.0.1:8022/v1/chat/completions `
  -ContentType "application/json" -Body "{\"model\":\"agent\",\"messages\":[{\"role\":\"user\",\"content\":\"Say hi.\"}]}" `
  -UseBasicParsing
```

Result: `200` with `message.content=""` (still empty).

Additional IOLOG evidence:
- Even without tools, `proxy_8022.err` shows the same upstream `IndentationError` error payload.

### Phase 3 Output

- IndentationError source: **upstream script node**, error payload lacks file path; only `Line 9` and statement text are visible.
- No-tools request still returns empty content, and logs still show the same upstream error.  
  → Issue is **not tool-only**; upstream script node failure affects all requests.

---

## Phase 4: Error Surface Validation (post-fix behavior)

### Minimal no-tools request

Command:
```powershell
Invoke-WebRequest -Method Post -Uri http://127.0.0.1:8022/v1/chat/completions `
  -ContentType "application/json" -Body "{\"model\":\"agent\",\"messages\":[{\"role\":\"user\",\"content\":\"Say hi.\"}]}" `
  -UseBasicParsing
```

Result:
- Status: **502**
- Body (summary): `{"error":{"type":"upstream_error","code":"upstream_run_failed","message":"...IndentationError..."}}`

### Smoke script

Command:
```powershell
powershell -File scripts/codex_toolcall_smoke.ps1
```

Result:
- `/v1/chat/completions` now returns **502** with OpenAI-style error JSON (no longer empty 200).
- Error content includes upstream IndentationError message.
