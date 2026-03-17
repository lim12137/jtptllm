# Concurrency & Context Test Report

**Date:** 2026-03-18
**Target:** `http://127.0.0.1:8022/v1/chat/completions`

## Environment Check
- `GET /health`: `200` (`{"ok":true}`)

## Concurrency Test A (same session key)
- Method: 10 parallel requests, no `x-client-id` (falls back to `ip:127.0.0.1` session key).
- Result: `ok=4`, `fail=6`, all failures `502`.
- Median successful latency: ~`260-345ms`.
- Failed latency: ~`1.3-1.6s`.
- Log evidence: multiple `PROCESS_CONCURRENCY_LOCK` entries in `proxy_8022.err`.

## Concurrency Test B (isolated session keys)
- Method: 10 parallel requests, each request has unique `x-client-id`.
- Result: `ok=0`, `fail=10`, all failures `502`.
- Failure latency: ~`1.39-1.53s`.
- Interpretation: removing session-key contention did not recover success rate in this window; upstream instability dominates.

## Context Capability Test (subagent run)
- cid: `ctx-9887bedf`
- token: `TOKEN-781d55b0`
- turn1 (remember): fail, `502`, retries exhausted (5)
- turn2 (recall): fail, `502`, retries exhausted (5)
- turn3 (reset+recall): fail, `502`, retries exhausted (5)
- Conclusion: cannot evaluate context memory because all turns failed at gateway stage.

## Diagnosis
- Proxy itself is alive (`/health` succeeds).
- Main blocker is upstream call path returning intermittent or sustained `502` from proxy perspective.
- In same-session bursts, upstream also reports concurrency lock (`PROCESS_CONCURRENCY_LOCK`), causing empty assistant outputs for some accepted responses.

## Actionable Next Step
1. Add retry/backoff and explicit mapping for upstream lock errors in proxy handlers.
2. Add lightweight load guard by session key to serialize same-session requests before hitting upstream.
3. Re-run tests after upstream stabilization window.
