# 2026-03-19 Production Concurrency Validation Template

## 基本信息
- 日期：2026-03-19
- 目标：验证不同 session key 下的总并发能力
- 入口：`/v1/chat/completions`
- 脚本：`scripts/bench_concurrency_multi_key.ps1`

## 执行参数
- Uri：
- Model：
- ConcurrencyList：
- PerLevelTotal：
- CandidateLevels：
- SteadyTotal：
- P95BudgetMs：

## 成功标准
- 有效返回记为 `success`
- HTTP 200 但空内容记为 `empty_200` 失败
- 分类至少包含：`success`、`PROCESS_CONCURRENCY_LOCK`、`502`、`empty_200`、`timeout`、`other`

## 结果汇总
| 阶段 | 并发 | 总请求 | success | PROCESS_CONCURRENCY_LOCK | 502 | empty_200 | timeout | other | p95_ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| staircase |  |  |  |  |  |  |  |  |  |
| steady |  |  |  |  |  |  |  |  |  |

## 原始结果
- JSON：
- Markdown：

## 结论
- 观察到的最高可用总并发：
- 推荐生产稳态并发：
- 主要失败类型：
- 后续动作：
