package openai

import "testing"

func TestChatToPrompt(t *testing.T) {
	in := ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
	if PromptFromChat(in) != "user: hi" {
		t.Fatal("prompt")
	}
}

// TestCoerceToolResultString 验证 Anthropic/Claude SDK 风格的 tool_result（字符串 content）能被提取。
func TestCoerceToolResultString(t *testing.T) {
	content := []any{
		map[string]any{
			"type":    "tool_result",
			"content": "2026-06-15",
		},
	}
	if got := coerceContentToStr(content); got != "2026-06-15" {
		t.Fatalf("expected tool_result string content, got %q", got)
	}
}

// TestCoerceToolResultStructured 验证 tool_result 的 content 是结构化数组时也能提取。
func TestCoerceToolResultStructured(t *testing.T) {
	content := []any{
		map[string]any{
			"type": "tool_result",
			"content": []any{
				map[string]any{"type": "text", "text": "晴，26C"},
			},
		},
	}
	if got := coerceContentToStr(content); got != "晴，26C" {
		t.Fatalf("expected structured tool_result content, got %q", got)
	}
}

// TestCoerceMixedToolResultAndText 验证同一消息里 tool_result 与 text 混合时都能提取。
func TestCoerceMixedToolResultAndText(t *testing.T) {
	content := []any{
		map[string]any{"type": "text", "text": "结果如下："},
		map[string]any{"type": "tool_result", "content": "ok"},
	}
	if got := coerceContentToStr(content); got != "结果如下：ok" {
		t.Fatalf("expected mixed content, got %q", got)
	}
}