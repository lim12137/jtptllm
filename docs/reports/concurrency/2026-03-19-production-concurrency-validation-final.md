# 2026-03-19 Production Concurrency Validation Final

## Scope
- Goal: validate total concurrency across different session keys
- Endpoint: `http://127.0.0.1:8022/v1/chat/completions`
- Model: `qingyuan`
- Success rule: valid response only; `empty_200` counts as failure
- Pass threshold: success rate >= 99% and p95 <= 3000ms

## Commands
```powershell
Invoke-WebRequest -UseBasicParsing -Uri 'http://127.0.0.1:8022/health'
& .\scripts\bench_concurrency_multi_key.ps1 -Uri 'http://127.0.0.1:8022/v1/chat/completions' -Model 'qingyuan' -ConcurrencyList 1,2,4,6,8,10,12,16,20 -PerLevelTotal 60 -SteadyTotal 0 -P95BudgetMs 3000
& .\scripts\bench_concurrency_multi_key.ps1 -Uri 'http://127.0.0.1:8022/v1/chat/completions' -Model 'qingyuan' -ConcurrencyList 12,16,20 -PerLevelTotal 0 -CandidateLevels 12,16,20 -SteadyTotal 300 -P95BudgetMs 3000
```

## Staircase Results
| Concurrency | Total | Success | PROCESS_CONCURRENCY_LOCK | 502 | empty_200 | timeout | other | Avg ms | p95 ms | Pass |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | :---: |
| 1 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2423 | 2525 | yes |
| 2 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2425 | 2565 | yes |
| 4 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2425 | 2569 | yes |
| 6 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2392 | 2461 | yes |
| 8 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2412 | 2497 | yes |
| 10 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2390 | 2443 | yes |
| 12 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2415 | 2558 | yes |
| 16 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2463 | 2552 | yes |
| 20 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2436 | 2584 | yes |

## Steady Results
| Concurrency | Total | Success | Success Rate | PROCESS_CONCURRENCY_LOCK | 502 | empty_200 | timeout | other | Avg ms | p95 ms | Pass |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | :---: |
| 12 | 300 | 300 | 100.00% | 0 | 0 | 0 | 0 | 0 | 2427 | 2646 | yes |
| 16 | 300 | 300 | 100.00% | 0 | 0 | 0 | 0 | 0 | 2454 | 2778 | yes |
| 20 | 300 | 300 | 100.00% | 0 | 0 | 0 | 0 | 0 | 2365 | 2447 | yes |

## Key Findings
- Major failure types observed: none
- Highest fully passing level in this run: `20`
- Recommended production golden concurrency: `16`

## Rationale
- Staircase levels `1-20` all passed with 100% success and no failures in any category.
- Steady candidate levels `12`, `16`, and `20` each achieved `300/300` success and `p95 <= 3000ms`.
- `20` is the highest validated passing level in this run.
- `16` is recommended as the production golden concurrency to preserve headroom below the currently validated upper bound.

## Paths
- Final raw JSON: `docs\reports\concurrency\raw\2026-03-19-production-concurrency-validation-final.json`
- Final Markdown: `docs\reports\concurrency\2026-03-19-production-concurrency-validation-final.md`
- Staircase source JSON: `docs\reports\concurrency\raw\2026-03-19_093209-multi-key-benchmark.json`
- Steady source JSON: `docs\reports\concurrency\raw\2026-03-19_094215-multi-key-benchmark.json`
