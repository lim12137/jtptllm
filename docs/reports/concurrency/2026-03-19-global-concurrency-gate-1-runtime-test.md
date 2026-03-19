# 2026-03-19 Global Concurrency Gate=1 Runtime Test

## Scope
- Goal: validate runtime queueing behavior by temporarily setting the global HTTP gate from `16` to `1`
- Temporary code change: `internal/http/server.go` only
- Test path: `5` concurrent real `/v1/chat/completions` requests
- Expected behavior: requests should serialize / queue instead of failing immediately

## Temporary Change
- File: `internal/http/server.go`
- Constant changed for this runtime-only test:
  - from: `defaultGlobalHTTPLimit = 16`
  - to: `defaultGlobalHTTPLimit = 1`

## Restart / Health
- Restart method: Go debug proxy via `scripts/restart_proxy.ps1` with `go run ./cmd/proxy`
- Health result after restart:

```json
{"ok":true}
```

## Runtime Method
- Sent `5` concurrent real `POST /v1/chat/completions` requests
- Each request used a unique `x-client-id`
- Minimal valid payload:

```json
{"model":"qingyuan","messages":[{"role":"user","content":"请只回复OK"}]}
```

## Results
| Seq | Start | End | Elapsed ms | HTTP | Valid Return |
| ---: | --- | --- | ---: | ---: | :---: |
| 1 | 11:37:17.958 | 11:37:20.317 | 2359 | 200 | yes |
| 2 | 11:37:17.992 | 11:37:22.520 | 4528 | 200 | yes |
| 3 | 11:37:18.312 | 11:37:24.753 | 6441 | 200 | yes |
| 4 | 11:37:18.584 | 11:37:26.941 | 8357 | 200 | yes |
| 5 | 11:37:18.853 | 11:37:29.129 | 10276 | 200 | yes |

## Key Observations
- All `5` requests returned `HTTP 200`
- All `5` requests returned valid assistant content
- No immediate rejection was observed
- No `429`
- No `502`
- No timeout
- Elapsed times formed a clear queue-like staircase:
  - request 1: `2359ms`
  - request 2: `4528ms`
  - request 3: `6441ms`
  - request 4: `8357ms`
  - request 5: `10276ms`
- Total spread from fastest to slowest request: `7917ms`

## Assessment
This is direct runtime evidence that with the global gate forced to `1`, concurrent requests do not fail immediately. Instead, they wait and complete one after another, which matches the intended queueing behavior of the global HTTP concurrency gate.

## Cleanup
- After recording this result, the temporary constant change in `internal/http/server.go` must be restored from `1` back to `16`
- Proxy should then be restarted again to return the workspace to normal runtime settings
