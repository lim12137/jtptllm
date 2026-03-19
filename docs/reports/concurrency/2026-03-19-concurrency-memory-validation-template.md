# 2026-03-19 Concurrency Memory Validation Template

## Scope
- Goal: collect memory metrics while running concurrency validation at `8`, `16`, and `20`.
- Primary conclusion target: `proxy.exe`
- Secondary reference target: `go.exe`
- Metrics: `WorkingSet64`, `PrivateMemorySize64`, `PeakWorkingSet64`

## Commands
```powershell
powershell -File scripts/collect_concurrency_memory.ps1 -WhatIf
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/collect_concurrency_memory.ps1 -ConcurrencyLevels 1 -SteadyTotal 1 -SampleIntervalMs 200
powershell -File scripts/collect_concurrency_memory.ps1 -ConcurrencyLevels 8,16,20 -SteadyTotal 300 -P95BudgetMs 3000
```

## Raw Data
- Memory summary JSON:
- Per-level sample JSON:
- Per-level benchmark stdout/stderr:

## Summary Table
| Concurrency | Sample Count | Benchmark Exit | Proxy Max WorkingSet64 | Proxy Max PrivateMemorySize64 | Proxy Max PeakWorkingSet64 | Go Max WorkingSet64 | Go Max PrivateMemorySize64 | Go Max PeakWorkingSet64 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 8 |  |  |  |  |  |  |  |  |
| 16 |  |  |  |  |  |  |  |  |
| 20 |  |  |  |  |  |  |  |  |

## Conclusion
- Primary observation (`proxy.exe`):
- Secondary observation (`go.exe`):
- Growth trend from `8 -> 16 -> 20`:
- Bench summary linkage:
- Follow-up action:
