package openai

import (
	"fmt"
	"strings"
)

const (
	// toolCallHardLimit 对话历史中成功 tool 结果达到此阈值后，强制 planner 进入总结态。
	// code-agent 默认 maxIterations 通常 8~12，这里取保守的 6，给模型留出少量缓冲，
	// 同时避免日志里看到的 messages 一路涨到 60 的失控场景。
	toolCallHardLimit = 6

	// duplicateToolCallLimit 连续重复调用（同名+同参）达到此阈值即判定为循环，强制终止。
	duplicateToolCallLimit = 2
)

// toolCallRecord 记录一次工具调用签名（name + 归一化 arguments）。
type toolCallRecord struct {
	name string
	args string
}

// ToolLoopDecision 描述对当前请求的工具循环收敛判定结果。
type ToolLoopDecision struct {
	// ForceStop 为 true 表示应强制 planner 返回最终回答，不再调用工具。
	ForceStop bool
	// Reason 触发强制停止的原因（用于日志/调试）。
	Reason string
	// Hint 注入到 planning prompt 的强制停止提示（ForceStop 为 true 时非空）。
	Hint string
}

// AnalyzeToolLoop 扫描 messages 历史，判定是否应强制 planner 收敛。
// 判定规则（任一命中即 ForceStop）：
//  1. 成功 tool 结果数 >= toolCallHardLimit
//  2. 同一 (tool_name, arguments) 连续重复 >= duplicateToolCallLimit
//
// 重要：所有统计都限定在「最近一条 user 消息之后」的范围内，即"当前用户回合"。
// 这样在多轮对话中，新 user 消息会重置计数，不会因为历史累积的旧工具结果
// 而误伤合理的后续工具调用。注意：纯 tool_result 消息（Anthropic 风格 user+tool_result）
// 不会被当作"新 user 回合"的分界——只认 role=user 且不是 tool_result 容器的消息。
func AnalyzeToolLoop(messages []map[string]any) ToolLoopDecision {
	currentTurn := truncateToCurrentTurn(messages)
	toolResults := countSuccessfulToolResults(currentTurn)
	calls := extractAssistantToolCalls(currentTurn)

	if dup := detectDuplicateCalls(calls); dup != "" {
		return ToolLoopDecision{
			ForceStop: true,
			Reason:    "duplicate-tool-call:" + dup,
			Hint:      buildForceStopHint(toolResults, "检测到重复调用同一工具且参数相同（"+dup+"），这通常是循环，必须直接基于已有工具结果给出最终回答，禁止再次调用任何工具。"),
		}
	}

	if toolResults >= toolCallHardLimit {
		return ToolLoopDecision{
			ForceStop: true,
			Reason:    fmt.Sprintf("tool-result-limit:%d", toolResults),
			Hint:      buildForceStopHint(toolResults, ""),
		}
	}

	return ToolLoopDecision{ForceStop: false}
}

// truncateToCurrentTurn 截取「最近一条真实 user 消息」到末尾的消息切片。
// 真实 user 消息指 role=user 且不是纯 tool_result 容器（Anthropic 风格）的消息。
// 这样统计工具调用时只看当前用户回合，历史回合的工具结果不计入。
func truncateToCurrentTurn(messages []map[string]any) []map[string]any {
	lastUserIdx := -1
	for i, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(strOr(msg["role"], "")))
		if role != "user" {
			continue
		}
		if isAnthropicToolResultContainer(msg) {
			// Anthropic 风格的 tool_result 是 user 消息，但不是新用户回合
			continue
		}
		lastUserIdx = i
	}
	if lastUserIdx < 0 {
		// 没有真实 user 消息（例如纯工具结果回写场景），用全部历史
		return messages
	}
	return messages[lastUserIdx:]
}

// isAnthropicToolResultContainer 判断 user 消息是否实际是 Anthropic 风格的 tool_result 容器。
// 判定：content 为数组且至少包含一个 tool_result 块。
func isAnthropicToolResultContainer(msg map[string]any) bool {
	list, ok := msg["content"].([]any)
	if !ok {
		return false
	}
	for _, part := range list {
		if pm, ok := part.(map[string]any); ok {
			if pt, _ := pm["type"].(string); pt == "tool_result" {
				return true
			}
		}
	}
	return false
}

// buildForceStopHint 构造注入 planning prompt 的强制停止提示。
func buildForceStopHint(toolResults int, extra string) string {
	var b strings.Builder
	b.WriteString("\n【强制收敛】当前对话已有 ")
	b.WriteString(fmt.Sprintf("%d", toolResults))
	b.WriteString(" 个工具结果，已足够回答用户问题。本轮必须返回 need_tool=false 并在 final_answer 中总结所有工具结果；禁止再次调用任何工具。")
	if extra != "" {
		b.WriteString("\n")
		b.WriteString(extra)
	}
	return b.String()
}

// countSuccessfulToolResults 统计历史中 role=tool 且 content 非空的消息数。
// 兼容 Anthropic 风格（user 内含 tool_result 块）。
func countSuccessfulToolResults(messages []map[string]any) int {
	n := 0
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(strOr(msg["role"], "")))
		switch role {
		case "tool", "function":
			if strings.TrimSpace(coerceContentToStr(msg["content"])) != "" {
				n++
			}
		case "user":
			// Anthropic 风格：content 为数组且含 tool_result 块
			if list, ok := msg["content"].([]any); ok {
				for _, part := range list {
					if pm, ok := part.(map[string]any); ok {
						if pt, _ := pm["type"].(string); pt == "tool_result" {
							if strings.TrimSpace(coerceToolResultContent(pm["content"])) != "" {
								n++
							}
						}
					}
				}
			}
		}
	}
	return n
}

// extractAssistantToolCalls 按顺序提取所有 assistant 消息的 tool_calls 签名。
func extractAssistantToolCalls(messages []map[string]any) []toolCallRecord {
	var out []toolCallRecord
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(strOr(msg["role"], "")))
		if role != "assistant" {
			continue
		}
		calls, ok := msg["tool_calls"].([]any)
		if !ok {
			continue
		}
		for _, c := range calls {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			fn, _ := cm["function"].(map[string]any)
			if fn == nil {
				continue
			}
			name, _ := fn["name"].(string)
			args, _ := fn["arguments"].(string)
			if name == "" {
				continue
			}
			out = append(out, toolCallRecord{name: name, args: normalizeArgs(args)})
		}
	}
	return out
}

// detectDuplicateCalls 检测是否存在连续重复的同一工具调用签名。
// 返回触发重复的工具名（用于日志），无重复返回空串。
func detectDuplicateCalls(calls []toolCallRecord) string {
	if len(calls) < duplicateToolCallLimit {
		return ""
	}
	// 检查尾部连续相同调用
	last := calls[len(calls)-1]
	streak := 1
	for i := len(calls) - 2; i >= 0; i-- {
		if calls[i] == last {
			streak++
			if streak >= duplicateToolCallLimit {
				return last.name
			}
		} else {
			break
		}
	}
	return ""
}

// normalizeArgs 归一化 arguments 字符串，用于去重比较。
// 去除空白差异，避免 {"a":1} 和 {"a": 1} 被判为不同。
func normalizeArgs(args string) string {
	var b strings.Builder
	for _, r := range args {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// synthesizeFinalAnswerFromToolResults 在强制收敛时，从历史工具结果合成一个兜底最终回答。
// 不追求高质量——目的是打破循环，把已收集的信息交给用户/code-agent 继续处理。
// 采用"最近若干条 tool 结果摘要"的形式，控制长度避免再次撑爆上下文。
func synthesizeFinalAnswerFromToolResults(messages []map[string]any) string {
	type toolEntry struct {
		call string
		resp string
	}
	var entries []toolEntry

	// 按 assistant tool_calls + 后续 tool 结果配对
	var pendingCalls []string
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(strOr(msg["role"], "")))
		if role == "assistant" {
			if calls, ok := msg["tool_calls"].([]any); ok {
				for _, c := range calls {
					cm, ok := c.(map[string]any)
					if !ok {
						continue
					}
					fn, _ := cm["function"].(map[string]any)
					if fn == nil {
						continue
					}
					name, _ := fn["name"].(string)
					args, _ := fn["arguments"].(string)
					if name == "" {
						continue
					}
					if args != "" {
						pendingCalls = append(pendingCalls, name+"("+args+")")
					} else {
						pendingCalls = append(pendingCalls, name+"()")
					}
				}
			}
			continue
		}
		// 配对的工具结果
		var resp string
		switch role {
		case "tool", "function":
			resp = strings.TrimSpace(coerceContentToStr(msg["content"]))
		case "user":
			// Anthropic 风格
			if list, ok := msg["content"].([]any); ok {
				for _, part := range list {
					if pm, ok := part.(map[string]any); ok {
						if pt, _ := pm["type"].(string); pt == "tool_result" {
							resp = strings.TrimSpace(coerceToolResultContent(pm["content"]))
						}
					}
				}
			}
		}
		if resp == "" {
			continue
		}
		call := ""
		if len(pendingCalls) > 0 {
			call, pendingCalls = pendingCalls[0], pendingCalls[1:]
		}
		entries = append(entries, toolEntry{call: call, resp: resp})
	}

	if len(entries) == 0 {
		return ""
	}

	// 只保留最近若干条，避免单条超长结果撑爆 final_answer
	const maxEntries = 4
	const maxRespRunes = 400
	start := 0
	if len(entries) > maxEntries {
		start = len(entries) - maxEntries
	}
	recent := entries[start:]

	var b strings.Builder
	b.WriteString("已收集的工具结果摘要（已达到工具调用上限，自动收敛）：\n")
	for i, e := range recent {
		idx := start + i + 1
		label := fmt.Sprintf("结果%d", idx)
		if e.call != "" {
			label = e.call
		}
		b.WriteString("- ")
		b.WriteString(label)
		b.WriteString("：")
		b.WriteString(truncateRunes(e.resp, maxRespRunes))
		b.WriteString("\n")
	}
	b.WriteString("\n请基于上述结果总结回答用户的问题；如信息不足，请说明还缺什么。")
	return strings.TrimSpace(b.String())
}
