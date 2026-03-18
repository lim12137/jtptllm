package openai

import (
	"encoding/json"
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"
)

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ParsedRequest struct {
	Model    string
	Prompt   string
	Stream   bool
	HasTools bool
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type ToolParseResult struct {
	ToolCalls   []ToolCall
	Content     string
	HasSentinel bool
}

func PromptFromChat(req ChatRequest) string {
	return chatMessagesToPrompt(req.Messages)
}

func PromptFromResponses(payload map[string]any) string {
	instructions, _ := payload["instructions"].(string)
	if _, ok := payload["messages"]; ok {
		if _, hasInput := payload["input"]; !hasInput {
			prompt := chatMessagesToPrompt(asMessages(payload["messages"]))
			return withInstructions(instructions, prompt)
		}
	}

	input := payload["input"]
	prompt := ""
	switch v := input.(type) {
	case string:
		prompt = v
	case []any:
		var lines []string
		for _, item := range v {
			switch iv := item.(type) {
			case string:
				if strings.TrimSpace(iv) != "" {
					lines = append(lines, "user: "+iv)
				}
			case map[string]any:
				role, _ := iv["role"].(string)
				content := coerceContentToStr(iv["content"])
				if strings.TrimSpace(content) != "" {
					if strings.TrimSpace(role) == "" {
						role = "user"
					}
					lines = append(lines, strings.TrimSpace(role)+": "+content)
				}
			}
		}
		prompt = strings.Join(lines, "\n")
	case nil:
		prompt = ""
	default:
		prompt = toString(input)
	}

	return withInstructions(instructions, strings.TrimSpace(prompt))
}

func ParseChatRequest(payload map[string]any) ParsedRequest {
	model := strOr(payload["model"], "agent")
	stream := boolOr(payload["stream"], false)
	prompt := chatMessagesToPrompt(asMessages(payload["messages"]))
	tools := extractTools(payload)
	choice := extractToolChoice(payload)
	prefix := buildToolSystemPrefix(tools, choice)
	prompt = prependSystemPrefix(prefix, prompt)
	return ParsedRequest{Model: model, Prompt: prompt, Stream: stream, HasTools: len(tools) > 0}
}

func ParseResponsesRequest(payload map[string]any) ParsedRequest {
	model := strOr(payload["model"], "agent")
	stream := boolOr(payload["stream"], false)
	prompt := PromptFromResponses(payload)
	tools := extractTools(payload)
	choice := extractToolChoice(payload)
	prefix := buildToolSystemPrefix(tools, choice)
	prompt = prependSystemPrefix(prefix, prompt)
	return ParsedRequest{Model: model, Prompt: prompt, Stream: stream, HasTools: len(tools) > 0}
}

func BuildChatCompletionResponse(text string, model string) map[string]any {
	created := time.Now().Unix()
	cid := newID("chatcmpl")
	return map[string]any{
		"id":      cid,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}
}

func BuildChatCompletionResponseFromText(text string, model string) map[string]any {
	parsed := ParseToolSentinel(text)
	content := strings.TrimSpace(parsed.Content)
	if content == "" && len(parsed.ToolCalls) == 0 {
		content = strings.TrimSpace(text)
	}
	if len(parsed.ToolCalls) == 0 {
		return BuildChatCompletionResponse(content, model)
	}
	created := time.Now().Unix()
	cid := newID("chatcmpl")
	msg := map[string]any{
		"role":       "assistant",
		"content":    content,
		"tool_calls": buildChatToolCalls(parsed.ToolCalls),
	}
	if len(parsed.ToolCalls) == 1 {
		msg["function_call"] = map[string]any{
			"name":      parsed.ToolCalls[0].Name,
			"arguments": parsed.ToolCalls[0].Arguments,
		}
	}
	return map[string]any{
		"id":      cid,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{
			map[string]any{
				"index":         0,
				"message":       msg,
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}
}

func BuildResponsesResponse(text string, model string) map[string]any {
	rid := newID("resp")
	created := time.Now().Unix()
	return map[string]any{
		"id":         rid,
		"object":     "response",
		"created_at": created,
		"model":      model,
		"output": []any{
			map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "output_text", "text": text},
				},
			},
		},
		"output_text": text,
	}
}

func BuildResponsesResponseFromText(text string, model string) map[string]any {
	parsed := ParseToolSentinel(text)
	content := strings.TrimSpace(parsed.Content)
	if content == "" && len(parsed.ToolCalls) == 0 {
		content = strings.TrimSpace(text)
	}
	if len(parsed.ToolCalls) == 0 {
		return BuildResponsesResponse(content, model)
	}
	rid := newID("resp")
	created := time.Now().Unix()
	output := make([]any, 0, len(parsed.ToolCalls))
	for _, call := range parsed.ToolCalls {
		output = append(output, map[string]any{
			"type":      "function_call",
			"call_id":   call.ID,
			"name":      call.Name,
			"arguments": call.Arguments,
		})
	}
	return map[string]any{
		"id":          rid,
		"object":      "response",
		"created_at":  created,
		"model":       model,
		"output":      output,
		"output_text": content,
	}
}

func IterChatCompletionSSE(deltas []string, model string, chatcmplID string) []string {
	created := time.Now().Unix()
	cid := chatcmplID
	if cid == "" {
		cid = newID("chatcmpl")
	}
	var out []string
	first := map[string]any{
		"id":      cid,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil}},
	}
	out = append(out, sseData(first))
	for _, d := range deltas {
		if d == "" {
			continue
		}
		chunk := map[string]any{
			"id":      cid,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": d}, "finish_reason": nil}},
		}
		out = append(out, sseData(chunk))
	}
	final := map[string]any{
		"id":      cid,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
	}
	out = append(out, sseData(final))
	out = append(out, "data: [DONE]\n\n")
	return out
}

func IterResponsesSSE(deltas []string, model string, respID string) []string {
	rid := respID
	if rid == "" {
		rid = newID("resp")
	}
	created := time.Now().Unix()
	var out []string
	createdEvt := map[string]any{"type": "response.created", "response": map[string]any{"id": rid, "model": model, "created_at": created}}
	out = append(out, sseData(createdEvt))
	for _, d := range deltas {
		if d == "" {
			continue
		}
		out = append(out, sseData(map[string]any{"type": "response.output_text.delta", "delta": d, "response_id": rid}))
	}
	out = append(out, sseData(map[string]any{"type": "response.completed", "response_id": rid}))
	out = append(out, "data: [DONE]\n\n")
	return out
}

func DiffDeltas(chunks []string) []string {
	full := ""
	var out []string
	for _, chunk := range chunks {
		if chunk == "" {
			continue
		}
		if full != "" && strings.HasPrefix(chunk, full) {
			delta := chunk[len(full):]
			full = chunk
			if delta != "" {
				out = append(out, delta)
			}
			continue
		}
		delta := chunk
		full = full + chunk
		if delta != "" {
			out = append(out, delta)
		}
	}
	return out
}

func withInstructions(instructions string, prompt string) string {
	ins := strings.TrimSpace(instructions)
	if ins == "" {
		return strings.TrimSpace(prompt)
	}
	if strings.TrimSpace(prompt) == "" {
		return "system: " + ins
	}
	return "system: " + ins + "\n" + strings.TrimSpace(prompt)
}

func chatMessagesToPrompt(messages []Message) string {
	var lines []string
	for _, m := range messages {
		role := strings.TrimSpace(m.Role)
		if role == "" {
			role = "user"
		}
		content := strings.TrimSpace(coerceContentToStr(m.Content))
		if content == "" {
			continue
		}
		lines = append(lines, role+": "+content)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func coerceContentToStr(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		var chunks []string
		for _, part := range v {
			switch pv := part.(type) {
			case string:
				chunks = append(chunks, pv)
			case map[string]any:
				ptype, _ := pv["type"].(string)
				if ptype == "text" || ptype == "input_text" || ptype == "output_text" {
					if t, ok := pv["text"].(string); ok {
						chunks = append(chunks, t)
						continue
					}
					if t, ok := pv["text"].(map[string]any); ok {
						if v, ok := t["value"].(string); ok {
							chunks = append(chunks, v)
						}
					}
				}
			}
		}
		return strings.Join(chunks, "")
	case map[string]any:
		if t, ok := v["text"].(string); ok {
			return t
		}
		if v2, ok := v["value"].(string); ok {
			return v2
		}
	}
	return toString(content)
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		if x == nil {
			return ""
		}
		if s, ok := v.(interface{ String() string }); ok {
			return s.String()
		}
		b, _ := json.Marshal(v)
		if utf8.Valid(b) {
			return string(b)
		}
		return ""
	}
}

func strOr(v any, def string) string {
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	return def
}

func boolOr(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func ParseToolSentinel(text string) ToolParseResult {
	start := strings.Index(text, "<<<TC>>>")
	end := strings.Index(text, "<<<END>>>")
	if start < 0 || end < 0 || end <= start+len("<<<TC>>>") {
		return parseToolCallJSONBlock(text)
	}
	raw := text[start+len("<<<TC>>>") : end]
	var payload struct {
		TC []map[string]any `json:"tc"`
		C  any              `json:"c"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ToolParseResult{Content: strings.TrimSpace(text)}
	}
	content := ""
	if payload.C != nil {
		if s, ok := payload.C.(string); ok {
			content = s
		} else {
			content = toString(payload.C)
		}
	}
	calls := make([]ToolCall, 0, len(payload.TC))
	for _, item := range payload.TC {
		name, _ := item["n"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		id, _ := item["id"].(string)
		if strings.TrimSpace(id) == "" {
			id = newID("call")
		}
		argStr := ""
		if av, ok := item["a"]; ok {
			switch v := av.(type) {
			case string:
				argStr = v
			default:
				if b, err := json.Marshal(v); err == nil {
					argStr = string(b)
				}
			}
		}
		calls = append(calls, ToolCall{ID: id, Name: name, Arguments: argStr})
	}
	return ToolParseResult{ToolCalls: calls, Content: strings.TrimSpace(content), HasSentinel: true}
}

func parseToolCallJSONBlock(text string) ToolParseResult {
	block, stripped, ok := extractFirstJSONBlock(text)
	if !ok {
		return ToolParseResult{Content: strings.TrimSpace(text)}
	}
	var payload any
	if err := json.Unmarshal([]byte(block), &payload); err != nil {
		return ToolParseResult{Content: strings.TrimSpace(text)}
	}
	calls := parseToolCallsFromAny(payload)
	if len(calls) == 0 {
		return ToolParseResult{Content: strings.TrimSpace(text)}
	}
	return ToolParseResult{ToolCalls: calls, Content: strings.TrimSpace(stripped)}
}

func extractFirstJSONBlock(text string) (string, string, bool) {
	lower := strings.ToLower(text)
	start := strings.Index(lower, "```json")
	if start < 0 {
		return "", "", false
	}
	contentStart := start + len("```json")
	end := strings.Index(lower[contentStart:], "```")
	if end < 0 {
		return "", "", false
	}
	block := strings.TrimSpace(text[contentStart : contentStart+end])
	before := strings.TrimSpace(text[:start])
	after := strings.TrimSpace(text[contentStart+end+3:])
	if before == "" {
		return block, after, true
	}
	if after == "" {
		return block, before, true
	}
	return block, before + "\n" + after, true
}

func parseToolCallsFromAny(payload any) []ToolCall {
	m, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	if name, ok := m["name"].(string); ok && strings.TrimSpace(name) != "" {
		id, _ := m["toolCallId"].(string)
		if strings.TrimSpace(id) == "" {
			id = newID("call")
		}
		return []ToolCall{{
			ID:        id,
			Name:      name,
			Arguments: normalizeArgs(m["arguments"]),
		}}
	}
	if action, ok := m["action"].(string); ok && strings.EqualFold(strings.TrimSpace(action), "call_tool") {
		tool, _ := m["tool"].(string)
		if strings.TrimSpace(tool) == "" {
			return nil
		}
		id, _ := m["toolCallId"].(string)
		if strings.TrimSpace(id) == "" {
			id = newID("call")
		}
		return []ToolCall{{
			ID:        id,
			Name:      tool,
			Arguments: normalizeArgs(m["input"]),
		}}
	}
	if name, ok := m["toolName"].(string); ok && strings.TrimSpace(name) != "" {
		id, _ := m["toolCallId"].(string)
		if strings.TrimSpace(id) == "" {
			id = newID("call")
		}
		return []ToolCall{{
			ID:        id,
			Name:      name,
			Arguments: normalizeArgs(m["arguments"]),
		}}
	}
	if calls, ok := m["tool_calls"].([]any); ok {
		out := make([]ToolCall, 0, len(calls))
		for _, item := range calls {
			call, ok := parseToolCallItem(item)
			if ok {
				out = append(out, call)
			}
		}
		return out
	}
	if fc, ok := m["function_call"].(map[string]any); ok {
		name, _ := fc["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil
		}
		return []ToolCall{{
			ID:        newID("call"),
			Name:      name,
			Arguments: normalizeArgs(fc["arguments"]),
		}}
	}
	return nil
}

func parseToolCallItem(item any) (ToolCall, bool) {
	m, ok := item.(map[string]any)
	if !ok {
		return ToolCall{}, false
	}
	fn, ok := m["function"].(map[string]any)
	if !ok {
		return ToolCall{}, false
	}
	name, _ := fn["name"].(string)
	if strings.TrimSpace(name) == "" {
		return ToolCall{}, false
	}
	id, _ := m["id"].(string)
	if strings.TrimSpace(id) == "" {
		id = newID("call")
	}
	return ToolCall{
		ID:        id,
		Name:      name,
		Arguments: normalizeArgs(fn["arguments"]),
	}, true
}

func normalizeArgs(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		if b, err := json.Marshal(t); err == nil {
			return string(b)
		}
		return ""
	}
}

func buildChatToolCalls(calls []ToolCall) []any {
	out := make([]any, 0, len(calls))
	for _, call := range calls {
		out = append(out, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		})
	}
	return out
}

func compressSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	out := map[string]any{}
	if v, ok := schema["type"]; ok {
		out["type"] = v
	}
	if v, ok := schema["required"]; ok {
		out["required"] = v
	}
	if v, ok := schema["enum"]; ok {
		out["enum"] = v
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		outProps := map[string]any{}
		for k, pv := range props {
			if m, ok := pv.(map[string]any); ok {
				outProps[k] = compressSchema(m)
			} else {
				outProps[k] = pv
			}
		}
		out["properties"] = outProps
	}
	if items, ok := schema["items"].(map[string]any); ok {
		out["items"] = compressSchema(items)
	} else if items, ok := schema["items"].([]any); ok {
		outItems := make([]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				outItems = append(outItems, compressSchema(m))
			} else {
				outItems = append(outItems, item)
			}
		}
		out["items"] = outItems
	}
	return out
}

func extractTools(payload map[string]any) []map[string]any {
	var out []map[string]any
	if payload == nil {
		return out
	}
	if tools, ok := payload["tools"].([]any); ok {
		for _, item := range tools {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			fn, _ := m["function"].(map[string]any)
			tool := compressTool(fn)
			if tool != nil {
				out = append(out, tool)
			}
		}
	}
	if fns, ok := payload["functions"].([]any); ok {
		for _, item := range fns {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			tool := compressTool(m)
			if tool != nil {
				out = append(out, tool)
			}
		}
	}
	return out
}

func compressTool(fn map[string]any) map[string]any {
	if fn == nil {
		return nil
	}
	name, _ := fn["name"].(string)
	if strings.TrimSpace(name) == "" {
		return nil
	}
	out := map[string]any{"name": name}
	if desc, ok := fn["description"].(string); ok && strings.TrimSpace(desc) != "" {
		out["description"] = desc
	}
	if params, ok := fn["parameters"].(map[string]any); ok {
		out["parameters"] = compressSchema(params)
	}
	return out
}

func extractToolChoice(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload["tool_choice"]; ok {
		if name := toolChoiceName(v); name != "" {
			return name
		}
	}
	if v, ok := payload["function_call"]; ok {
		if name := toolChoiceName(v); name != "" {
			return name
		}
	}
	return ""
}

func toolChoiceName(v any) string {
	switch tc := v.(type) {
	case string:
		return tc
	case map[string]any:
		if fn, ok := tc["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok {
				return name
			}
		}
		if name, ok := tc["name"].(string); ok {
			return name
		}
	}
	return ""
}

func buildToolSystemPrefix(tools []map[string]any, choice string) string {
	if len(tools) == 0 {
		return ""
	}
	payload := map[string]any{}
	payload["tools"] = tools
	if strings.TrimSpace(choice) != "" {
		payload["tool_choice"] = choice
	}
	payload["tc_protocol"] = "<<<TC>>>{\"tc\":[{\"id\":\"call_1\",\"n\":\"tool_name\",\"a\":{}}],\"c\":\"\"}<<<END>>>"
	if strings.EqualFold(strings.TrimSpace(choice), "none") {
		payload["tc_forbid"] = true
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return "system: " + string(b)
}

func prependSystemPrefix(prefix string, prompt string) string {
	if strings.TrimSpace(prefix) == "" {
		return prompt
	}
	if strings.TrimSpace(prompt) == "" {
		return prefix
	}
	return prefix + "\n" + strings.TrimSpace(prompt)
}

func asMessages(v any) []Message {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []Message
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		msg := Message{}
		if r, ok := m["role"].(string); ok {
			msg.Role = r
		}
		msg.Content = m["content"]
		out = append(out, msg)
	}
	return out
}

func newID(prefix string) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return prefix + "_" + string(b)
}

func sseData(obj map[string]any) string {
	b, _ := json.Marshal(obj)
	return "data: " + string(b) + "\n\n"
}
