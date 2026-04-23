# Responses API 兼容性报告

**测试日期**: 2026-04-23  
**测试版本**: jtptllm proxy with `<tool_name>` parsing support

## 总结

Responses API (`POST /v1/responses`) 的实现状态：

- ✅ **文本生成接口完整支持**
- ✅ **流式 SSE 事件完整支持**
- ✅ **输入侧工具结果 (`function_call_output`) 支持**
- ❌ **输出侧结构化工具调用 (`function_call`) 未打通**
- ⚠️ **`tool_choice: "required"` 未被严格执行**

## 已验证支持的功能

### 1. 基础文本生成（非流式）

```bash
curl -X POST http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": "你好，请自我介绍"
  }'
```

**结果**: ✅ 正常返回

```json
{
  "id": "resp_xxx",
  "object": "response",
  "output": [{
    "type": "message",
    "role": "assistant",
    "content": [{"type": "output_text", "text": "..."}]
  }]
}
```

### 2. 流式文本生成

```bash
curl -X POST http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": "hello",
    "stream": true
  }'
```

**结果**: ✅ 返回标准 SSE 事件

```
event: response.created
data: {"type":"response.created",...}

event: response.in_progress
data: {"type":"response.in_progress",...}

event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"Hello..."}

event: response.completed
data: {"type":"response.completed",...}
```

### 3. 输入侧接受 function_call + function_call_output

```bash
curl -X POST http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": [
      {
        "type": "function_call",
        "call_id": "call_123",
        "name": "add",
        "arguments": "{\"a\":123,\"b\":456}"
      },
      {
        "type": "function_call_output",
        "call_id": "call_123",
        "output": "579"
      },
      {
        "type": "message",
        "role": "user",
        "content": [{"type": "input_text", "text": "计算结果是多少"}]
      }
    ]
  }'
```

**结果**: ✅ 模型能理解工具调用历史并基于结果继续回答

### 4. previous_response_id 字段

```bash
curl -X POST http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": "继续",
    "previous_response_id": "resp_dummy"
  }'
```

**结果**: ⚠️ 接受任意值，无严格校验

## 未实现/失败的功能

### 1. 输出侧结构化工具调用

```bash
curl -X POST http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": "不要心算。必须调用 add 工具计算 123+456，并只返回结果。",
    "tools": [{
      "name": "add",
      "description": "Add two numbers",
      "input_schema": {
        "type": "object",
        "properties": {
          "a": {"type": "number"},
          "b": {"type": "number"}
        },
        "required": ["a", "b"]
      }
    }],
    "tool_choice": "required"
  }'
```

**期望输出**:
```json
{
  "output": [{
    "type": "function_call",
    "call_id": "call_xxx",
    "name": "add",
    "arguments": "{\"a\":123,\"b\":456}"
  }]
}
```

**实际输出**: ❌
```json
{
  "output": [{
    "type": "message",
    "role": "assistant",
    "content": [{
      "type": "output_text",
      "text": "调用加法工具进行计算：add(123, 456) = 579"
    }]
  }]
}
```

或伪 JSON 格式：
```json
{
  "output": [{
    "type": "message",
    "content": [{
      "type": "output_text",
      "text": "{\"name\":\"add\",\"arguments\":{\"a\":123,\"b\":456}}"
    }]
  }]
}
```

### 2. 流式工具调用事件缺失

**期望的 SSE 事件**:
```
event: response.output_item.added
data: {"type":"response.output_item.added","item":{"type":"function_call",...}}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","delta":"{\"a\":123"}

event: response.function_call_arguments.done
data: {"type":"response.function_call_arguments.done","name":"add","arguments":"..."}

event: response.output_item.done
data: {"type":"response.output_item.done","item":{"type":"function_call",...}}
```

**实际的 SSE 事件**: ❌
只返回 `response.output_text.delta`，无 `function_call` 相关事件。

### 3. 仅有 function_call_output 报错

```bash
curl -X POST http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "input": [
      {
        "type": "function_call_output",
        "call_id": "call_123",
        "output": "579"
      }
    ]
  }'
```

**结果**: ❌ 返回错误

```json
{
  "error": "input/messages 为空，无法生成 prompt"
}
```

**原因**: 代理层强制要求必须有 user message 才能生成 prompt。

## 根本原因分析

### 代理层实现

1. **解析层**: ✅ 已支持 `<tool_name>` 格式解析
2. **Responses 构建**: ✅ `BuildResponsesResponseFromText` 正确生成 `function_call` items
3. **流式 SSE**: ✅ `streamResponses` 正确发出 `function_call` 事件
4. **Tool System Prefix**: ✅ 发送 `tc_protocol` 指令给上游模型

### 问题定位

问题不在代理层，而在**上游模型 (deepseek/fast) 对 Responses API 的响应行为**：

- 同样的 `tc_protocol` 指令，在 Chat API 下模型能正确输出 `<tool_name>` 标签
- 在 Responses API 下，模型输出**自然语言描述**而非结构化标签

**可能原因**:
1. 上游模型对 Responses API 的 system prompt 理解不同
2. `input` 数组格式与 `messages` 格式对模型的影响不同
3. 模型训练数据中 Responses API 的 tool call 样本较少

## 当前推荐用法

### ✅ 可用场景

- 简单对话问答
- 基于历史工具结果继续对话
- 流式文本输出

### ❌ 不可用场景

- 需要模型主动调用工具的 Agent Loop
- 强制工具调用 (`tool_choice: "required"`)
- 结构化函数调用输出

### 替代方案

如需工具调用功能，请使用 `/v1/chat/completions`：

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "messages": [{"role": "user", "content": "计算 123+456"}],
    "tools": [...],
    "tool_choice": "required"
  }'
```

该接口已验证能正确返回 `tool_calls`。

## 测试命令汇总

### 成功用例

```bash
# 1. 基础文本
curl -X POST http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4.1","input":"hello"}'

# 2. 流式文本
curl -X POST http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4.1","input":"hello","stream":true}'

# 3. 工具历史 + 继续对话
curl -X POST http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model":"gpt-4.1",
    "input":[
      {"type":"function_call","call_id":"c1","name":"add","arguments":"{}"},
      {"type":"function_call_output","call_id":"c1","output":"579"},
      {"type":"message","role":"user","content":[{"type":"input_text","text":"继续"}]}
    ]
  }'
```

### 失败用例

```bash
# 1. 强制工具调用
curl -X POST http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model":"gpt-4.1",
    "input":"调用 add 工具计算 123+456",
    "tools":[{
      "name":"add",
      "description":"Add numbers",
      "input_schema":{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}
    }],
    "tool_choice":"required"
  }'
# 返回自然语言而非 function_call

# 2. 仅有 function_call_output
curl -X POST http://localhost:8080/v1/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model":"gpt-4.1",
    "input":[{"type":"function_call_output","call_id":"c1","output":"579"}]
  }'
# 返回 "input/messages 为空" 错误
```

## 下一步建议

1. **修改上游模型配置**: 调整 deepseek/fast 的 Responses API system prompt 格式
2. **增加示例**: 在 `tc_protocol` 中添加更多 few-shot 示例
3. **实验不同指令**: 尝试使用更明确的指令格式
4. **考虑模型切换**: Responses API + tools 可能需要特定模型支持

---

**结论**: 当前 Responses API 适合作为**文本对话接口**使用，如需完整 Agent Loop 功能，请优先使用 `/v1/chat/completions`。
