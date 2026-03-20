# Production Concurrency Validation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 建立一套针对“不同 session key 下总并发能力”的生产可用验证流程，通过阶梯压测筛选候选上限，再通过稳态压测得出成功率 `>=99%` 且 `p95` 延迟受控的保守生产黄金并发值。

**Architecture:** 在现有并发压测资产基础上，新增面向“唯一 session key 并发”的运行态验证脚本与统一报告模板。脚本输出原始 JSON 结果，报告按统一分类统计 `PROCESS_CONCURRENCY_LOCK`、`502`、`empty_200`、超时与其他错误，并将所有结果落盘到 `docs/reports/concurrency/` 及其 `raw/` 子目录。

**Tech Stack:** PowerShell、HTTP `/v1/chat/completions`、Go 代理运行实例、Markdown 报告、JSON 结果文件。

---

### Task 1: 确认现有资产与口径基线

**Files:**
- Inspect: `scripts/bench_concurrency.ps1`
- Inspect: `docs/reports/2026-03-18-concurrency-context-report.md`
- Inspect: `docs/reports/2026-03-18-multisession-version-isolation-test.md`
- Inspect: `docs/reports/2026-03-19-concurrency-validation-from-git.md`
- Inspect: `internal/http/handlers.go:335`

**Step 1: 读取现有并发脚本**

```powershell
Get-Content scripts/bench_concurrency.ps1
```

**Step 2: 读取现有并发报告**

```powershell
Get-Content docs/reports/2026-03-18-concurrency-context-report.md
Get-Content docs/reports/2026-03-18-multisession-version-isolation-test.md
Get-Content docs/reports/2026-03-19-concurrency-validation-from-git.md
```

**Step 3: 读取当前会话并发相关请求头实现**

```powershell
Get-Content internal/http/handlers.go
```

**Step 4: 记录基线结论**

- 现有脚本默认使用固定 `x-client-id`，不适合验证“不同 session key 下总并发能力”
- 当前会话 key 受 `x-agent-session`、`x-client-id`、客户端 IP 影响
- 成功定义必须升级为“有效返回”，不能仅看 HTTP `200`

---

### Task 2: 先写失败测试与脚本设计（TDD）

**Files:**
- Create: `scripts/bench_concurrency_multi_key.ps1`
- Create: `docs/reports/concurrency/.gitkeep`
- Create: `docs/reports/concurrency/raw/.gitkeep`
- Create: `docs/reports/concurrency/README.md`

**Step 1: 先写脚本行为说明，明确失败判定**

在 `docs/reports/concurrency/README.md` 中先定义：

- 请求目标：`/v1/chat/completions`
- 每个并发请求必须使用唯一 session key
- 成功定义：HTTP `200` + 有效非空文本
- 失败分类：
  - `PROCESS_CONCURRENCY_LOCK`
  - `502`
  - `empty_200`
  - `timeout`
  - `other`

**Step 2: 为脚本写最小可验证行为清单**

脚本必须支持：

- 自定义目标地址
- 自定义模型
- 自定义并发档位列表
- 自定义每档总请求数
- 自定义稳态档位与总请求数
- 原始 JSON 落盘到 `docs/reports/concurrency/raw/`
- Markdown 汇总落盘到 `docs/reports/concurrency/`

**Step 3: 先运行占位检查，确认文件尚不存在**

```powershell
Test-Path scripts/bench_concurrency_multi_key.ps1
Test-Path docs/reports/concurrency/README.md
```

**Expected:** 初始为 `False`。

---

### Task 3: 实现阶梯压测脚本（唯一 session key）

**Files:**
- Create: `scripts/bench_concurrency_multi_key.ps1`
- Target: `docs/reports/concurrency/raw/`

**Step 1: 实现请求模型**

脚本每次请求应至少生成以下请求头之一：

```powershell
@{
  "Content-Type" = "application/json"
  "x-client-id" = "bench-<runId>-<requestId>"
}
```

要求：每次请求的 session key 唯一，避免同 key 锁竞争干扰结论。

**Step 2: 实现结果分类函数**

分类逻辑至少输出：

- `success`
- `PROCESS_CONCURRENCY_LOCK`
- `502`
- `empty_200`
- `timeout`
- `other`

**Step 3: 实现阶梯压测模式**

建议默认档位：

```powershell
@(1,2,4,6,8,10,12,16,20)
```

每档至少输出：

- `concurrency`
- `total`
- `success`
- `success_rate`
- `avg_ms`
- `p95_ms`
- 各失败分类计数

**Step 4: 运行脚本帮助或最小 dry-run**

```powershell
powershell -File scripts/bench_concurrency_multi_key.ps1 -WhatIf
```

**Expected:** 能展示参数或执行计划；若未实现 `-WhatIf`，至少保证最小档位可跑通。

**Step 5: 提交脚本基础骨架**

```bash
git add scripts/bench_concurrency_multi_key.ps1 docs/reports/concurrency/.gitkeep docs/reports/concurrency/raw/.gitkeep docs/reports/concurrency/README.md
git commit -m "feat: add multi-key concurrency benchmark skeleton"
```

---

### Task 4: 先写汇总报告规则，再实现稳态压测

**Files:**
- Modify: `scripts/bench_concurrency_multi_key.ps1`
- Create: `docs/reports/concurrency/2026-03-19-production-concurrency-validation-template.md`

**Step 1: 先写报告模板**

模板必须包含：

- 测试时间
- 目标地址
- 模型
- 请求模式（非 stream、非 toolcall）
- session key 策略（唯一 key）
- 阶梯压测结果表
- 稳态压测结果表
- 成功定义
- 失败分类定义
- 结论：
  - 最大可通过档位
  - 推荐生产黄金档位
  - 风险说明

**Step 2: 在脚本中加入稳态压测模式**

建议参数：

```powershell
[int[]]$CandidateLevels = @(8,10,12)
[int]$SteadyTotal = 300
```

每个候选档位都要输出：

- `success_rate`
- `p95_ms`
- `PROCESS_CONCURRENCY_LOCK`
- `502`
- `empty_200`
- `timeout`
- `other`

**Step 3: 实现“黄金值”判定逻辑**

最小判定规则：

- 成功率 `>= 99%`
- `p95_ms` 不超过脚本参数中的上限，例如 `-P95BudgetMs 3000`
- 若多个档位满足，取更高档位
- 若边界波动明显，下调一档作为保守值

**Step 4: 保存原始结果与汇总报告**

建议输出文件：

- 原始 JSON：`docs/reports/concurrency/raw/<timestamp>-production-concurrency-validation.json`
- 汇总 Markdown：`docs/reports/concurrency/<timestamp>-production-concurrency-validation.md`

**Step 5: 提交稳态压测与报告模板**

```bash
git add scripts/bench_concurrency_multi_key.ps1 docs/reports/concurrency/2026-03-19-production-concurrency-validation-template.md
git commit -m "feat: add steady-state production concurrency evaluation"
```

---

### Task 5: 执行阶段 1 阶梯压测

**Files:**
- Run: `scripts/bench_concurrency_multi_key.ps1`
- Output: `docs/reports/concurrency/raw/<timestamp>-production-concurrency-validation.json`
- Output: `docs/reports/concurrency/<timestamp>-production-concurrency-validation.md`

**Step 1: 先确认代理健康**

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8022/health | Select-Object -ExpandProperty Content
```

**Expected:** `{"ok":true}`

**Step 2: 执行阶梯压测**

```powershell
powershell -File scripts/bench_concurrency_multi_key.ps1 `
  -Uri http://127.0.0.1:8022/v1/chat/completions `
  -Model agent `
  -ConcurrencyList 1,2,4,6,8,10,12,16,20 `
  -PerLevelTotal 60 `
  -CandidateLevels 8,10,12 `
  -SteadyTotal 0
```

**Expected:** 生成每档结果表，并筛出 1-3 个候选档位。

**Step 3: 检查候选档位**

- 若成功率在某档后明显跌破 `99%`，记录其前一档为优先候选
- 若 `p95` 已明显失控，标记该档为风险档位

---

### Task 6: 执行阶段 2 稳态压测

**Files:**
- Run: `scripts/bench_concurrency_multi_key.ps1`
- Output: `docs/reports/concurrency/raw/<timestamp>-production-concurrency-validation.json`
- Output: `docs/reports/concurrency/<timestamp>-production-concurrency-validation.md`

**Step 1: 对候选档位做稳态压测**

```powershell
powershell -File scripts/bench_concurrency_multi_key.ps1 `
  -Uri http://127.0.0.1:8022/v1/chat/completions `
  -Model agent `
  -ConcurrencyList 1,2,4,6,8,10,12,16,20 `
  -PerLevelTotal 60 `
  -CandidateLevels 8,10,12 `
  -SteadyTotal 300 `
  -P95BudgetMs 3000
```

**Expected:** 每个候选档位都有稳态统计结果。

**Step 2: 用统一规则判定黄金值**

必须同时满足：

- `success_rate >= 99%`
- `p95_ms <= P95BudgetMs`
- `empty_200` 不被计入成功
- 失败分类已完整统计

**Step 3: 若无档位满足**

- 结论必须明确写为“当前时间窗口下未找到满足生产保守口径的档位”
- 同时给出最接近档位与主要失败类型

---

### Task 7: 固化最终报告与结论

**Files:**
- Create: `docs/reports/concurrency/<timestamp>-production-concurrency-validation.md`
- Create: `docs/reports/concurrency/raw/<timestamp>-production-concurrency-validation.json`

**Step 1: 补齐报告摘要**

报告必须明确：

- 最大可过档位
- 推荐生产黄金档位
- 主要失败原因排序
- 是否受上游 `PROCESS_CONCURRENCY_LOCK` 或 `502` 主导
- 测试窗口与环境说明

**Step 2: 复核分类与成功定义**

确认：

- `empty_200` 未被算入成功
- `PROCESS_CONCURRENCY_LOCK` 与 `502` 单独统计
- 超时未混入其他类

**Step 3: 检查工作区状态**

```bash
git status --short
```

**Expected:** 仅新增本任务相关脚本与报告文件；若存在无关脏文件，不回退、不覆盖。

**Step 4: 建议最终提交信息**

```bash
git commit -m "docs: add production concurrency validation report"
```

---

## 阶段划分摘要

- 阶段 1：阶梯压测
  - 目标：找候选上限
  - 输出：候选档位、拐点、失败分布
- 阶段 2：稳态压测
  - 目标：确认保守生产黄金值
  - 输出：成功率、`p95`、失败分类、最终推荐值

## 产出物

- 设计文档：`docs/plans/2026-03-19-production-concurrency-validation-design.md`
- 实施计划：`docs/plans/2026-03-19-production-concurrency-validation.md`
- 脚本：`scripts/bench_concurrency_multi_key.ps1`
- 分类说明：`docs/reports/concurrency/README.md`
- 报告模板：`docs/reports/concurrency/2026-03-19-production-concurrency-validation-template.md`
- 原始结果：`docs/reports/concurrency/raw/<timestamp>-production-concurrency-validation.json`
- 汇总报告：`docs/reports/concurrency/<timestamp>-production-concurrency-validation.md`

## 判定规则摘要

- 成功：HTTP `200` + 有效非空文本返回
- 失败分类至少包括：
  - `PROCESS_CONCURRENCY_LOCK`
  - `502`
  - `empty_200`
  - `timeout`
  - `other`
- 生产黄金并发值：满足 `success_rate >= 99%` 且 `p95` 受控的最大稳态档位

## 建议提交信息

- `feat: add multi-key concurrency benchmark`
- `docs: add production concurrency validation template`
- `docs: add production concurrency validation report`
- 或合并为：`feat: add production concurrency validation workflow`
