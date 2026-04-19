# Codubuddy CLI Toolcall Validation

- Date: 2026-04-19 20:51:44
- Base URL: "http://127.0.0.1:8022"
- Model: "agent"
- Execution mode: direct OpenAI-compatible HTTP validation for codubuddy integration

## Commands
- Proxy start: `bin\proxy.exe`
- Validation script: `powershell -File scripts/codubuddy_toolcall_validation.ps1`

## Summary
- Total tool calls: 30
- Successful tool calls: 30
- Failed tool calls: 0
- Chat successes: 15
- Responses successes: 15
- Document-intent successes: 18
- Average latency: 3475 ms

## Acceptance
- Passed: at least 30 tool calls succeeded and document-processing intents succeeded in multiple runs.

## Sample Results
- [chat] case="weather-shanghai" success="True" tool="get_weather" finish="tool_calls" raw="docs\reports\concurrency\raw\2026-04-19_204958-01-chat-weather-shanghai.json"
- [chat] case="skill-doc-summary" success="True" tool="summarize_document" finish="tool_calls" raw="docs\reports\concurrency\raw\2026-04-19_204958-02-chat-skill-doc-summary.json"
- [chat] case="plan-doc-summary" success="True" tool="summarize_document" finish="tool_calls" raw="docs\reports\concurrency\raw\2026-04-19_204958-03-chat-plan-doc-summary.json"
- [chat] case="report-doc-facts" success="True" tool="extract_doc_facts" finish="tool_calls" raw="docs\reports\concurrency\raw\2026-04-19_204958-04-chat-report-doc-facts.json"
- [chat] case="weather-paris" success="True" tool="get_weather" finish="tool_calls" raw="docs\reports\concurrency\raw\2026-04-19_204958-05-chat-weather-paris.json"
- [chat] case="weather-shanghai" success="True" tool="get_weather" finish="tool_calls" raw="docs\reports\concurrency\raw\2026-04-19_204958-06-chat-weather-shanghai.json"

## Notes
- Document scenarios use real repository files from `skills/` and `docs/` as prompt context.
- Validation intentionally exercises both `/v1/chat/completions` and `/v1/responses` because codubuddy may support either OpenAI surface.
- Raw summary JSON: "docs\reports\concurrency\raw\2026-04-19_204958-codubuddy-toolcall-summary.json"
