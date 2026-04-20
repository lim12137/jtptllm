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

func TestNormalizeAssistantHistoryContentUnclosedToolCallTagStrippedInNormalMode(t *testing.T) {
	t.Setenv("PROXY_LOG_IO", "")
	src := `<tool_call>
  <Bash>
  <command>ls -la</command>
  <description>List all files and directories in the current directory</description>
  </command>
  </Bash>`
	got := normalizeAssistantHistoryContent(src)
	want := "assistant_tool_call: Bash"
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestPromptFromChatNormalizesCodebuddyAgentArtifactToToolSummary(t *testing.T) {
	t.Setenv("PROXY_LOG_IO", "")
	assistant := `I'll analyze the codebase and create a CODEBUDDY.md file. Let me start by exploring the repository structure to understand what's already available.

<tool_call>
<Agent
{
  "description": "Explore repository structure",
  "prompt": "I need to analyze this codebase to understand its structure, architecture, and development workflow. Please explore...",
  "subagent_type": "Explore"
}
</Agent>
</tool_call>`
	in := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "do it"},
			{Role: "assistant", Content: assistant},
		},
	}
	got := PromptFromChat(in)
	want := "user: do it\nassistant: assistant_tool_call: Agent"
	if got != want {
		t.Fatalf("prompt=%q want=%q", got, want)
	}
}

func TestEstimateTextTokensFixtures(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{
			name: "english short sentence",
			text: "Write a concise summary of this API response.",
			want: 12,
		},
		{
			name: "english long sentence",
			text: "Summarize the response headers, status code, and body in a compact table for the incident report.",
			want: 21,
		},
		{
			name: "chinese short sentence",
			text: "请总结这个接口返回的数据结构。",
			want: 14,
		},
		{
			name: "chinese long sentence",
			text: "请根据返回结果，提炼字段含义、错误码差异以及下一步排查建议，并输出给值班同事。",
			want: 31,
		},
		{
			name: "mixed chinese and english",
			text: "请 summarize 这个 API response，并指出 error code 处理方式。",
			want: 22,
		},
		{
			name: "json payload",
			text: "{\"tool\":\"search\",\"args\":{\"query\":\"weather\",\"days\":3,\"city\":\"shanghai\"}}",
			want: 28,
		},
		{
			name: "code snippet",
			text: "if err != nil {\n\treturn fmt.Errorf(\"wrap: %w\", err)\n}\n",
			want: 24,
		},
		{
			name: "xml tool schema",
			text: "<tool_call><search>{\"query\":\"weather tomorrow\"}</search></tool_call>",
			want: 27,
		},
		{
			name: "chat style multiline prompt",
			text: "system: follow the schema strictly\nuser: summarize the payload\nassistant: acknowledged",
			want: 22,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := estimateTextTokens(tc.text)
			if got != tc.want {
				t.Fatalf("estimateTextTokens(%q)=%d want %d", tc.name, got, tc.want)
			}
		})
	}
}

func TestEstimateChatPromptTokensAddsRoleOverhead(t *testing.T) {
	prompt := "system: follow the schema strictly\nuser: hi"
	base := estimateTextTokens(prompt)
	got := estimateChatPromptTokens(prompt)
	if got <= base {
		t.Fatalf("chat prompt estimate=%d base=%d", got, base)
	}
}

func TestEstimateChatPromptTokensAddsToolPrefixOverhead(t *testing.T) {
	prompt := "system: You have tools available.\n<tool_call>{\"name\":\"search\"}</tool_call>\nuser: hi"
	plain := estimateChatPromptTokens("user: hi")
	got := estimateChatPromptTokens(prompt)
	if got <= plain {
		t.Fatalf("tool prompt estimate=%d plain=%d", got, plain)
	}
}

func TestChatUsageFromCharCountUsesHeuristicEstimator(t *testing.T) {
	usage := ChatUsageFromCharCount("system: follow the schema strictly\nuser: hi", "已整理完成。")
	if usage["prompt_tokens"] != 15 || usage["completion_tokens"] != 7 || usage["total_tokens"] != 22 {
		t.Fatalf("usage=%v", usage)
	}
}

func TestResponsesUsageFromCharCountUsesHeuristicEstimator(t *testing.T) {
	usage := ResponsesUsageFromCharCount("{\"tool\":\"search\",\"args\":{\"query\":\"weather\",\"days\":3}}", "Found 3 weather updates for tomorrow.")
	if usage["input_tokens"] != 20 || usage["output_tokens"] != 9 || usage["total_tokens"] != 29 {
		t.Fatalf("usage=%v", usage)
	}
}

func TestResponsesUsageFromCharCountHandlesEmptyStrings(t *testing.T) {
	usage := ResponsesUsageFromCharCount("", "")
	if usage["input_tokens"] != 0 || usage["output_tokens"] != 0 || usage["total_tokens"] != 0 {
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

func TestParseToolSentinelTagWrappedToolCallMalformedDelimitersCompatibility(t *testing.T) {
	t.Run("extra closing tool_name", func(t *testing.T) {
		text := `<tool_call><tool_name>Glob</tool_name>{"pattern":"*.md"}</tool_name></tool_call>`
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
	})

	t.Run("missing closing tool_name", func(t *testing.T) {
		text := `<tool_call><tool_name>Glob{"pattern":"*.md"}</tool_call>`
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
	})

	t.Run("extra closing tool_call", func(t *testing.T) {
		text := `<tool_call><Read>{"file_path":"go.mod"}</Read></tool_call></tool_call>`
		res := ParseToolSentinel(text)
		if len(res.ToolCalls) != 1 {
			t.Fatalf("toolcalls=%d", len(res.ToolCalls))
		}
		if res.ToolCalls[0].Name != "Read" {
			t.Fatalf("name=%q", res.ToolCalls[0].Name)
		}
		if strings.TrimSpace(res.Content) != "" {
			t.Fatalf("content=%q", res.Content)
		}
	})

	t.Run("missing closing tool_call", func(t *testing.T) {
		text := `<tool_call><Read>{"file_path":"go.mod"}</Read>`
		res := ParseToolSentinel(text)
		if len(res.ToolCalls) != 1 {
			t.Fatalf("toolcalls=%d", len(res.ToolCalls))
		}
		if res.ToolCalls[0].Name != "Read" {
			t.Fatalf("name=%q", res.ToolCalls[0].Name)
		}
	})

	t.Run("mismatched inner closing tag", func(t *testing.T) {
		text := `<tool_call><Read>{"file_path":"go.mod"}</tool_name></tool_call>`
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

	t.Run("tool_call open tag with attributes or spaces", func(t *testing.T) {
		text := `<tool_call data-kind="fn"   ><Read>{"file_path":"go.mod"}</Read></tool_call>`
		res := ParseToolSentinel(text)
		if len(res.ToolCalls) != 1 {
			t.Fatalf("toolcalls=%d", len(res.ToolCalls))
		}
		if res.ToolCalls[0].Name != "Read" {
			t.Fatalf("name=%q", res.ToolCalls[0].Name)
		}
	})

	t.Run("xml args extra closing subtag", func(t *testing.T) {
		text := `<tool_call><Read><file_path>go.mod</file_path></file_path></Read></tool_call>`
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

	t.Run("xml args missing closing subtag", func(t *testing.T) {
		text := `<tool_call><Read><file_path>go.mod</Read></tool_call>`
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

	t.Run("xml args mismatched closing subtag", func(t *testing.T) {
		text := `<tool_call><Read><file_path>go.mod</pattern></Read></tool_call>`
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

	t.Run("xml args command description mismatched close", func(t *testing.T) {
		text := `<tool_call><Bash><command>ls -la</command><description>List all files and directories in the current directory</command></Bash></tool_call>`
		res := ParseToolSentinel(text)
		if len(res.ToolCalls) != 1 {
			t.Fatalf("toolcalls=%d", len(res.ToolCalls))
		}
		if res.ToolCalls[0].Name != "Bash" {
			t.Fatalf("name=%q", res.ToolCalls[0].Name)
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(res.ToolCalls[0].Arguments), &args); err != nil {
			t.Fatalf("bad args json: %v", err)
		}
		if args["command"] != "ls -la" {
			t.Fatalf("args=%v", args)
		}
		if args["description"] != "List all files and directories in the current directory" {
			t.Fatalf("args=%v", args)
		}
	})
}

func TestParseToolSentinelSingleMalformedTaggedArtifactNeedsRetry(t *testing.T) {
	text := "前置说明 <tool_call>\n<Agent\n{\n  \"description\": \"Explore repository structure\"\n}\n</Agent>\n</tool_call> 后置说明"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 0 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if !res.NeedsRetry {
		t.Fatalf("needsRetry=false")
	}
	if strings.Contains(res.Content, "<tool_call>") {
		t.Fatalf("content leaked tool_call tag: %q", res.Content)
	}
	if !strings.Contains(res.Content, "前置说明") || !strings.Contains(res.Content, "后置说明") {
		t.Fatalf("content=%q", res.Content)
	}
}

func TestBuildChatCompletionResponseFromTextSingleMalformedTaggedArtifactNoLeak(t *testing.T) {
	text := "前置说明 <tool_call>\n<Agent\n{\n  \"description\": \"Explore repository structure\"\n}\n</Agent>\n</tool_call> 后置说明"
	out := BuildChatCompletionResponseFromText(text, "agent")
	choices, ok := out["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("choices empty")
	}
	msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
	content, _ := msg["content"].(string)
	if strings.Contains(content, "<tool_call>") {
		t.Fatalf("content leaked malformed tool_call: %q", content)
	}
	if content == "" {
		t.Fatalf("content should keep non-tool text")
	}
}

func TestBuildResponsesResponseFromTextSingleMalformedTaggedArtifactNoLeak(t *testing.T) {
	text := "前置说明 <tool_call>\n<Agent\n{\n  \"description\": \"Explore repository structure\"\n}\n</Agent>\n</tool_call> 后置说明"
	out := BuildResponsesResponseFromText(text, "agent")
	content, _ := out["output_text"].(string)
	if strings.Contains(content, "<tool_call>") {
		t.Fatalf("output_text leaked malformed tool_call: %q", content)
	}
	if content == "" {
		t.Fatalf("output_text should keep non-tool text")
	}
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
	if strings.Contains(res.Content, "<tool_call>") {
		t.Fatalf("content leaked tool_call tag: %q", res.Content)
	}
	if !strings.Contains(res.Content, "prefix") || !strings.Contains(res.Content, "suffix") {
		t.Fatalf("content=%q", res.Content)
	}
	if !res.NeedsRetry {
		t.Fatalf("needsRetry=false")
	}
}

func TestParseToolSentinelMalformedTagWrappedToolCallInvalidatesMixedCandidates(t *testing.T) {
	text := "prefix <tool_call><multi_tool_use.parallel>{\"tool_uses\":[}</multi_tool_use.parallel></tool_call> mid <tool_call><Read>{\"file_path\":\"go.mod\"}</Read></tool_call> suffix"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 0 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if strings.Contains(res.Content, "<tool_call>") {
		t.Fatalf("content leaked tool_call tag: %q", res.Content)
	}
	if !strings.Contains(res.Content, "prefix") || !strings.Contains(res.Content, "mid") || !strings.Contains(res.Content, "suffix") {
		t.Fatalf("content=%q", res.Content)
	}
	if !res.NeedsRetry {
		t.Fatalf("needsRetry=false")
	}
}

func TestParseToolSentinelMalformedTagWrappedToolCallInvalidatesEvenWhenFirstIsValid(t *testing.T) {
	text := "start <tool_call><Read>{\"file_path\":\"go.mod\"}</Read></tool_call> then <tool_call><multi_tool_use.parallel>{\"tool_uses\":[}</multi_tool_use.parallel></tool_call> end"
	res := ParseToolSentinel(text)
	if len(res.ToolCalls) != 0 {
		t.Fatalf("toolcalls=%d", len(res.ToolCalls))
	}
	if strings.Contains(res.Content, "<tool_call>") {
		t.Fatalf("content leaked tool_call tag: %q", res.Content)
	}
	if !strings.Contains(res.Content, "start") || !strings.Contains(res.Content, "then") || !strings.Contains(res.Content, "end") {
		t.Fatalf("content=%q", res.Content)
	}
	if !res.NeedsRetry {
		t.Fatalf("needsRetry=false")
	}
}

func TestAppendHiddenToolCallRetryPrompt(t *testing.T) {
	base := "user: hi\nassistant: bad tool call"
	got := AppendHiddenToolCallRetryPrompt(base)
	if !strings.Contains(got, "本次 tool-call 格式不完整，已忽略，请重试，并且一次只调用一个 tool-call。") {
		t.Fatalf("missing retry prompt: %q", got)
	}
	if !strings.Contains(got, "system: ") {
		t.Fatalf("missing system prefix: %q", got)
	}
	got2 := AppendHiddenToolCallRetryPrompt(got)
	if got2 != got {
		t.Fatalf("prompt should be idempotent, got=%q got2=%q", got, got2)
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
