package openai

import (
	"encoding/json"
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

func TestToolSystemPrefixIncludesProtocol(t *testing.T) {
	payload := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "get_weather",
				"parameters": map[string]any{"type": "object"},
			},
		}},
		"tool_choice": "none",
	}
	parsed := ParseChatRequest(payload)
	if !strings.Contains(parsed.Prompt, "tc_protocol") {
		t.Fatalf("missing tc_protocol")
	}
	if !strings.Contains(parsed.Prompt, "tc_forbid") {
		t.Fatalf("missing tc_forbid")
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

func TestParseToolSentinelExtractsContentAndArgs(t *testing.T) {
	text := "<<<TC>>>{\"tc\":[{\"n\":\"get_weather\",\"a\":{\"location\":\"Paris\",\"unit\":\"c\"}}],\"c\":\"ok\"}<<<END>>>"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.Content != "ok" {
		t.Fatalf("content=%q", res.Content)
	}
	if !strings.HasPrefix(res.ToolCalls[0].ID, "call_") {
		t.Fatalf("missing generated id")
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(res.ToolCalls[0].Arguments), &args); err != nil {
		t.Fatalf("bad args json: %v", err)
	}
	if args["location"] != "Paris" || args["unit"] != "c" {
		t.Fatalf("args=%v", args)
	}
}

func TestBuildChatCompletionResponseFromTextIncludesFunctionCall(t *testing.T) {
	text := "<<<TC>>>{\"tc\":[{\"id\":\"call_1\",\"n\":\"get_weather\",\"a\":{\"location\":\"Paris\"}}],\"c\":\"\"}<<<END>>>"
	out := BuildChatCompletionResponseFromText(text, "agent")
	choices := out["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["tool_calls"] == nil {
		t.Fatalf("missing tool_calls")
	}
	if msg["function_call"] == nil {
		t.Fatalf("missing function_call")
	}
	fc := msg["function_call"].(map[string]any)
	if fc["name"] != "get_weather" {
		t.Fatalf("function_call name=%v", fc["name"])
	}
}

func TestBuildResponsesResponseFromTextCreatesFunctionCalls(t *testing.T) {
	text := "<<<TC>>>{\"tc\":[{\"id\":\"call_1\",\"n\":\"get_weather\",\"a\":{\"location\":\"Paris\"}}],\"c\":\"\"}<<<END>>>"
	out := BuildResponsesResponseFromText(text, "agent")
	output := out["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("output len=%d", len(output))
	}
	item := output[0].(map[string]any)
	if item["type"] != "function_call" {
		t.Fatalf("type=%v", item["type"])
	}
	if item["name"] != "get_weather" {
		t.Fatalf("name=%v", item["name"])
	}
	if item["call_id"] != "call_1" {
		t.Fatalf("call_id=%v", item["call_id"])
	}
}

func TestParseToolSentinelFallbackToPlainText(t *testing.T) {
	text := "<<<TC>>>not json<<<END>>>"
	out := BuildChatCompletionResponseFromText(text, "agent")
	choices := out["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != text {
		t.Fatalf("content=%v", msg["content"])
	}
	if msg["tool_calls"] != nil {
		t.Fatalf("unexpected tool_calls")
	}

	out2 := BuildResponsesResponseFromText(text, "agent")
	if out2["output_text"] != text {
		t.Fatalf("output_text=%v", out2["output_text"])
	}
}

func TestParseToolSentinelFallbackJsonBlock(t *testing.T) {
	text := "answer\n```json\n{\"toolCallId\":\"tools_01\",\"toolName\":\"get_weather\",\"arguments\":{\"location\":\"Paris\"}}\n```"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.Content != "answer" {
		t.Fatalf("content=%q", res.Content)
	}
	if res.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	if res.ToolCalls[0].ID != "tools_01" {
		t.Fatalf("id=%q", res.ToolCalls[0].ID)
	}
}

func TestParseToolSentinelFallbackToolCalls(t *testing.T) {
	text := "prefix\n```json\n{\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":{\"location\":\"Paris\"}}}]}\n```\n"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	if res.ToolCalls[0].ID != "call_1" {
		t.Fatalf("id=%q", res.ToolCalls[0].ID)
	}
}

func TestParseToolSentinelFallbackFunctionCall(t *testing.T) {
	text := "```json\n{\"function_call\":{\"name\":\"get_weather\",\"arguments\":{\"location\":\"Paris\"}}}\n```"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	if res.ToolCalls[0].ID == "" {
		t.Fatalf("id empty")
	}
}
