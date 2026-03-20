# Multi-Key Concurrency Benchmark

- Generated At: 2026-03-19 10:54:08
- URI: `http://127.0.0.1:8022/v1/chat/completions`
- Model: `qingyuan`
- Raw JSON: `C:\Users\Administrator\Desktop\人工智能项目\低代码智能体\api调用\docs\reports\concurrency\raw\2026-03-19_105103-multi-key-benchmark.json`

## Summary

| phase | concurrency | total | success | PROCESS_CONCURRENCY_LOCK | 502 | empty_200 | timeout | other | p95_ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| staircase | 20 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | - |
| steady | 20 | 300 | 0 | 0 | 0 | 300 | 0 | 0 | - |

## Recommendation

- No steady-state candidate currently meets the success-rate/P95 budget. Review the raw results.

## Notes

- Success requires a valid response. HTTP 200 with empty content is classified as `empty_200`.
- Failure buckets include `success`, `PROCESS_CONCURRENCY_LOCK`, `502`, `empty_200`, `timeout`, and `other`.
- Every request uses a unique `x-client-id` to measure total concurrency across different session keys.
