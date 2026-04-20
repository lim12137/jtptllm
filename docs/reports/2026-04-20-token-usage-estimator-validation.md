# 2026-04-20 Token Usage Estimator Validation

## 摘要

本次验证覆盖 token usage fallback estimator 的 package 级和全量回归。

- estimator 已从旧的 `utf8.RuneCountInString(text) * 2` fallback 切换为纯 Go heuristic 近似估算
- 上游返回 `usage` 时，仍然优先透传上游真实值，未被 fallback 覆盖
- handler 层 fallback 断言已同步到新的 heuristic 估算结果
- package 回归与全量回归均通过

## Estimator 变更摘要

- 新 fallback 不再使用固定 `rune * 2`
- 估算器改为按文本特征分类计权的 heuristic 近似
- `chat` prompt 会在基础文本估算上叠加 role 和工具前缀开销
- `responses` 仍按输入与输出文本分别估算
- 该算法仍是近似值，不等同于真实 tokenizer；真实 `usage` 仍以上游透传为准

## Calibration Fixture 类别

- 英文短文本
- 英文长文本
- 中文短文本
- 中文长文本
- 中英混合文本
- JSON 载荷
- 代码片段
- XML / tool schema
- 多行 chat prompt

## 验证命令

```powershell
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/openai -v
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./internal/http -v
& 'C:\Users\Administrator\.tools\go1.22.12\go\bin\go.exe' test ./... -count=1
```

## 结果摘要

### 1. `go test ./internal/openai -v`

- 结果：`PASS`
- 重点结论：
  - estimator fixture tests 全部通过
  - chat overhead tests 全部通过
  - `ChatUsageFromCharCount` / `ResponsesUsageFromCharCount` 已切到 heuristic estimator
  - openai 兼容层其余既有行为未回归

### 2. `go test ./internal/http -v`

- 结果：`PASS`
- 重点结论：
  - upstream usage passthrough tests 全部通过
  - chat/responses 的非流式与流式 fallback tests 全部通过
  - chat stream usage 修复未回退
  - handler 层仍保持“有上游 usage 就透传，没有才 fallback”的优先级

### 3. `go test ./... -count=1`

- 结果：`PASS`
- 重点结论：
  - 全仓库回归通过
  - 受影响包 `internal/openai`、`internal/http` 通过
  - 其他包未出现由 estimator 变更引入的连带失败

## 兼容性说明

### upstream usage passthrough 优先级仍保留

- 本次变更只优化 fallback estimator
- 当上游返回 `usage` 时，兼容层继续优先使用上游值
- 本地 heuristic 仅在上游 `usage` 缺失时参与计算

### fallback 现在是 heuristic 近似

- fallback 已不再代表“字符数乘固定倍率”
- 当前返回的是面向兼容层的近似 token usage
- 该值用于在缺失上游 `usage` 时提供更合理的 usage 展示
- 若需要账单级精度，仍应以真实上游 usage 或真实 tokenizer 为准

## 结论

Task 4 要求的 package/full regression 已完成，三条回归命令全部通过。验证结果表明：

- heuristic estimator 已稳定接入 fallback 路径
- upstream usage passthrough 优先级未受影响
- handler 与 openai package 的测试基线已同步
- 当前仓库处于可继续进入后续收尾步骤的状态
