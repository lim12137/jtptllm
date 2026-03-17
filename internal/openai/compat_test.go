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

func TestParseChatRequestCompressesItemsArray(t *testing.T) {
	payload := map[string]any{
		"model":    "agent",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "list_places",
				"description": "List places",
				"parameters": map[string]any{
					"type": "array",
					"items": []any{
						map[string]any{
							"type":  "object",
							"title": "ItemSchema",
							"properties": map[string]any{
								"name": map[string]any{"type": "string", "title": "Name"},
							},
						},
					},
				},
			},
		}},
	}
	parsed := ParseChatRequest(payload)
	if strings.Contains(parsed.Prompt, "title") {
		t.Fatalf("items schema not compressed")
	}
}

func TestParseToolSentinelToChatResponse(t *testing.T) {
	text := "<<<TC>>>{\"tc\":[{\"id\":\"call_1\",\"n\":\"get_weather\",\"a\":{\"location\":\"Paris\"}}],\"c\":\"\"}<<<END>>>"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	out := BuildChatCompletionResponseFromText(text, "agent")
	choices := out["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["tool_calls"] == nil {
		t.Fatalf("missing tool_calls")
	}
}
