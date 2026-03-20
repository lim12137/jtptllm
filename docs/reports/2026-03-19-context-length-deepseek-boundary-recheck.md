# deepseek 上下文边界复核（新判定标准）

## 基本信息

- 日期：2026-03-19
- 代理：`http://127.0.0.1:8022`
- endpoint：`/v1/chat/completions`
- 口径：`model=deepseek`、`stream=true`、带 `tools`、`tool_choice=auto`、多轮 `messages` 最小工具闭环

## 新判定标准（必须明确）

- **成功**：SSE 中出现有效 assistant `content` 或有效 `tool_calls`。
- **失败**：`empty_200` 视为失败（HTTP 200 但无有效 `content/tool_calls`，如仅出现空 `delta` + `finish_reason:"stop"`）。

## 复核档位与结果

- `160k`：两次均 HTTP `200`，SSE 有效 `content`。
  - 耗时约 `4.7s`、`2.2s`
- `200k`：两次均 HTTP `200`，SSE 有效 `content`。
  - 耗时约 `3.1s`、`2.3s`

## 结论

按新标准，160k 和 200k 当前都通过；1M 因 `empty_200` 失败，因此不能外推更高上限。
