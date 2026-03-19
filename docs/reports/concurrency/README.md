# Concurrency Reports

该目录用于保存“不同 session key 下总并发能力”验证结果。

约定：
- `raw/`：原始 JSON 结果。
- 根目录：Markdown 汇总报告。
- `scripts/bench_concurrency_multi_key.ps1`：执行阶梯压测与候选稳态验证。

默认分类：
- `success`
- `PROCESS_CONCURRENCY_LOCK`
- `502`
- `empty_200`
- `timeout`
- `other`

最小 dry-run：

```powershell
powershell -File scripts/bench_concurrency_multi_key.ps1 -WhatIf
```

最小真实验证：

```powershell
powershell -File scripts/bench_concurrency_multi_key.ps1 -Uri http://127.0.0.1:8022/v1/chat/completions -Model qingyuan -ConcurrencyList 1 -PerLevelTotal 1 -CandidateLevels 1 -SteadyTotal 1 -P95BudgetMs 10000
```

正式压测前建议：
- 确认代理健康。
- 明确上游模型与限流配置。
- 先用小档位验证日志、落盘与分类是否正确，再拉高总量。
