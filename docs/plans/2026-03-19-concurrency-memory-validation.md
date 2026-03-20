# Concurrency Memory Validation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 建立一套针对 `8/16/20` 并发下 `proxy.exe`/`go.exe` 内存联动验证的执行流程，复用现有并发压测脚本，输出同时包含成功率、`p95` 与内存峰值的生产可读报告，并以 `proxy.exe` 作为主结论依据。

**Architecture:** 复用 `scripts/bench_concurrency_multi_key.ps1` 作为请求驱动，不改其核心请求逻辑；新增独立内存采样脚本按档位监控 `proxy.exe` 与 `go.exe` 的 `WorkingSet64`、`PrivateMemorySize64`、`PeakWorkingSet64`，最后将请求结果 JSON/Markdown 与内存采样结果汇总到统一报告中。结论按“`proxy.exe` 主指标 + `go.exe` 辅助指标 + 请求结果联动”给出，避免简单相加得出误导性总数。

**Tech Stack:** PowerShell、Windows `Get-Process`、HTTP `/v1/chat/completions`、现有并发压测脚本、Markdown/JSON 报告。

---

### Task 1: 确认现有压测与进程识别基线

**Files:**
- Inspect: `scripts/bench_concurrency_multi_key.ps1`
- Inspect: `docs/reports/concurrency/README.md`
- Inspect: `docs/reports/concurrency/2026-03-19-production-concurrency-validation-template.md`
- Inspect: `docs/plans/2026-03-19-concurrency-memory-validation-design.md`

**Step 1: 读取现有并发压测脚本**

```powershell
Get-Content scripts/bench_concurrency_multi_key.ps1
```

**Step 2: 确认其可直接驱动 `8/16/20` 并发**

```powershell
powershell -File scripts/bench_concurrency_multi_key.ps1 -WhatIf -ConcurrencyList 8,16,20 -CandidateLevels 8,16,20
```

**Expected:** 输出计划执行参数，不真正发请求。

**Step 3: 确认当前目标进程可识别**

```powershell
Get-Process go,proxy -ErrorAction SilentlyContinue | Select-Object ProcessName,Id,Path,StartTime,WorkingSet64,PrivateMemorySize64,PeakWorkingSet64
```

**Expected:** 能区分 `go.exe` 与 `proxy.exe`，并记录 PID 与路径。

---

### Task 2: 先写内存采样脚本与报告结构（TDD）

**Files:**
- Create: `scripts/collect_concurrency_memory.ps1`
- Create: `docs/reports/concurrency/2026-03-19-concurrency-memory-validation-template.md`
- Create: `docs/reports/concurrency/raw/.gitkeep`

**Step 1: 先定义采样脚本最小参数**

脚本至少支持：

- `-ProcessNames proxy,go`
- `-SampleIntervalMs`
- `-DurationSeconds`
- `-OutputJson`
- `-OutputCsv`（可选）

**Step 2: 先写报告模板字段**

模板必须预留：

- `proxy.exe` PID、Path
- `go.exe` PID、Path
- 并发档位 `8/16/20`
- 请求结果：`success_rate`、`p95_ms`、各失败分类
- 内存指标：
  - `WorkingSet64`
  - `PrivateMemorySize64`
  - `PeakWorkingSet64`
- baseline / peak / after / delta

**Step 3: 占位检查文件不存在**

```powershell
Test-Path scripts/collect_concurrency_memory.ps1
Test-Path docs/reports/concurrency/2026-03-19-concurrency-memory-validation-template.md
```

**Expected:** 初始为 `False`。

---

### Task 3: 实现内存采样脚本

**Files:**
- Create: `scripts/collect_concurrency_memory.ps1`
- Output: `docs/reports/concurrency/raw/<timestamp>-concurrency-memory-samples.json`

**Step 1: 采集目标进程标识**

脚本必须记录：

- `ProcessName`
- `Id`
- `Path`
- `StartTime`

并且分别记录 `proxy.exe` 与 `go.exe`，禁止合并为一个总进程。

**Step 2: 采集最小内存指标**

每次采样至少记录：

- `WorkingSet64`
- `PrivateMemorySize64`
- `PeakWorkingSet64`

建议同时记录：

- `VirtualMemorySize64`
- 时间戳
- 样本序号

**Step 3: 支持按时间窗口循环采样**

示例逻辑：

```powershell
for (...) {
  Get-Process proxy,go | Select-Object ProcessName,Id,Path,WorkingSet64,PrivateMemorySize64,PeakWorkingSet64
  Start-Sleep -Milliseconds 500
}
```

**Step 4: 输出原始结果**

建议输出：

- JSON：`docs/reports/concurrency/raw/<timestamp>-concurrency-memory-samples.json`
- CSV（可选）：`docs/reports/concurrency/raw/<timestamp>-concurrency-memory-samples.csv`

**Step 5: 提交采样脚本**

```bash
git add scripts/collect_concurrency_memory.ps1 docs/reports/concurrency/2026-03-19-concurrency-memory-validation-template.md
git commit -m "feat: add concurrency memory sampling script"
```

---

### Task 4: 将压测驱动与内存采样联动

**Files:**
- Run: `scripts/bench_concurrency_multi_key.ps1`
- Run: `scripts/collect_concurrency_memory.ps1`
- Output: `docs/reports/concurrency/raw/<timestamp>-multi-key-benchmark.json`
- Output: `docs/reports/concurrency/raw/<timestamp>-concurrency-memory-samples.json`

**Step 1: 先确认代理健康**

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8022/health | Select-Object -ExpandProperty Content
```

**Expected:** `{"ok":true}`

**Step 2: 对单个档位先做最小联调**

```powershell
powershell -File scripts/collect_concurrency_memory.ps1 -ProcessNames proxy,go -SampleIntervalMs 500 -DurationSeconds 30 -OutputJson docs/reports/concurrency/raw/test-memory.json
powershell -File scripts/bench_concurrency_multi_key.ps1 -Uri http://127.0.0.1:8022/v1/chat/completions -Model qingyuan -ConcurrencyList 8 -PerLevelTotal 30 -CandidateLevels 8 -SteadyTotal 30 -P95BudgetMs 5000
```

**Expected:** 能同时得到请求结果与内存样本文件。

**Step 3: 明确联动策略**

每个并发档位分别执行一轮：

- 档位 `8`
- 档位 `16`
- 档位 `20`

每轮都要保留：

- 压测结果文件
- 内存采样文件
- 进程 PID/路径快照

---

### Task 5: 执行 `8/16/20` 并发联动测试

**Files:**
- Run: `scripts/bench_concurrency_multi_key.ps1`
- Run: `scripts/collect_concurrency_memory.ps1`
- Output: `docs/reports/concurrency/raw/<timestamp>-*.json`

**Step 1: 执行 8 并发**

```powershell
powershell -File scripts/collect_concurrency_memory.ps1 -ProcessNames proxy,go -SampleIntervalMs 500 -DurationSeconds 60 -OutputJson docs/reports/concurrency/raw/<timestamp>-c8-memory.json
powershell -File scripts/bench_concurrency_multi_key.ps1 -Uri http://127.0.0.1:8022/v1/chat/completions -Model qingyuan -ConcurrencyList 8 -PerLevelTotal 60 -CandidateLevels 8 -SteadyTotal 120 -P95BudgetMs 5000
```

**Step 2: 执行 16 并发**

```powershell
powershell -File scripts/collect_concurrency_memory.ps1 -ProcessNames proxy,go -SampleIntervalMs 500 -DurationSeconds 60 -OutputJson docs/reports/concurrency/raw/<timestamp>-c16-memory.json
powershell -File scripts/bench_concurrency_multi_key.ps1 -Uri http://127.0.0.1:8022/v1/chat/completions -Model qingyuan -ConcurrencyList 16 -PerLevelTotal 60 -CandidateLevels 16 -SteadyTotal 120 -P95BudgetMs 5000
```

**Step 3: 执行 20 并发**

```powershell
powershell -File scripts/collect_concurrency_memory.ps1 -ProcessNames proxy,go -SampleIntervalMs 500 -DurationSeconds 60 -OutputJson docs/reports/concurrency/raw/<timestamp>-c20-memory.json
powershell -File scripts/bench_concurrency_multi_key.ps1 -Uri http://127.0.0.1:8022/v1/chat/completions -Model qingyuan -ConcurrencyList 20 -PerLevelTotal 60 -CandidateLevels 20 -SteadyTotal 120 -P95BudgetMs 5000
```

**Step 4: 每轮后立即记录进程快照**

```powershell
Get-Process go,proxy -ErrorAction SilentlyContinue | Select-Object ProcessName,Id,Path,WorkingSet64,PrivateMemorySize64,PeakWorkingSet64 | ConvertTo-Json -Depth 4
```

**Expected:** 每档都有对应请求结果、内存样本和进程快照。

---

### Task 6: 生成生产可读内存报告

**Files:**
- Create: `docs/reports/concurrency/<timestamp>-concurrency-memory-validation.md`
- Create: `docs/reports/concurrency/raw/<timestamp>-concurrency-memory-summary.json`
- Reference: `docs/reports/concurrency/2026-03-19-concurrency-memory-validation-template.md`

**Step 1: 汇总每档请求结果**

每档至少汇总：

- `success`
- `PROCESS_CONCURRENCY_LOCK`
- `502`
- `empty_200`
- `timeout`
- `other`
- `success_rate`
- `p95_ms`

**Step 2: 汇总每档内存结果**

对 `proxy.exe` 和 `go.exe` 分别汇总：

- baseline `WorkingSet64`
- peak `WorkingSet64`
- after `WorkingSet64`
- baseline `PrivateMemorySize64`
- peak `PrivateMemorySize64`
- after `PrivateMemorySize64`
- `PeakWorkingSet64`
- 相对 baseline 的增量

**Step 3: 报告中明确主次口径**

报告必须明确写出：

- 主结论看 `proxy.exe`
- `go.exe` 作为辅助观测
- 不将两者简单相加为唯一结论

**Step 4: 按联合口径给出结论**

至少回答：

- `8/16/20` 中哪个档位 `proxy.exe` 峰值增长最明显
- `16` 并发是否仍兼顾成功率、`p95` 与内存峰值
- 推荐生产保守档位是什么

---

### Task 7: 判定与收尾

**Files:**
- Verify: `docs/reports/concurrency/<timestamp>-concurrency-memory-validation.md`
- Verify: `docs/reports/concurrency/raw/<timestamp>-*.json`

**Step 1: 判定规则复核**

一个档位要被视为“可接受”，至少满足：

- `success_rate` 达到预设目标
- `p95_ms` 受控
- `empty_200` 不算成功
- `proxy.exe` 峰值未超过预设可接受范围
- `go.exe` 无异常失控增长

**Step 2: 检查工作区状态**

```bash
git status --short
```

**Expected:** 仅新增本任务相关脚本与报告文件；如有无关脏文件，不回退、不覆盖。

**Step 3: 建议最终提交信息**

```bash
git commit -m "docs: add concurrency memory validation report"
```

---

## 阶段划分摘要

- 阶段 1：确认压测与进程识别基线
- 阶段 2：实现内存采样脚本与模板
- 阶段 3：联动执行 `8/16/20` 压测与采样
- 阶段 4：汇总请求结果与内存结果
- 阶段 5：产出生产可读报告与保守结论

## 产出物

- 设计文档：`docs/plans/2026-03-19-concurrency-memory-validation-design.md`
- 实施计划：`docs/plans/2026-03-19-concurrency-memory-validation.md`
- 采样脚本：`scripts/collect_concurrency_memory.ps1`
- 报告模板：`docs/reports/concurrency/2026-03-19-concurrency-memory-validation-template.md`
- 原始内存样本：`docs/reports/concurrency/raw/<timestamp>-concurrency-memory-samples.json`
- 原始压测结果：`docs/reports/concurrency/raw/<timestamp>-multi-key-benchmark.json`
- 汇总报告：`docs/reports/concurrency/<timestamp>-concurrency-memory-validation.md`

## 判定规则摘要

- 主结论：`proxy.exe`
- 辅助观测：`go.exe`
- 核心内存指标至少包括：
  - `WorkingSet64`
  - `PrivateMemorySize64`
  - `PeakWorkingSet64`
- 并发档位：`8`、`16`、`20`
- 结果必须联动评估：
  - `success_rate`
  - `p95_ms`
  - `proxy.exe` 内存峰值
  - `go.exe` 辅助变化

## 建议提交信息

- `feat: add concurrency memory sampling workflow`
- `docs: add concurrency memory validation template`
- `docs: add concurrency memory validation report`
- 或合并为：`feat: add concurrency memory validation workflow`
