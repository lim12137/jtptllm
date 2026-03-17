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

func TestParseChatRequestWithToolsAddsSystemPrefix(t *testing.T) {
	payload := map[string]any{
		"model":    "agent",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "Get weather",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"location": map[string]any{"type": "string", "title": "loc"}},
					"required":   []any{"location"},
					"title":      "Weather",
				},
			},
		}},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": "get_weather"}},
	}
	parsed := ParseChatRequest(payload)
	if !strings.Contains(parsed.Prompt, "system:") {
		t.Fatalf("missing system prefix")
	}
	if !strings.Contains(parsed.Prompt, "get_weather") {
		t.Fatalf("missing tool name")
	}
	if strings.Contains(parsed.Prompt, "title") {
		t.Fatalf("schema not compressed")
	}
}
