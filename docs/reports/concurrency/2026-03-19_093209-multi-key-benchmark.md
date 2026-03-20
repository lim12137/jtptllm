# Multi-Key Concurrency Benchmark

- Generated At: 2026-03-19 09:40:04
- URI: `http://127.0.0.1:8022/v1/chat/completions`
- Model: `qingyuan`
- Raw JSON: `C:\Users\Administrator\Desktop\人工智能项目\低代码智能体\api调用\docs\reports\concurrency\raw\2026-03-19_093209-multi-key-benchmark.json`

## Summary

| phase | concurrency | total | success | PROCESS_CONCURRENCY_LOCK | 502 | empty_200 | timeout | other | p95_ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| staircase | 1 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2525 |
| staircase | 2 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2565 |
| staircase | 4 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2569 |
| staircase | 6 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2461 |
| staircase | 8 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2497 |
| staircase | 10 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2443 |
| staircase | 12 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2558 |
| staircase | 16 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2552 |
| staircase | 20 | 60 | 60 | 0 | 0 | 0 | 0 | 0 | 2584 |
| steady | 4 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | - |
| steady | 8 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | - |
| steady | 12 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | - |

## Recommendation

- Highest steady-state candidate within budget: `12`

## Notes

- Success requires a valid response. HTTP 200 with empty content is classified as `empty_200`.
- Failure buckets include `success`, `PROCESS_CONCURRENCY_LOCK`, `502`, `empty_200`, `timeout`, and `other`.
- Every request uses a unique `x-client-id` to measure total concurrency across different session keys.
