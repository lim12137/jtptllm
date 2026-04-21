package openai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseMessagesRequestBasic(t *testing.T) {
	payload := map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"stream": false,
	}
	parsed := ParseMessagesRequest(payload)
	if parsed.Model != "claude-sonnet-4-6" {
		t.Fatalf("model=%q", parsed.Model)
	}
	if parsed.Stream {
		t.Fatal("stream should be false")
	}
	if !strings.Contains(parsed.Prompt, "user: hello") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
	if !strings.Contains(parsed.Prompt, "model = deepseek") {
		t.Fatalf("prompt should contain deepseek marker, got=%q", parsed.Prompt)
	}
	if parsed.HasTools {
		t.Fatal("has_tools should be false")
	}
}

func TestParseMessagesRequestHaikuMapsToFast(t *testing.T) {
	payload := map[string]any{
		"model": "claude-haiku-4-5",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	parsed := ParseMessagesRequest(payload)
	if !strings.Contains(parsed.Prompt, "model = fast") {
		t.Fatalf("haiku should map to fast, prompt=%q", parsed.Prompt)
	}
}

func TestParseMessagesRequestWithSystem(t *testing.T) {
	payload := map[string]any{
		"model": "claude-sonnet-4-6",
		"system": "You are helpful.",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	parsed := ParseMessagesRequest(payload)
	if !strings.Contains(parsed.Prompt, "system: You are helpful.") {
		t.Fatalf("prompt should contain system, got=%q", parsed.Prompt)
	}
	if !strings.Contains(parsed.Prompt, "user: hi") {
		t.Fatalf("prompt should contain user, got=%q", parsed.Prompt)
	}
}

func TestParseMessagesRequestWithSystemArray(t *testing.T) {
	payload := map[string]any{
		"model": "claude-sonnet-4-6",
		"system": []any{
			map[string]any{"type": "text", "text": "Be concise."},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	parsed := ParseMessagesRequest(payload)
	if !strings.Contains(parsed.Prompt, "system: Be concise.") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseMessagesRequestWithTools(t *testing.T) {
	payload := map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []any{
			map[string]any{"role": "user", "content": "use tool"},
		},
		"tools": []any{
			map[string]any{
				"name":        "Read",
				"description": "Read a file",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_path": map[string]any{"type": "string"},
					},
					"required": []any{"file_path"},
				},
			},
		},
	}
	parsed := ParseMessagesRequest(payload)
	if !parsed.HasTools {
		t.Fatal("has_tools should be true")
	}
	if !strings.Contains(parsed.Prompt, "tc_protocol") {
		t.Fatalf("prompt should contain tc_protocol, got=%q", parsed.Prompt)
	}
}

func TestParseMessagesRequestToolChoiceNamed(t *testing.T) {
	payload := map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []any{
			map[string]any{"role": "user", "content": "test"},
		},
		"tools": []any{
			map[string]any{"name": "Read", "input_schema": map[string]any{"type": "object"}},
		},
		"tool_choice": map[string]any{"type": "tool", "name": "Read"},
	}
	parsed := ParseMessagesRequest(payload)
	if !strings.Contains(parsed.Prompt, "must respond with exactly one tool call") {
		t.Fatalf("prompt should force tool call, got=%q", parsed.Prompt)
	}
}

func TestParseMessagesRequestContentArray(t *testing.T) {
	payload := map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "hello world"},
				},
			},
		},
	}
	parsed := ParseMessagesRequest(payload)
	if !strings.Contains(parsed.Prompt, "user: hello world") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseMessagesRequestToolUseContent(t *testing.T) {
	payload := map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []any{
			map[string]any{"role": "user", "content": "use tool"},
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_abc123",
						"name":  "Read",
						"input": map[string]any{"file_path": "/test.txt"},
					},
				},
			},
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "toolu_abc123",
						"content":     "file contents here",
					},
				},
			},
		},
	}
	parsed := ParseMessagesRequest(payload)
	if !strings.Contains(parsed.Prompt, "[tool_use: Read") {
		t.Fatalf("prompt should contain tool_use, got=%q", parsed.Prompt)
	}
	if !strings.Contains(parsed.Prompt, "[tool_result: toolu_abc123]") {
		t.Fatalf("prompt should contain tool_result, got=%q", parsed.Prompt)
	}
}

func TestBuildMessagesResponsePlainText(t *testing.T) {
	resp := BuildMessagesResponseFromText("hello world", "claude-sonnet-4-6")
	if resp["type"] != "message" {
		t.Fatalf("type=%v", resp["type"])
	}
	if resp["role"] != "assistant" {
		t.Fatalf("role=%v", resp["role"])
	}
	if resp["stop_reason"] != "end_turn" {
		t.Fatalf("stop_reason=%v", resp["stop_reason"])
	}
	content, ok := resp["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content=%v", resp["content"])
	}
	block := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Fatalf("block type=%v", block["type"])
	}
	if block["text"] != "hello world" {
		t.Fatalf("block text=%v", block["text"])
	}
}

func TestBuildMessagesResponseFromTextPlainText(t *testing.T) {
	resp := BuildMessagesResponseFromText("simple text", "claude-sonnet-4-6")
	if resp["stop_reason"] != "end_turn" {
		t.Fatalf("stop_reason=%v", resp["stop_reason"])
	}
}

func TestBuildMessagesResponseFromTextWithToolCall(t *testing.T) {
	text := "Let me read that file.<<<TC>>>{\"tc\":[{\"n\":\"Read\",\"id\":\"call_abc\",\"a\":{\"file_path\":\"test.go\"}}]}<<<END>>>"
	resp := BuildMessagesResponseFromText(text, "claude-sonnet-4-6")
	if resp["stop_reason"] != "tool_use" {
		t.Fatalf("stop_reason=%v", resp["stop_reason"])
	}
	content, ok := resp["content"].([]any)
	if !ok || len(content) < 1 {
		t.Fatalf("content=%v", resp["content"])
	}
	var foundTool bool
	for _, c := range content {
		block := c.(map[string]any)
		if block["type"] == "tool_use" {
			foundTool = true
			if block["name"] != "Read" {
				t.Fatalf("tool name=%v", block["name"])
			}
			if block["id"] != "call_abc" {
				t.Fatalf("tool id=%v", block["id"])
			}
		}
	}
	if !foundTool {
		t.Fatal("no tool_use block found")
	}
}

func TestBuildMessagesSSEPlainText(t *testing.T) {
	events := BuildMessagesSSE("hello", "claude-sonnet-4-6", nil)
	eventTypes := extractMsgSSEEventTypes(events)
	wantTypes := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	for _, want := range wantTypes {
		found := false
		for _, got := range eventTypes {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing event type %q, got=%v", want, eventTypes)
		}
	}
	lastDelta := findMsgEventPayload(events, "message_delta")
	if delta, ok := lastDelta["delta"].(map[string]any); ok {
		if delta["stop_reason"] != "end_turn" {
			t.Fatalf("stop_reason=%v", delta["stop_reason"])
		}
	}
}

func TestBuildMessagesSSEWithToolCall(t *testing.T) {
	text := "<<<TC>>>{\"tc\":[{\"n\":\"Read\",\"id\":\"call_123\",\"a\":{\"file_path\":\"a.go\"}}]}<<<END>>>"
	events := BuildMessagesSSE(text, "claude-sonnet-4-6", nil)
	hasToolBlockStart := false
	for _, line := range events {
		if strings.Contains(line, "content_block_start") && strings.Contains(line, "tool_use") {
			hasToolBlockStart = true
		}
	}
	if !hasToolBlockStart {
		t.Fatal("missing tool_use content_block_start")
	}
	lastDelta := findMsgEventPayload(events, "message_delta")
	if delta, ok := lastDelta["delta"].(map[string]any); ok {
		if delta["stop_reason"] != "tool_use" {
			t.Fatalf("stop_reason=%v", delta["stop_reason"])
		}
	}
}

func TestNormalizeMessagesUsage(t *testing.T) {
	raw := map[string]any{"input_tokens": 100, "output_tokens": 50}
	usage := NormalizeMessagesUsage(raw)
	if usage["input_tokens"] != 100 {
		t.Fatalf("input_tokens=%v", usage["input_tokens"])
	}
	if usage["output_tokens"] != 50 {
		t.Fatalf("output_tokens=%v", usage["output_tokens"])
	}
}

func TestNormalizeMessagesUsageFromPromptTokens(t *testing.T) {
	raw := map[string]any{"prompt_tokens": 200, "completion_tokens": 80}
	usage := NormalizeMessagesUsage(raw)
	if usage["input_tokens"] != 200 {
		t.Fatalf("input_tokens=%v", usage["input_tokens"])
	}
	if usage["output_tokens"] != 80 {
		t.Fatalf("output_tokens=%v", usage["output_tokens"])
	}
}

func TestNormalizeMessagesUsageNil(t *testing.T) {
	if usage := NormalizeMessagesUsage(nil); usage != nil {
		t.Fatalf("expected nil, got=%v", usage)
	}
}

func TestExtractAnthropicTools(t *testing.T) {
	payload := map[string]any{
		"tools": []any{
			map[string]any{
				"name":        "Read",
				"description": "Read file",
				"input_schema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"file_path": map[string]any{"type": "string"}},
				},
			},
			map[string]any{
				"name":         "Write",
				"description":  "Write file",
				"input_schema": map[string]any{"type": "object"},
			},
		},
	}
	tools := extractAnthropicTools(payload)
	if len(tools) != 2 {
		t.Fatalf("len=%d", len(tools))
	}
	if tools[0]["name"] != "Read" {
		t.Fatalf("name=%v", tools[0]["name"])
	}
	if tools[1]["name"] != "Write" {
		t.Fatalf("name=%v", tools[1]["name"])
	}
}

func TestExtractAnthropicToolChoiceMapNamed(t *testing.T) {
	payload := map[string]any{
		"tool_choice": map[string]any{"type": "tool", "name": "Read"},
	}
	if choice := extractAnthropicToolChoice(payload); choice != "Read" {
		t.Fatalf("choice=%q", choice)
	}
}

func TestMessagesUsageFromHeuristicFallback(t *testing.T) {
	usage := MessagesUsageFromHeuristicFallback("hello", "world")
	if _, ok := usage["input_tokens"]; !ok {
		t.Fatal("missing input_tokens")
	}
	if _, ok := usage["output_tokens"]; !ok {
		t.Fatal("missing output_tokens")
	}
}

func TestBuildMessagesSSEWithUsage(t *testing.T) {
	usage := map[string]any{"input_tokens": 42, "output_tokens": 13}
	events := BuildMessagesSSE("hello", "claude-sonnet-4-6", usage)
	// Check message_start has input_tokens from usage
	startPayload := findMsgEventPayload(events, "message_start")
	if startPayload == nil {
		t.Fatal("missing message_start payload")
	}
	msg, ok := startPayload["message"].(map[string]any)
	if !ok {
		t.Fatal("message_start has no message field")
	}
	msgUsage, ok := msg["usage"].(map[string]any)
	if !ok {
		t.Fatal("message_start message has no usage")
	}
	inTok, _ := msgUsage["input_tokens"].(float64)
	if inTok != 42 {
		t.Fatalf("input_tokens=%v, want 42", msgUsage["input_tokens"])
	}
	// Check message_delta has output_tokens from usage
	deltaPayload := findMsgEventPayload(events, "message_delta")
	if deltaPayload == nil {
		t.Fatal("missing message_delta payload")
	}
	deltaUsage, ok := deltaPayload["usage"].(map[string]any)
	if !ok {
		t.Fatal("message_delta has no usage")
	}
	outTok, _ := deltaUsage["output_tokens"].(float64)
	if outTok != 13 {
		t.Fatalf("output_tokens=%v, want 13", deltaUsage["output_tokens"])
	}
}

func extractMsgSSEEventTypes(events []string) []string {
	var types []string
	for _, event := range events {
		for _, line := range strings.Split(event, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "event: ") {
				types = append(types, strings.TrimPrefix(line, "event: "))
			}
		}
	}
	return types
}

func findMsgEventPayload(events []string, eventType string) map[string]any {
	for _, event := range events {
		var foundEvent, foundData bool
		var data string
		for _, line := range strings.Split(event, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "event: "+eventType) {
				foundEvent = true
			}
			if foundEvent && strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
				foundData = true
				break
			}
		}
		if foundEvent && foundData {
			var payload map[string]any
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				return nil
			}
			return payload
		}
	}
	return nil
}
