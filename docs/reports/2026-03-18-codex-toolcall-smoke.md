# Codex CLI Toolcall Smoke Report

**Date:** 2026-03-18

## Setup
- Proxy started with `PROXY_LOG_IO=1` using `restart_proxy_8022_exe.bat`.
- Base URL: `http://127.0.0.1:8022`.
- Smoke script: `scripts/codex_toolcall_smoke.ps1`.

## Execution
1. Direct run:
   - Command: `powershell -File scripts/codex_toolcall_smoke.ps1`
   - Result: **502 Bad Gateway** (upstream `/agent/api/run` returned 500).
2. Codex CLI run:
   - Command: `node C:/Users/Administrator/AppData/Roaming/npm/node_modules/@openai/codex/bin/codex.js exec "Run scripts/codex_toolcall_smoke.ps1 and summarize tool-call stability based on proxy_8022.log"`
   - Result: script executed with 200 responses, but `proxy_8022.log` was empty.
3. Codex CLI log review (stderr log):
   - Command: `node C:/Users/Administrator/AppData/Roaming/npm/node_modules/@openai/codex/bin/codex.js exec "Summarize tool-call stability based on IOLOG entries in proxy_8022.err (not proxy_8022.log). Focus on whether tool_calls were produced or only plain text."`
   - Result: Codex concluded **no tool_calls were produced**, only plain text outputs.

## Log Location
- `proxy_8022.log`: empty (stdout).
- `proxy_8022.err`: contains IOLOG entries (stderr) due to `log.Printf` defaulting to stderr.

## Findings (IOLOG)
- Requests contained `tools` and `tool_choice` for `get_weather`.
- Outputs were **pure text** only, no `tool_calls` / `function_call` structures.
- Codex summary: **tool-call did not trigger in these runs**.

## Stability Conclusion
Tool-call **not stable / not triggered** in this smoke run. The proxy forwarded tool definitions, but the upstream response stayed as plain text. Further investigation should focus on:
- Upstream model/tool-calling capability.
- Protocol agreement (sentinel/JSON wrapper not present in outputs).
- Whether tools should be enforced in upstream system.
