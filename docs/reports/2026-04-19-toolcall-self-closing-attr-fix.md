# 2026-04-19 tool_call 自闭合标签属性解析修复报告

## 背景
- 复现输入：`<tool_call><Glob pattern="*" path="d:/1work/api调用" /></tool_call>`
- 现象：被当作普通文本返回，未转换为 tool call。
- 原因：`parseToolCallTaggedBlock` 仅支持成对标签（`<Read>...</Read>`）参数，不支持自闭合标签属性参数。

## 本次修改
- 修改文件：`internal/openai/compat.go`
  - 在 `parseToolCallTaggedBlock` 中抽出统一解析入口 `parseTaggedToolCall`。
  - 新增 `parseSelfClosingTaggedToolCall`，支持解析 `<Tool attr="value" />`：
    - 工具名取标签名；
    - 属性键值对转为 `arguments` JSON；
    - 无属性时回退为 `{}`。
  - 保留既有成对标签路径，继续支持：
    - `<Read>{"file_path":"go.mod"}</Read>`（JSON 参数）
    - `<Read><file_path>go.mod</file_path></Read>`（XML 参数）

- 修改文件：`internal/openai/compat_test.go`
  - 新增 `TestParseToolSentinelTagWrappedToolCallCompatibility`，覆盖：
    - 自闭合标签属性参数（`Glob`）
    - 成对 XML 标签参数不回归（`Read/file_path`）
    - 普通文本不误判

## 测试执行
- 命令：
  - `$env:GOCACHE='D:\1work\api调用\.gocache'; go test ./internal/openai -v`
- 结果摘要：
  - `internal/openai` 包全部测试通过（含新增兼容测试子用例）。

## 风险与后续建议
- 当前仅支持标准 XML 属性写法（带引号）；非标准写法不会识别为 tool call，会按普通文本回退。
- 本次未执行 `go test ./... -v`，全仓回归覆盖仍依赖后续统一 CI/本地全量测试。
