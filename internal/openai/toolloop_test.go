package openai

import (
	"strings"
	"testing"
)

func mkMsg(role string, content string) map[string]any {
	return map[string]any{"role": role, "content": content}
}

func mkAssistantWithToolCall(name, args string) map[string]any {
	return map[string]any{
		"role":    "assistant",
		"content": "",
		"tool_calls": []any{
			map[string]any{
				"id": "call_x",
				"function": map[string]any{
					"name":      name,
					"arguments": args,
				},
			},
		},
	}
}

func mkAssistantWithToolCalls(calls ...[2]string) map[string]any {
	tcs := make([]any, 0, len(calls))
	for i, c := range calls {
		tcs = append(tcs, map[string]any{
			"id": "call_" + string(rune('a'+i)),
			"function": map[string]any{
				"name":      c[0],
				"arguments": c[1],
			},
		})
	}
	return map[string]any{"role": "assistant", "content": "", "tool_calls": tcs}
}

// TestAnalyzeToolLoopNoTools 无工具调用时不触发强制停止。
func TestAnalyzeToolLoopNoTools(t *testing.T) {
	msgs := []map[string]any{
		mkMsg("user", "你好"),
		mkMsg("assistant", "你好，有什么可以帮你？"),
	}
	d := AnalyzeToolLoop(msgs)
	if d.ForceStop {
		t.Fatalf("should not force stop, reason=%s", d.Reason)
	}
}

// TestAnalyzeToolLoopUnderLimit 工具结果未达阈值时不触发。
func TestAnalyzeToolLoopUnderLimit(t *testing.T) {
	msgs := []map[string]any{
		mkMsg("user", "查天气"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴 26C"),
	}
	d := AnalyzeToolLoop(msgs)
	if d.ForceStop {
		t.Fatalf("1 tool result should not force stop, reason=%s", d.Reason)
	}
}

// TestAnalyzeToolLoopAtLimit 工具结果达到上限时强制停止。
func TestAnalyzeToolLoopAtLimit(t *testing.T) {
	msgs := []map[string]any{mkMsg("user", "多步任务")}
	for i := 0; i < toolCallHardLimit; i++ {
		msgs = append(msgs, mkAssistantWithToolCall("read", `{"path":"f`+string(rune('a'+i))+`"}`))
		msgs = append(msgs, mkMsg("tool", "result "+string(rune('a'+i))))
	}
	d := AnalyzeToolLoop(msgs)
	if !d.ForceStop {
		t.Fatal("should force stop when tool results reach limit")
	}
	if !strings.Contains(d.Reason, "tool-result-limit") {
		t.Fatalf("unexpected reason: %s", d.Reason)
	}
	if d.Hint == "" {
		t.Fatal("force stop hint should be non-empty")
	}
}

// TestAnalyzeToolLoopDuplicateCalls 连续重复调用同一工具+参数触发循环检测。
func TestAnalyzeToolLoopDuplicateCalls(t *testing.T) {
	msgs := []map[string]any{
		mkMsg("user", "查天气"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴 26C"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`), // 重复
		mkMsg("tool", "晴 26C"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`), // 再重复
	}
	d := AnalyzeToolLoop(msgs)
	if !d.ForceStop {
		t.Fatal("should force stop on duplicate tool calls")
	}
	if !strings.Contains(d.Reason, "duplicate-tool-call:get_weather") {
		t.Fatalf("unexpected reason: %s", d.Reason)
	}
}

// TestAnalyzeToolLoopDifferentArgsNotDuplicate 不同参数不算重复。
func TestAnalyzeToolLoopDifferentArgsNotDuplicate(t *testing.T) {
	msgs := []map[string]any{
		mkMsg("user", "查天气"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴 26C"),
		mkAssistantWithToolCall("get_weather", `{"city":"上海"}`), // 不同城市
		mkMsg("tool", "雨 22C"),
	}
	d := AnalyzeToolLoop(msgs)
	if d.ForceStop {
		t.Fatalf("different args should not be duplicate, reason=%s", d.Reason)
	}
}

// TestAnalyzeToolLoopWhitespaceArgsNormalized 参数空白差异仍判为重复。
func TestAnalyzeToolLoopWhitespaceArgsNormalized(t *testing.T) {
	msgs := []map[string]any{
		mkMsg("user", "查天气"),
		mkAssistantWithToolCall("get_weather", `{"city": "北京"}`), // 有空格
		mkMsg("tool", "晴 26C"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`), // 无空格
		mkMsg("tool", "晴 26C"),
		mkAssistantWithToolCall("get_weather", `{"city": "北京"}`),
	}
	d := AnalyzeToolLoop(msgs)
	if !d.ForceStop {
		t.Fatal("whitespace-only arg difference should still be detected as duplicate")
	}
}

// TestAnalyzeToolLoopAnthropicStyleToolResult 兼容 Anthropic 风格 tool_result 计数。
func TestAnalyzeToolLoopAnthropicStyleToolResult(t *testing.T) {
	toolResultUser := map[string]any{
		"role": "user",
		"content": []any{
			map[string]any{
				"type":    "tool_result",
				"content": "2026-06-15",
			},
		},
	}
	msgs := []map[string]any{mkMsg("user", "task")}
	for i := 0; i < toolCallHardLimit; i++ {
		msgs = append(msgs, mkAssistantWithToolCall("read", `{"path":"f`+string(rune('a'+i))+`"}`))
		// 深拷贝 tool_result user
		msgs = append(msgs, map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":    "tool_result",
					"content": "result " + string(rune('a'+i)),
				},
			},
		})
	}
	_ = toolResultUser
	d := AnalyzeToolLoop(msgs)
	if !d.ForceStop {
		t.Fatal("should force stop with Anthropic-style tool_result counting")
	}
}

// TestForceStopPlanRewritesNeedTool ForceStopPlan 把 need_tool=true 改写为总结。
func TestForceStopPlanRewritesNeedTool(t *testing.T) {
	msgs := []map[string]any{
		mkMsg("user", "查天气"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴 26C"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴 26C"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
	}
	plan := ToolPlan{
		NeedTool:  true,
		ToolName:  "get_weather",
		Arguments: map[string]any{"city": "北京"},
	}
	stopped := ForceStopPlan(plan, msgs, "duplicate-tool-call:get_weather")
	if stopped.NeedTool {
		t.Fatal("ForceStopPlan should set NeedTool=false")
	}
	if stopped.ToolName != "" {
		t.Fatal("ForceStopPlan should clear ToolName")
	}
	if strings.TrimSpace(stopped.FinalAnswer) == "" {
		t.Fatal("ForceStopPlan should synthesize a non-empty final answer")
	}
}

// TestForceStopPlanKeepsNoToolPlan planner 已返回 need_tool=false 时不改写。
func TestForceStopPlanKeepsNoToolPlan(t *testing.T) {
	plan := ToolPlan{
		NeedTool:    false,
		FinalAnswer: "已经够了",
	}
	stopped := ForceStopPlan(plan, nil, "tool-result-limit:6")
	if stopped.NeedTool {
		t.Fatal("should remain NeedTool=false")
	}
	if stopped.FinalAnswer != "已经够了" {
		t.Fatalf("should keep original final answer, got %q", stopped.FinalAnswer)
	}
}

// TestForceStopPlanKeepsPlannerAnswer planner 自带 final_answer 时优先保留。
func TestForceStopPlanKeepsPlannerAnswer(t *testing.T) {
	msgs := []map[string]any{
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴 26C"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴 26C"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
	}
	plan := ToolPlan{
		NeedTool:    true,
		ToolName:    "get_weather",
		Arguments:   map[string]any{"city": "北京"},
		FinalAnswer: "北京今天晴，26度。",
	}
	stopped := ForceStopPlan(plan, msgs, "duplicate")
	if stopped.FinalAnswer != "北京今天晴，26度。" {
		t.Fatalf("should keep planner's own answer, got %q", stopped.FinalAnswer)
	}
}

// TestSynthesizeFinalAnswerEmpty 无工具结果时返回空。
func TestSynthesizeFinalAnswerEmpty(t *testing.T) {
	msgs := []map[string]any{
		mkMsg("user", "你好"),
		mkMsg("assistant", "你好"),
	}
	if got := synthesizeFinalAnswerFromToolResults(msgs); got != "" {
		t.Fatalf("expected empty synthesis, got %q", got)
	}
}

// TestSynthesizeFinalAnswerFromResults 有工具结果时合成非空回答。
func TestSynthesizeFinalAnswerFromResults(t *testing.T) {
	msgs := []map[string]any{
		mkMsg("user", "查天气"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴 26C"),
	}
	got := synthesizeFinalAnswerFromToolResults(msgs)
	if !strings.Contains(got, "get_weather") {
		t.Fatalf("synthesis should mention tool call, got %q", got)
	}
	if !strings.Contains(got, "晴 26C") {
		t.Fatalf("synthesis should include tool result, got %q", got)
	}
}

// TestSynthesizeFinalAnswerMultipleToolCalls 多工具调用正确配对。
func TestSynthesizeFinalAnswerMultipleToolCalls(t *testing.T) {
	msgs := []map[string]any{
		mkMsg("user", "多步任务"),
		mkAssistantWithToolCalls(
			[2]string{"get_skill", `{"id":"skill-creator"}`},
			[2]string{"read", `{"path":"code-agent.ts"}`},
		),
		mkMsg("tool", "skill 内容"),
		mkMsg("tool", "源码内容"),
	}
	got := synthesizeFinalAnswerFromToolResults(msgs)
	if !strings.Contains(got, "get_skill") {
		t.Fatalf("should mention get_skill call")
	}
	if !strings.Contains(got, "read") {
		t.Fatalf("should mention read call")
	}
}

// TestAnalyzeToolLoopNewUserResetsCounter 新 user 消息应重置工具计数，不误伤后续调用。
// 这是修复"改写后中断多轮调用"问题的关键用例：历史回合累积的工具结果不计入当前回合。
func TestAnalyzeToolLoopNewUserResetsCounter(t *testing.T) {
	msgs := []map[string]any{
		// 回合1：累积达到上限的工具结果（历史回合）
		mkMsg("user", "查北京天气"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴"),
		// 回合2：新 user 消息，计数应重置
		mkMsg("user", "再查上海天气"),
		mkAssistantWithToolCall("get_weather", `{"city":"上海"}`),
		mkMsg("tool", "雨"),
	}
	d := AnalyzeToolLoop(msgs)
	if d.ForceStop {
		t.Fatalf("new user message should reset counter, should NOT force stop; reason=%s", d.Reason)
	}
}

// TestAnalyzeToolLoopDuplicateOnlyWithinCurrentTurn 重复检测也限定在当前回合。
func TestAnalyzeToolLoopDuplicateOnlyWithinCurrentTurn(t *testing.T) {
	msgs := []map[string]any{
		// 回合1：有重复调用（历史回合）
		mkMsg("user", "查北京天气"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴"),
		mkAssistantWithToolCall("get_weather", `{"city":"北京"}`),
		mkMsg("tool", "晴"),
		// 回合2：新请求，合理的新调用
		mkMsg("user", "查上海天气"),
		mkAssistantWithToolCall("get_weather", `{"city":"上海"}`),
		mkMsg("tool", "雨"),
	}
	d := AnalyzeToolLoop(msgs)
	if d.ForceStop {
		t.Fatalf("duplicate detection should be scoped to current turn; reason=%s", d.Reason)
	}
}

// TestAnalyzeToolLoopCurrentTurnHitsLimit 当前回合内达到上限仍应触发。
func TestAnalyzeToolLoopCurrentTurnHitsLimit(t *testing.T) {
	msgs := []map[string]any{
		// 旧回合（不应影响）
		mkMsg("user", "旧任务"),
		mkAssistantWithToolCall("read", `{"path":"old"}`),
		mkMsg("tool", "old result"),
		// 新回合：本回合内累积到上限
		mkMsg("user", "新任务"),
	}
	for i := 0; i < toolCallHardLimit; i++ {
		msgs = append(msgs, mkAssistantWithToolCall("read", `{"path":"f`+string(rune('a'+i))+`"}`))
		msgs = append(msgs, mkMsg("tool", "result "+string(rune('a'+i))))
	}
	d := AnalyzeToolLoop(msgs)
	if !d.ForceStop {
		t.Fatal("current turn reaching limit should force stop")
	}
	if !strings.Contains(d.Reason, "tool-result-limit") {
		t.Fatalf("unexpected reason: %s", d.Reason)
	}
}
