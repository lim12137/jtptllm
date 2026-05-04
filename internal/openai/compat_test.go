package openai

import (
	"strings"
	"testing"
)

func TestChatToPrompt(t *testing.T) {
	in := ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
	if PromptFromChat(in) != "user: hi" {
		t.Fatal("prompt")
	}
}

func TestParseToolCallsFromText(t *testing.T) {
	got := ParseToolCallsFromText(`[function_calls][call:default_api:read_file]{"filePath":"/tmp/a.txt"}[/call][/function_calls]`, "fast")
	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls: %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Function.Name != "default_api:read_file" {
		t.Fatalf("name: %s", got.ToolCalls[0].Function.Name)
	}
	if got.Content != "" {
		t.Fatalf("content: %q", got.Content)
	}
}

func TestBuildChatCompletionResponseToolCalls(t *testing.T) {
	resp := BuildChatCompletionResponse(`[function_calls][call:write_to_file]{"filePath":"/tmp/a.txt","content":"hello"}[/call][/function_calls]`, "fast")
	choices := resp["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != nil {
		t.Fatalf("content: %#v", msg["content"])
	}
	tcs := msg["tool_calls"].([]ToolCall)
	if len(tcs) != 1 {
		t.Fatalf("tool calls: %d", len(tcs))
	}
	if !strings.Contains(tcs[0].Function.Arguments, `"filePath"`) {
		t.Fatalf("args: %s", tcs[0].Function.Arguments)
	}
}

func TestParseToolCallsFromTextWithContentPrefix(t *testing.T) {
	got := ParseToolCallsFromText(`before [function_calls][call:write_to_file]{"filePath":"/tmp/a.txt","content":"hello"}[/call][/function_calls] after`, "fast")
	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls: %d", len(got.ToolCalls))
	}
	if got.Content != "before  after" {
		t.Fatalf("content: %q", got.Content)
	}
}

func TestParseToolCallsFromTextForDeepSeekV32(t *testing.T) {
	got := ParseToolCallsFromText(`[function_calls][call:replace_in_file]{"filePath":"/tmp/a.txt","old_str":"a","new_str":"b"}[/call][/function_calls]`, "deepseek-v3.2")
	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls: %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Function.Name != "replace_in_file" {
		t.Fatalf("name: %s", got.ToolCalls[0].Function.Name)
	}
}

func TestParseToolCallsFromTextIgnoresOtherModels(t *testing.T) {
	raw := `[function_calls][call:write_to_file]{"filePath":"/tmp/a.txt","content":"hello"}[/call][/function_calls]`
	got := ParseToolCallsFromText(raw, "agent")
	if len(got.ToolCalls) != 0 {
		t.Fatalf("unexpected tool calls: %d", len(got.ToolCalls))
	}
	if got.Content != raw {
		t.Fatalf("content changed: %q", got.Content)
	}
}
