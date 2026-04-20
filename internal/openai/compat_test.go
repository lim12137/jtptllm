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

func TestPromptFromChatAssistantToolCallOnlySummarized(t *testing.T) {
	in := ChatRequest{
		Messages: []Message{
			{Role: "assistant", Content: `<tool_call><Read>{"file_path":"go.mod"}</Read></tool_call>`},
		},
	}
	got := PromptFromChat(in)
	want := "assistant: assistant_tool_call: Read"
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestPromptFromChatAssistantMixedNaturalLanguageAndToolCall(t *testing.T) {
	in := ChatRequest{
		Messages: []Message{
			{Role: "assistant", Content: `我先查一下 <tool_call><Read>{"file_path":"go.mod"}</Read></tool_call> 稍等`},
		},
	}
	got := PromptFromChat(in)
	want := "assistant: 我先查一下 稍等"
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestPromptFromChatAssistantThinkingRemoved(t *testing.T) {
	in := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: `<thinking>internal tool plan</thinking>可见回答`},
		},
	}
	got := PromptFromChat(in)
	want := "user: hi\nassistant: 可见回答"
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestPromptFromChatUserToolCallLikeTextUnchanged(t *testing.T) {
	userText := `<tool_call><Read>{"file_path":"go.mod"}</Read></tool_call>`
	in := ChatRequest{Messages: []Message{{Role: "user", Content: userText}}}
	got := PromptFromChat(in)
	want := "user: " + userText
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestPromptFromResponsesMessagesPathSanitizesAssistantHistory(t *testing.T) {
	payload := map[string]any{
		"messages": []any{
			map[string]any{"role": "assistant", "content": `<tool_call><Read>{"file_path":"go.mod"}</Read></tool_call>`},
		},
	}
	got := PromptFromResponses(payload)
	want := "assistant: assistant_tool_call: Read"
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestPromptFromResponsesInputArraySanitizesAssistantOnly(t *testing.T) {
	payload := map[string]any{
		"input": []any{
			map[string]any{
				"role":    "assistant",
				"content": `先查下 <tool_call><Read>{"file_path":"go.mod"}</Read></tool_call>`,
			},
			map[string]any{
				"role":    "user",
				"content": `<tool_call><Read>{"file_path":"go.mod"}</Read></tool_call>`,
			},
		},
	}
	got := PromptFromResponses(payload)
	want := "assistant: 先查下\nuser: <tool_call><Read>{\"file_path\":\"go.mod\"}</Read></tool_call>"
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestPromptFromChatAssistantSentinelOnlySummarized(t *testing.T) {
	in := ChatRequest{
		Messages: []Message{
			{Role: "assistant", Content: `<<<TC>>>{"tc":[{"id":"call_1","n":"multi_tool_use.parallel","a":{"tool_uses":[{"recipient_name":"functions.shell_command","parameters":{"command":"Get-Date"}}]}}],"c":""}<<<END>>>`},
		},
	}
	got := PromptFromChat(in)
	want := "assistant: assistant_tool_call: multi_tool_use.parallel"
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestPromptFromChatAssistantDoubleAngleSentinelOnlySummarized(t *testing.T) {
	in := ChatRequest{
		Messages: []Message{
			{Role: "assistant", Content: `<<TC>>{"tc":[{"id":"call_1","n":"multi_tool_use.parallel","a":{"tool_uses":[{"recipient_name":"functions.shell_command","parameters":{"command":"Get-Date"}}]}}],"c":""}<<END>>`},
		},
	}
	got := PromptFromChat(in)
	want := "assistant: assistant_tool_call: multi_tool_use.parallel"
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestPromptFromChatAssistantMixedNaturalLanguageAndSentinel(t *testing.T) {
	in := ChatRequest{
		Messages: []Message{
			{Role: "assistant", Content: `checking <<<TC>>>{"tc":[{"n":"Read","a":{"file_path":"go.mod"}}],"c":""}<<<END>>> done`},
		},
	}
	got := PromptFromChat(in)
	want := "assistant: checking   done"
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestPromptFromChatAssistantMixedNaturalLanguageAndDoubleAngleSentinel(t *testing.T) {
	in := ChatRequest{
		Messages: []Message{
			{Role: "assistant", Content: `checking <<TC>>{"tc":[{"n":"Read","a":{"file_path":"go.mod"}}],"c":""}<<END>> done`},
		},
	}
	got := PromptFromChat(in)
	want := "assistant: checking   done"
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestPromptFromChatRepairBranchOutputBackfillSanitized(t *testing.T) {
	repairedOutput := `<tool_call><Read>{"file_path":"go.mod"}</Read></tool_call>`
	parsed := ParseToolSentinel(repairedOutput)
	if len(parsed.ToolCalls) != 1 || parsed.ToolCalls[0].Name != "Read" {
		t.Fatalf("repair parse mismatch: %+v", parsed)
	}

	in := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "step1"},
			{Role: "assistant", Content: repairedOutput},
			{Role: "user", Content: "step2"},
		},
	}
	got := PromptFromChat(in)
	want := "user: step1\nassistant: assistant_tool_call: Read\nuser: step2"
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestNormalizeAssistantHistoryContentIdempotent(t *testing.T) {
	src := `checking <tool_call><Read>{"file_path":"go.mod"}</Read></tool_call> done`
	once := normalizeAssistantHistoryContent(src)
	twice := normalizeAssistantHistoryContent(once)
	if once != twice {
		t.Fatalf("once=%q twice=%q", once, twice)
	}
}

func TestNormalizeAssistantHistoryContentMalformedToolCallStrippedInNormalMode(t *testing.T) {
	t.Setenv("PROXY_LOG_IO", "")
	src := `<tool_call><multi_tool_use.parallel>{"tool_uses":[}</multi_tool_use.parallel></tool_call>`
	got := normalizeAssistantHistoryContent(src)
	want := "assistant_tool_call: multi_tool_use.parallel"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestNormalizeAssistantHistoryContentMalformedToolCallPreservedInDebugMode(t *testing.T) {
	t.Setenv("PROXY_LOG_IO", "1")
	src := `<tool_call><multi_tool_use.parallel>{"tool_uses":[}</multi_tool_use.parallel></tool_call>`
	got := normalizeAssistantHistoryContent(src)
	want := src
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestNormalizeAssistantHistoryContentMalformedToolNameWrapperUsesInnerName(t *testing.T) {
	t.Setenv("PROXY_LOG_IO", "")
	src := `<tool_call><tool_name>Glob</tool_name>{"pattern":"CODEBUDDY.md"}</tool_name></tool_call>`
	got := normalizeAssistantHistoryContent(src)
	want := "assistant_tool_call: Glob"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestChatUsageFromCharCountScalesByRuneMultiplier(t *testing.T) {
	usage := ChatUsageFromCharCount("hi", "回应")
	if usage["prompt_tokens"] != 4 || usage["completion_tokens"] != 4 || usage["total_tokens"] != 8 {
		t.Fatalf("usage=%v", usage)
	}
}

func TestResponsesUsageFromCharCountScalesByRuneMultiplier(t *testing.T) {
	usage := ResponsesUsageFromCharCount("你好", "abc")
	if usage["input_tokens"] != 4 || usage["output_tokens"] != 6 || usage["total_tokens"] != 10 {
		t.Fatalf("usage=%v", usage)
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
	if !strings.Contains(parsed.Prompt, "tc_instruction") {
		t.Fatalf("missing tc_instruction")
	}
	if !strings.Contains(parsed.Prompt, "tc_protocol") || !strings.Contains(parsed.Prompt, "tool_name") {
		t.Fatalf("expected xml tc_protocol: %q", parsed.Prompt)
	}
	if !strings.Contains(parsed.Prompt, "tc_forbid") {
		t.Fatalf("missing tc_forbid")
	}
}

func TestToolSystemPrefixAutoDoesNotForceToolCall(t *testing.T) {
	payload := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "get_weather",
				"parameters": map[string]any{"type": "object"},
			},
		}},
		"tool_choice": "auto",
	}
	parsed := ParseChatRequest(payload)
	if !strings.Contains(parsed.Prompt, "tc_protocol") {
		t.Fatalf("missing tc_protocol")
	}
	if strings.Contains(parsed.Prompt, "必须使用 tc_protocol") {
		t.Fatalf("tool_choice=auto should not force tool call: %q", parsed.Prompt)
	}
}

func TestToolSystemPrefixWithoutChoiceDoesNotForceToolCall(t *testing.T) {
	payload := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "get_weather",
				"parameters": map[string]any{"type": "object"},
			},
		}},
	}
	parsed := ParseChatRequest(payload)
	if !strings.Contains(parsed.Prompt, "tc_protocol") {
		t.Fatalf("missing tc_protocol")
	}
	if strings.Contains(parsed.Prompt, "必须使用 tc_protocol") {
		t.Fatalf("missing tool_choice should not force tool call: %q", parsed.Prompt)
	}
}

func TestLegacyFunctionCallNamedChoiceForcesSpecificTool(t *testing.T) {
	payload := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"functions": []any{
			map[string]any{
				"name":       "get_weather",
				"parameters": map[string]any{"type": "object"},
			},
		},
		"function_call": map[string]any{"name": "get_weather"},
	}
	parsed := ParseChatRequest(payload)
	if !strings.Contains(parsed.Prompt, "get_weather") {
		t.Fatalf("missing named tool choice")
	}
	if !strings.Contains(parsed.Prompt, "exactly one tool call using tc_protocol") {
		t.Fatalf("named function_call should force protocol output: %q", parsed.Prompt)
	}
}

func TestToolSystemPrefixNotInjectedWithoutTools(t *testing.T) {
	payload := map[string]any{
		"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
		"tool_choice": "auto",
	}
	parsed := ParseChatRequest(payload)
	if strings.Contains(parsed.Prompt, "tc_protocol") {
		t.Fatalf("unexpected tc_protocol")
	}
	if parsed.Prompt != "**model = qingyuan**\nuser: hi" {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseChatRequestInjectsModelFast(t *testing.T) {
	payload := map[string]any{
		"model":    "fast",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	parsed := ParseChatRequest(payload)
	if !strings.HasPrefix(parsed.Prompt, "**model = fast**\n") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseChatRequestInjectsModelQingyuan(t *testing.T) {
	payload := map[string]any{
		"model":    "qingyuan",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	parsed := ParseChatRequest(payload)
	if !strings.HasPrefix(parsed.Prompt, "**model = qingyuan**\n") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseChatRequestDefaultsToQingyuan(t *testing.T) {
	payload := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	parsed := ParseChatRequest(payload)
	if parsed.Model != "qingyuan" {
		t.Fatalf("model=%q", parsed.Model)
	}
	if !strings.HasPrefix(parsed.Prompt, "**model = qingyuan**\n") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseChatRequestReplacesExistingMarker(t *testing.T) {
	payload := map[string]any{
		"model":    "deepseek",
		"messages": []any{map[string]any{"role": "user", "content": "**model = fast**\nhello"}},
	}
	parsed := ParseChatRequest(payload)
	if strings.Contains(parsed.Prompt, "**model = fast**") {
		t.Fatalf("old marker still present: %q", parsed.Prompt)
	}
	if !strings.HasPrefix(parsed.Prompt, "**model = deepseek**\n") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseResponsesRequestInjectsModelDeepseek(t *testing.T) {
	payload := map[string]any{
		"model": "deepseek",
		"input": "hi",
	}
	parsed := ParseResponsesRequest(payload)
	if !strings.HasPrefix(parsed.Prompt, "**model = deepseek**\n") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseResponsesRequestInjectsModelQingyuan(t *testing.T) {
	payload := map[string]any{
		"model": "qingyuan",
		"input": "hi",
	}
	parsed := ParseResponsesRequest(payload)
	if !strings.HasPrefix(parsed.Prompt, "**model = qingyuan**\n") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseResponsesRequestDefaultsToQingyuan(t *testing.T) {
	payload := map[string]any{
		"input": "hi",
	}
	parsed := ParseResponsesRequest(payload)
	if parsed.Model != "qingyuan" {
		t.Fatalf("model=%q", parsed.Model)
	}
	if !strings.HasPrefix(parsed.Prompt, "**model = qingyuan**\n") {
		t.Fatalf("prompt=%q", parsed.Prompt)
	}
}

func TestParseResponsesRequestNoInjectionForOtherModel(t *testing.T) {
	payload := map[string]any{
		"model": "agent",
		"input": "hi",
	}
	parsed := ParseResponsesRequest(payload)
	if strings.Contains(parsed.Prompt, "**model =") {
		t.Fatalf("unexpected marker: %q", parsed.Prompt)
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

func TestParseToolSentinelDoubleAngleExtractsContentAndArgs(t *testing.T) {
	text := "<<TC>>{\"tc\":[{\"n\":\"get_weather\",\"a\":{\"location\":\"Paris\",\"unit\":\"c\"}}],\"c\":\"ok\"}<<END>>"
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

func TestParseToolSentinelFallbackNameArguments(t *testing.T) {
	text := "answer\n```json\n{\"name\":\"mcp__skillsServer__listSkills\",\"arguments\":{}}\n```"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.Content != "answer" {
		t.Fatalf("content=%q", res.Content)
	}
	if res.ToolCalls[0].Name != "mcp__skillsServer__listSkills" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	if res.ToolCalls[0].ID == "" {
		t.Fatalf("id empty")
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

func TestParseToolSentinelFallbackRawJSONObjectFunctionCall(t *testing.T) {
	text := "{\"function_call\":{\"name\":\"spawn_agent\",\"arguments\":{\"task\":\"rebuild\"}}}"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "spawn_agent" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	if res.ToolCalls[0].ID == "" {
		t.Fatalf("id empty")
	}
	if !strings.Contains(res.ToolCalls[0].Arguments, "\"task\":\"rebuild\"") {
		t.Fatalf("arguments=%q", res.ToolCalls[0].Arguments)
	}
	if res.Content != "" {
		t.Fatalf("content=%q", res.Content)
	}
}

func TestParseToolSentinelTagWrappedToolCall(t *testing.T) {
	text := "<tool_call><multi_tool_use.parallel>{\"tool_uses\":[{\"recipient_name\":\"functions.shell_command\",\"parameters\":{\"command\":\"Get-Date\"}}]}</multi_tool_use.parallel></tool_call>"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "multi_tool_use.parallel" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	if res.ToolCalls[0].ID == "" {
		t.Fatalf("id empty")
	}
	if !strings.Contains(res.ToolCalls[0].Arguments, "\"recipient_name\":\"functions.shell_command\"") {
		t.Fatalf("arguments=%q", res.ToolCalls[0].Arguments)
	}
	if res.Content != "" {
		t.Fatalf("content=%q", res.Content)
	}
}

func TestParseToolSentinelTagWrappedToolCallJSONArgs(t *testing.T) {
	text := "<tool_call><Read>{\"file_path\":\"go.mod\"}</Read></tool_call>"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "Read" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(res.ToolCalls[0].Arguments), &args); err != nil {
		t.Fatalf("bad args json: %v", err)
	}
	if args["file_path"] != "go.mod" {
		t.Fatalf("args=%v", args)
	}
}

func TestParseToolSentinelTagWrappedToolCallXMLArgs(t *testing.T) {
	text := "<tool_call>\n<Read>\n<file_path>go.mod</file_path>\n</Read>\n</tool_call>"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "Read" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(res.ToolCalls[0].Arguments), &args); err != nil {
		t.Fatalf("bad args json: %v", err)
	}
	if args["file_path"] != "go.mod" {
		t.Fatalf("args=%v", args)
	}
}

func TestParseToolSentinelTagWrappedToolCallToolNameJSONArgs(t *testing.T) {
	text := `<tool_call><tool_name>Glob</tool_name>{"pattern":"*.md"}</tool_call>`
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "Glob" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(res.ToolCalls[0].Arguments), &args); err != nil {
		t.Fatalf("bad args json: %v", err)
	}
	if args["pattern"] != "*.md" {
		t.Fatalf("args=%v", args)
	}
}

func TestParseToolSentinelTagWrappedToolCallCompatibility(t *testing.T) {
	t.Run("self-closing attributes", func(t *testing.T) {
		text := `<tool_call><Glob pattern="*" path="d:/1work/api调用" /></tool_call>`
		res := ParseToolSentinel(text)
		if len(res.ToolCalls) != 1 {
			t.Fatalf("toolcalls=%d", len(res.ToolCalls))
		}
		if res.ToolCalls[0].Name != "Glob" {
			t.Fatalf("name=%q", res.ToolCalls[0].Name)
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(res.ToolCalls[0].Arguments), &args); err != nil {
			t.Fatalf("bad args json: %v", err)
		}
		if args["pattern"] != "*" || args["path"] != "d:/1work/api调用" {
			t.Fatalf("args=%v", args)
		}
	})

	t.Run("paired xml no regression", func(t *testing.T) {
		text := "<tool_call><Read><file_path>go.mod</file_path></Read></tool_call>"
		res := ParseToolSentinel(text)
		if len(res.ToolCalls) != 1 {
			t.Fatalf("toolcalls=%d", len(res.ToolCalls))
		}
		if res.ToolCalls[0].Name != "Read" {
			t.Fatalf("name=%q", res.ToolCalls[0].Name)
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(res.ToolCalls[0].Arguments), &args); err != nil {
			t.Fatalf("bad args json: %v", err)
		}
		if args["file_path"] != "go.mod" {
			t.Fatalf("args=%v", args)
		}
	})

	t.Run("plain text no false positive", func(t *testing.T) {
		text := `normal reply: <Glob pattern="*" path="d:/1work/api调用" />`
		res := ParseToolSentinel(text)
		if len(res.ToolCalls) != 0 {
			t.Fatalf("toolcalls=%d", len(res.ToolCalls))
		}
		if res.Content != text {
			t.Fatalf("content=%q", res.Content)
		}
	})
}

func TestParseToolSentinelPlainTextDoesNotMisclassifyAsToolCall(t *testing.T) {
	text := "normal assistant reply with <tool_call> marker words only"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 0 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.Content != strings.TrimSpace(text) {
		t.Fatalf("content=%q", res.Content)
	}
}

func TestParseToolSentinelTagWrappedToolCallPreservesOuterText(t *testing.T) {
	text := "before <tool_call><multi_tool_use.parallel>{\"tool_uses\":[{\"recipient_name\":\"functions.shell_command\",\"parameters\":{\"command\":\"Get-Date\"}}]}</multi_tool_use.parallel></tool_call> after"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "multi_tool_use.parallel" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	if res.Content != "before after" {
		t.Fatalf("content=%q", res.Content)
	}
}

func TestParseToolSentinelTagWrappedToolCallDoesNotGloballyCompressSpaces(t *testing.T) {
	// We only want to dedupe the *one* redundant space introduced at the join point.
	// The extra spaces after the tag should still be preserved.
	text := "before <tool_call><multi_tool_use.parallel>{\"tool_uses\":[{\"recipient_name\":\"functions.shell_command\",\"parameters\":{\"command\":\"Get-Date\"}}]}</multi_tool_use.parallel></tool_call>   after"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "multi_tool_use.parallel" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	// left contributes 1 trailing space, right contributes 3 leading spaces; we only remove 1 => 3 spaces remain.
	if res.Content != "before   after" {
		t.Fatalf("content=%q", res.Content)
	}
}

func TestParseToolSentinelMalformedTagWrappedToolCallFallsBackToText(t *testing.T) {
	text := "prefix <tool_call><multi_tool_use.parallel>{\"tool_uses\":[}</multi_tool_use.parallel></tool_call> suffix"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 0 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.Content != strings.TrimSpace(text) {
		t.Fatalf("content=%q", res.Content)
	}
}

func TestParseToolSentinelFallbackActionToolInput(t *testing.T) {
	text := "answer\n```json\n{\"action\":\"call_tool\",\"tool\":\"mcp__kqSse__checkLoginStatus\",\"input\":{}}\n```"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 1 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if res.Content != "answer" {
		t.Fatalf("content=%q", res.Content)
	}
	if res.ToolCalls[0].Name != "mcp__kqSse__checkLoginStatus" {
		t.Fatalf("name=%q", res.ToolCalls[0].Name)
	}
	if res.ToolCalls[0].ID == "" {
		t.Fatalf("id empty")
	}
}
