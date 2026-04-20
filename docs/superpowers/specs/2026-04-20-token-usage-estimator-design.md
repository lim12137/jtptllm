# Token Usage Fallback Estimator Design

## 背景

当前代理在上游返回 `usage` 时，已经会优先透传；只有上游缺失 `usage` 时，才会回退到本地估算。现有回退实现位于 `internal/openai/compat.go`：

- `ChatUsageFromCharCount(prompt, completion)`
- `ResponsesUsageFromCharCount(input, output)`
- `scaledRuneCountForUsage(s)`

现状算法等价于：

```go
tokens = utf8.RuneCountInString(text) * 2
```

这个规则实现简单，但误差分布过于粗糙：

- 纯英文通常被高估或低估不稳定，取决于单词长度和空白比例。
- 中文、英文、数字、标点混合文本会出现系统性偏差。
- JSON、代码、XML、tool schema 这类结构化文本与自然语言的 token 密度不同，但当前被一视同仁。
- chat prompt 实际包含 role 前缀、消息边界、工具定义前缀等包装开销，当前算法没有单独建模。

目标是在不引入外部 tokenizer 服务、不增加重依赖、保持纯 Go 的前提下，把 fallback 估算从“统一倍率”升级为“混合启发式估算”，尽量把样本误差压到 `±10%` 范围。

## 已确认约束

- 保持现有优先级不变：上游 `usage` 透传优先，fallback 次之。
- 不引入外部 tokenizer 服务。
- 不引入重依赖，保持纯 Go、低依赖实现。
- chat 与 responses 共用同一个底层估算器。
- chat 额外考虑消息包装开销。
- 第一版必须覆盖以下样本类型：
  - 纯中文
  - 纯英文
  - 中英混合
  - JSON
  - 代码
  - XML / tool schema
  - 多轮 chat prompt

## 当前调用链

当前 fallback 的调用点已经统一，设计只需要替换底层估算逻辑，不需要改变调用优先级：

- `internal/http/handlers.go`
  - `handleChatCompletions()` 非流式缺失 usage 时调用 `openai.ChatUsageFromCharCount`
  - `handleChatCompletions()` 流式缺失 usage 时调用 `openai.ChatUsageFromCharCount`
  - `handleResponses()` 非流式缺失 usage 时调用 `openai.ResponsesUsageFromCharCount`
  - `streamResponses()` 缺失 usage 时调用 `openai.ResponsesUsageFromCharCount`
- `internal/openai/compat.go`
  - `NormalizeChatUsage()` / `NormalizeResponsesUsage()` 负责上游 usage 归一化
  - `ChatUsageFromCharCount()` / `ResponsesUsageFromCharCount()` 负责 fallback

因此，本次设计的核心是：保留 `Normalize*Usage()` 和 handler 调用顺序不变，只替换 `compat.go` 中的 fallback 估算实现。

## 设计目标

### 主目标

- 在 fallback 场景下，比当前 `runeCount * 2` 明显更接近真实 tokenizer 行为。
- 第一版针对定义样本集，目标误差尽量压到 `±10%` 范围。
- 算法可解释、可调参、可测试，不依赖黑盒外部服务。

### 非目标

- 不追求与 OpenAI 官方 tokenizer 完全一致。
- 不为每个上游模型实现独立 tokenizer。
- 不修改上游 usage 透传逻辑。
- 不在本次设计中引入持久化校准数据或在线学习机制。

## 方案选型

### 方案 A：统一倍率线性修正

把 `*2` 改成另一个常数，例如 `*1.3` 或 `*1.6`。

不采用，原因如下：

- 只能修正总体均值，无法修正不同文本类型之间的结构性偏差。
- 对 JSON、代码、英文长单词、中英文混合场景仍然不稳定。
- 很难达到 `±10%` 的目标。

### 方案 B：纯 Go 混合启发式估算

把文本按可观测特征拆解后加权估算，再加上协议包装开销。

采用，原因如下：

- 不需要外部 tokenizer 服务。
- 不需要大体积词典或第三方分词库。
- 可以针对当前项目的真实输入类型做定向优化。
- 便于测试和后续调参。

### 方案 C：内置轻量 BPE 近似词典

理论上精度可能更高，但不采用，原因如下：

- 实现复杂度明显提高。
- 维护成本高。
- 没有官方 tokenizer 数据支撑时，容易变成不完整 tokenizer，收益不稳定。

## 目标方案

采用“共享底层文本估算器 + chat 包装开销修正”的结构。

### 总体结构

新增一个底层估算器，输出单段文本的 token 估算值。`responses` 直接用它计算输入和输出；`chat` 在此基础上额外增加消息包装开销。

建议结构如下：

```text
estimateTextTokens(text) -> int
estimateChatPromptTokens(prompt) -> int
ChatUsageFromCharCount(prompt, completion) -> chat usage
ResponsesUsageFromCharCount(input, output) -> responses usage
```

其中：

- `estimateTextTokens(text)` 负责文本内容本身的估算。
- `estimateChatPromptTokens(prompt)` 负责在 `estimateTextTokens(prompt)` 基础上增加 chat 包装开销。
- `ChatUsageFromCharCount()` 调用 `estimateChatPromptTokens(prompt)` 和 `estimateTextTokens(completion)`。
- `ResponsesUsageFromCharCount()` 对输入输出都直接调用 `estimateTextTokens()`。

## 核心算法设计

### 1. 分类统计

底层估算器不再只看 rune 总数，而是先扫描文本并统计以下特征：

- `cjkCount`
  - CJK 统一表意文字、日文假名、常见全角字符
- `asciiLetterCount`
  - ASCII 英文字母
- `digitCount`
  - 数字
- `whitespaceCount`
  - 空格、制表、换行
- `punctCount`
  - 通用标点与符号
- `jsonSyntaxCount`
  - `{ } [ ] : , "`
- `xmlSyntaxCount`
  - `< > / =`
- `newlineCount`
  - 换行数
- `wordCount`
  - 连续 ASCII 字母/数字/下划线/连字符形成的词段数
- `longWordCount`
  - 长度超过阈值的 ASCII 词段数

这一步是算法稳定性的基础。第一版不依赖分词库，只做线性扫描和轻量状态机统计。

### 2. 基础加权规则

估算器按统计结果计算基础 token 数：

```text
estimatedTokens =
  cjkWeight * cjkCount +
  asciiWordWeight * wordCount +
  asciiCharResidualWeight * residualAsciiChars +
  digitWeight * digitCount +
  punctWeight * punctCount +
  structureWeight * (jsonSyntaxCount + xmlSyntaxCount) +
  newlineWeight * newlineCount +
  shortTextBase
```

第一版不在设计里写死常数值，但常数必须满足以下原则：

- CJK 权重大于单个 ASCII 字母权重。
- 连续英文应优先按词段计价，不按字符线性计价。
- JSON / XML / 代码结构字符必须高于普通空白权重。
- 连续空白不能与普通字符等价计价。
- 超短文本必须有基础开销，避免 `hi`、`ok`、`{}` 被低估。

实现时常数应集中定义在同一处，禁止散落在多个函数里。

### 3. 英文与词段处理

英文 token 与“词段边界”强相关，单纯按字符数误差较大，因此：

- 对连续 ASCII 单词优先按 `wordCount` 计主成本。
- 对特别长的词段追加 residual 成本。
- 驼峰、蛇形、路径、URL、带连字符词串允许产生额外成本。

这样做的目的不是模拟完整 BPE，而是避免：

- 英文长句被严重低估
- 短单词密集文本被系统性高估
- 代码标识符被简单等同于普通英文句子

### 4. 结构化文本修正

JSON、代码、XML、tool schema 的 token 密度通常高于普通自然语言，因此第一版明确加入结构修正：

- `{ } [ ] : , "` 计入 JSON 结构成本
- `< > / =` 计入 XML / tag 结构成本
- 多行缩进与换行单独计权
- 引号包围的键名、属性名、函数名、字段名通过 `wordCount` 与结构计数叠加体现

这部分规则要让如下文本不再被低估：

- tool schema
- JSON arguments
- XML 片段
- Go / JS / shell 代码

### 5. chat 包装开销

chat prompt 不是单纯的自由文本。当前 `ParseChatRequest()` 会把消息格式化为 prompt，内部包含：

- role 前缀
- 消息边界
- 工具 system prefix
- model marker

这些内容已经进入 `parsed.Prompt`，但其 token 开销与纯文本密度不同。为降低误差，`ChatUsageFromCharCount()` 需要在文本估算之外增加一层固定和半固定包装成本：

- 每个消息行的固定开销
- `system:` / `user:` / `assistant:` 角色前缀开销
- 工具 system prefix 存在时的额外开销
- model marker 存在时的额外开销

第一版采用“从 prompt 文本特征中反推包装”的方式，不要求重建原始 messages 数组。原因是当前 fallback 接口签名只有 `prompt` 和 `completion`，不应为了估算重构 handler 调用接口。

### 6. 下限与平滑规则

为避免极短输入或极端符号串被低估，算法必须包含：

- 最小 token 下限
- 短文本基础常数
- 对空字符串保持 `0`
- 对非空字符串保证结果至少为正整数

此外，所有输出必须满足：

- `prompt_tokens >= 0`
- `completion_tokens >= 0`
- `input_tokens >= 0`
- `output_tokens >= 0`
- `total_tokens = input/prompt + output/completion`

## 实现边界

### 允许修改

- `internal/openai/compat.go`
- `internal/openai/compat_test.go`
- `internal/http/handlers_test.go`
- 配套测试报告文档

### 不修改

- `internal/http/handlers.go` 中 usage 优先级
- `NormalizeChatUsage()` / `NormalizeResponsesUsage()` 的透传行为
- 上游 gateway 协议
- session 逻辑

## 测试与校准策略

### 1. 样本集

第一版至少建立以下样本：

- 纯中文短句、长句
- 纯英文短句、长句
- 中英混合问答
- JSON 对象、嵌套数组
- 代码片段
- XML / tool schema
- 多轮 chat prompt

每个样本记录三项数据：

- 输入文本
- 期望 token 基准值
- 估算结果

这里的“期望 token 基准值”必须来自离线整理好的真实 tokenizer 对照样本，而不是拍脑袋填写。设计允许把这些基准值直接固化到测试中，避免运行时依赖外部服务。

### 2. 测试类型

需要覆盖三类测试：

1. 底层估算器测试
   - 针对单段文本，断言估算值落在目标区间或精确命中固化样本值
2. fallback 使用点测试
   - 断言 chat / responses 在上游缺失 usage 时会使用新估算器
3. 透传保护测试
   - 断言上游返回 usage 时，仍然优先透传，不被新估算器覆盖

### 3. 验收标准

第一版验收以样本集为准，而不是泛化到所有未知文本。验收标准为：

- 定义样本集中，大多数样本误差应控制在 `±10%` 范围
- 个别边界样本若超过 `±10%`，必须在报告中解释原因
- 相比当前 `runeCount * 2`，新算法在样本集上的平均误差明显下降
- chat 与 responses 的 fallback 输出字段格式保持兼容
- 所有现有 passthrough 测试继续通过

## 风险与缓解

### 风险 1：启发式规则过拟合样本

如果常数只针对少数样本调优，其他文本可能表现恶化。

缓解：

- 样本集必须覆盖自然语言、结构化文本、混合文本、多轮 prompt。
- 测试报告同时记录旧算法与新算法的对比。

### 风险 2：chat 包装开销估算不稳定

当前 fallback 接口只拿到拼接后的 prompt，无法直接访问消息数组。

缓解：

- 第一版仅根据 prompt 文本中的 role 前缀、marker、tool prefix 特征估算包装开销。
- 若后续误差仍偏大，再评估是否扩展 fallback 接口签名。

### 风险 3：规则过多导致维护困难

缓解：

- 所有权重和阈值集中定义。
- 统计逻辑与加权逻辑分层。
- 每条规则必须有对应样本或测试支撑。

## 结论

本次设计采用“纯 Go 的混合启发式估算”替换当前 `runeCount * 2` fallback。核心原则是：

- 上游 usage 优先透传
- fallback 只在缺失 usage 时生效
- chat 与 responses 共用底层文本估算器
- chat 额外增加包装开销修正
- 第一版用固定样本集校准，目标把主要样本误差尽量压到 `±10%` 范围

这是在当前约束下，复杂度、可维护性和精度之间最合理的折中方案。
