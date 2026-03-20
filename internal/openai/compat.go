package openai

import (
	"encoding/json"
	"math/rand"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var modelMarkerRe = regexp.MustCompile(`\*\*model\s*=\s*[^*]+\*\*`)

const DefaultModel = "qingyuan"

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

func intFromAny(v any) (int, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	case float32:
		return int(t), true
	default:
		return 0, false
	}
}

// NormalizeResponsesUsage converts upstream usage into Responses usage keys:
//
//	{input_tokens, output_tokens, total_tokens}
//
// Returns nil if no recognizable usage fields exist.
func NormalizeResponsesUsage(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	// Some callers may pass an object that contains {usage:{...}}.
	if u, ok := raw["usage"].(map[string]any); ok {
		raw = u
	}

	if in, okIn := intFromAny(raw["input_tokens"]); okIn {
		out, okOut := intFromAny(raw["output_tokens"])
		tot, okTot := intFromAny(raw["total_tokens"])
		if !okOut {
			out = 0
		}
		if !okTot {
			tot = in + out
		}
		return map[string]any{
			"input_tokens":  in,
			"output_tokens": out,
			"total_tokens":  tot,
		}
	}
	if out, okOut := intFromAny(raw["output_tokens"]); okOut {
		in, okIn := intFromAny(raw["input_tokens"])
		tot, okTot := intFromAny(raw["total_tokens"])
		if !okIn {
			in = 0
		}
		if !okTot {
			tot = in + out
		}
		return map[string]any{
			"input_tokens":  in,
			"output_tokens": out,
			"total_tokens":  tot,
		}
	}

	// Chat usage key mapping.
	p, okP := intFromAny(raw["prompt_tokens"])
	c, okC := intFromAny(raw["completion_tokens"])
	t, okT := intFromAny(raw["total_tokens"])
	if okP || okC || okT {
		if !okP {
			p = 0
		}
		if !okC {
			c = 0
		}
		if !okT {
			t = p + c
		}
		return map[string]any{
			"input_tokens":  p,
			"output_tokens": c,
			"total_tokens":  t,
		}
	}
	return nil
}

// NormalizeChatUsage converts upstream usage into Chat Completions usage keys:
//
//	{prompt_tokens, completion_tokens, total_tokens}
//
// Returns nil if no recognizable usage fields exist.
func NormalizeChatUsage(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	if u, ok := raw["usage"].(map[string]any); ok {
		raw = u
	}

	if p, okP := intFromAny(raw["prompt_tokens"]); okP {
		c, okC := intFromAny(raw["completion_tokens"])
		t, okT := intFromAny(raw["total_tokens"])
		if !okC {
			c = 0
		}
		if !okT {
			t = p + c
		}
		return map[string]any{
			"prompt_tokens":     p,
			"completion_tokens": c,
			"total_tokens":      t,
		}
	}
	if c, okC := intFromAny(raw["completion_tokens"]); okC {
		p, okP := intFromAny(raw["prompt_tokens"])
		t, okT := intFromAny(raw["total_tokens"])
		if !okP {
			p = 0
		}
		if !okT {
			t = p + c
		}
		return map[string]any{
			"prompt_tokens":     p,
			"completion_tokens": c,
			"total_tokens":      t,
		}
	}

	// Responses usage key mapping.
	in, okIn := intFromAny(raw["input_tokens"])
	out, okOut := intFromAny(raw["output_tokens"])
	t, okT := intFromAny(raw["total_tokens"])
	if okIn || okOut || okT {
		if !okIn {
			in = 0
		}
		if !okOut {
			out = 0
		}
		if !okT {
			t = in + out
		}
		return map[string]any{
			"prompt_tokens":     in,
			"completion_tokens": out,
			"total_tokens":      t,
		}
	}
	return nil
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
		// Some clients send Responses "input" as a top-level content-part list:
		//   input: [{type:"input_text", text:"..."}]
		// Treat that as a single user message.
		allParts := len(v) > 0
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				allParts = false
				break
			}
			if _, has := m["role"]; has {
				allParts = false
				break
			}
			if _, has := m["content"]; has {
				allParts = false
				break
			}
			if _, has := m["type"]; !has {
				allParts = false
				break
			}
		}
		if allParts {
			text := strings.TrimSpace(coerceContentToStr(v))
			if text != "" {
				prompt = "user: " + text
			}
			break
		}

		var lines []string
		for _, item := range v {
			switch iv := item.(type) {
			case string:
				if strings.TrimSpace(iv) != "" {
					lines = append(lines, "user: "+iv)
				}
			case map[string]any:
				role, _ := iv["role"].(string)
				contentSrc := any(iv)
				if c, ok := iv["content"]; ok {
					contentSrc = c
				}
				content := coerceContentToStr(contentSrc)
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
	model := strOr(payload["model"], DefaultModel)
	stream := boolOr(payload["stream"], false)
	prompt := chatMessagesToPrompt(asMessages(payload["messages"]))
	tools := extractTools(payload)
	choice := extractToolChoice(payload)
	prefix := buildToolSystemPrefix(tools, choice)
	prompt = injectModelMarker(model, prompt)
	prompt = prependSystemPrefix(prefix, prompt)
	return ParsedRequest{Model: model, Prompt: prompt, Stream: stream, HasTools: len(tools) > 0}
}

func ParseResponsesRequest(payload map[string]any) ParsedRequest {
	model := strOr(payload["model"], DefaultModel)
	stream := boolOr(payload["stream"], false)
	prompt := PromptFromResponses(payload)
	tools := extractTools(payload)
	choice := extractToolChoice(payload)
	prefix := buildToolSystemPrefix(tools, choice)
	prompt = injectModelMarker(model, prompt)
	prompt = prependSystemPrefix(prefix, prompt)
	return ParsedRequest{Model: model, Prompt: prompt, Stream: stream, HasTools: len(tools) > 0}
}

func injectModelMarker(model string, prompt string) string {
	m := strings.TrimSpace(model)
	if m != "fast" && m != "deepseek" && m != "qingyuan" {
		return prompt
	}
	if strings.TrimSpace(prompt) == "" {
		return prompt
	}
	cleaned := modelMarkerRe.ReplaceAllString(prompt, "")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "**model = " + m + "**"
	}
	return "**model = " + m + "**\n" + cleaned
}

// ChatUsageFromCharCount is a lightweight fallback when upstream usage is missing.
// It intentionally counts Unicode code points (runes) rather than bytes.
func ChatUsageFromCharCount(prompt string, completion string) map[string]any {
	p := utf8.RuneCountInString(prompt)
	c := utf8.RuneCountInString(completion)
	return map[string]any{
		"prompt_tokens":     p,
		"completion_tokens": c,
		"total_tokens":      p + c,
	}
}

// ResponsesUsageFromCharCount is a lightweight fallback when upstream usage is missing.
// It intentionally counts Unicode code points (runes) rather than bytes.
func ResponsesUsageFromCharCount(input string, output string) map[string]any {
	in := utf8.RuneCountInString(input)
	out := utf8.RuneCountInString(output)
	return map[string]any{
		"input_tokens":  in,
		"output_tokens": out,
		"total_tokens":  in + out,
	}
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
		"usage":      map[string]any{},
		"output": []any{
			map[string]any{
				"id":   newID("msg"),
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
		"usage":       map[string]any{},
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
	seq := 0

	baseResp := map[string]any{
		"id":         rid,
		"object":     "response",
		"created_at": created,
		"model":      model,
		"status":     "in_progress",
		"output":     []any{},
		"usage":      nil, // created/in_progress usage must be null
	}

	out = append(out, sseEvent("response.created", map[string]any{
		"sequence_number": seq,
		"response":        baseResp,
	}))
	seq++

	out = append(out, sseEvent("response.in_progress", map[string]any{
		"sequence_number": seq,
		"response":        baseResp,
	}))
	seq++

	var full strings.Builder
	for _, d := range deltas {
		if d == "" {
			continue
		}
		full.WriteString(d)
	}

	text := full.String()
	parsed := ParseToolSentinel(text)
	var outputs []any
	if len(parsed.ToolCalls) > 0 {
		for outputIndex, call := range parsed.ToolCalls {
			itemID := newID("fc")
			callID := call.ID
			if strings.TrimSpace(callID) == "" {
				callID = newID("call")
			}
			addedItem := map[string]any{
				"id":        itemID,
				"type":      "function_call",
				"status":    "in_progress",
				"call_id":   callID,
				"name":      call.Name,
				"arguments": "",
			}
			out = append(out, sseEvent("response.output_item.added", map[string]any{
				"sequence_number": seq,
				"response_id":     rid,
				"output_index":    outputIndex,
				"item":            addedItem,
			}))
			seq++

			out = append(out, sseEvent("response.function_call_arguments.delta", map[string]any{
				"sequence_number": seq,
				"response_id":     rid,
				"item_id":         itemID,
				"output_index":    outputIndex,
				"call_id":         callID,
				"delta":           call.Arguments,
			}))
			seq++

			out = append(out, sseEvent("response.function_call_arguments.done", map[string]any{
				"sequence_number": seq,
				"response_id":     rid,
				"item_id":         itemID,
				"output_index":    outputIndex,
				"call_id":         callID,
				"name":            call.Name,
				"arguments":       call.Arguments,
			}))
			seq++

			doneItem := map[string]any{
				"id":        itemID,
				"type":      "function_call",
				"status":    "completed",
				"call_id":   callID,
				"name":      call.Name,
				"arguments": call.Arguments,
			}
			out = append(out, sseEvent("response.output_item.done", map[string]any{
				"sequence_number": seq,
				"response_id":     rid,
				"item_id":         itemID,
				"output_index":    outputIndex,
				"item":            doneItem,
			}))
			seq++
			outputs = append(outputs, map[string]any{
				"id":        itemID,
				"type":      "function_call",
				"status":    "completed",
				"call_id":   callID,
				"name":      call.Name,
				"arguments": call.Arguments,
			})
		}
	} else {
		itemID := newID("msg")
		outputIndex := 0
		contentIndex := 0
		item := map[string]any{
			"id":      itemID,
			"type":    "message",
			"role":    "assistant",
			"status":  "in_progress",
			"content": []any{}, // official example uses empty content for output_item.added
		}
		out = append(out, sseEvent("response.output_item.added", map[string]any{
			"sequence_number": seq,
			"response_id":     rid,
			"output_index":    outputIndex,
			"item":            item,
		}))
		seq++

		out = append(out, sseEvent("response.content_part.added", map[string]any{
			"sequence_number": seq,
			"response_id":     rid,
			"item_id":         itemID,
			"output_index":    outputIndex,
			"content_index":   contentIndex,
			"part":            map[string]any{"type": "output_text", "text": ""},
		}))
		seq++

		if text != "" {
			out = append(out, sseEvent("response.output_text.delta", map[string]any{
				"sequence_number": seq,
				"response_id":     rid,
				"item_id":         itemID,
				"output_index":    outputIndex,
				"content_index":   contentIndex,
				"delta":           text,
			}))
			seq++
		}
		out = append(out, sseEvent("response.output_text.done", map[string]any{
			"sequence_number": seq,
			"response_id":     rid,
			"item_id":         itemID,
			"output_index":    outputIndex,
			"content_index":   contentIndex,
			"text":            text,
		}))
		seq++

		out = append(out, sseEvent("response.content_part.done", map[string]any{
			"sequence_number": seq,
			"response_id":     rid,
			"item_id":         itemID,
			"output_index":    outputIndex,
			"content_index":   contentIndex,
			"part":            map[string]any{"type": "output_text", "text": text},
		}))
		seq++

		item["status"] = "completed"
		item["content"] = []any{map[string]any{"type": "output_text", "text": text}}
		out = append(out, sseEvent("response.output_item.done", map[string]any{
			"sequence_number": seq,
			"response_id":     rid,
			"item_id":         itemID,
			"output_index":    outputIndex,
			"item":            item,
		}))
		seq++
		outputs = append(outputs, item)
	}

	finalResp := map[string]any{
		"id":          rid,
		"object":      "response",
		"created_at":  created,
		"model":       model,
		"status":      "completed",
		"output":      outputs,
		"output_text": parsed.Content,
		"usage": map[string]any{
			"input_tokens":  0,
			"output_tokens": 0,
			"total_tokens":  0,
		},
	}
	out = append(out, sseEvent("response.completed", map[string]any{
		"sequence_number": seq,
		"response":        finalResp,
	}))
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
		res := parseToolCallJSONBlock(text)
		if len(res.ToolCalls) > 0 {
			return res
		}
		res = parseToolCallTaggedBlock(text)
		if len(res.ToolCalls) > 0 {
			return res
		}
		return parseToolCallRawJSON(text)
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

func parseToolCallRawJSON(text string) ToolParseResult {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ToolParseResult{Content: ""}
	}
	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return ToolParseResult{Content: trimmed}
	}
	calls := parseToolCallsFromAny(payload)
	if len(calls) == 0 {
		return ToolParseResult{Content: trimmed}
	}
	return ToolParseResult{ToolCalls: calls, Content: ""}
}

func parseToolCallTaggedBlock(text string) ToolParseResult {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ToolParseResult{Content: ""}
	}
	const openTag = "<tool_call>"
	const closeTag = "</tool_call>"
	start := strings.Index(trimmed, openTag)
	if start < 0 {
		return ToolParseResult{Content: trimmed}
	}
	end := strings.Index(trimmed[start+len(openTag):], closeTag)
	if end < 0 {
		return ToolParseResult{Content: trimmed}
	}
	innerStart := start + len(openTag)
	innerEnd := innerStart + end
	inner := strings.TrimSpace(trimmed[innerStart:innerEnd])
	if !strings.HasPrefix(inner, "<") {
		return ToolParseResult{Content: trimmed}
	}
	nameEnd := strings.Index(inner, ">")
	if nameEnd <= 1 {
		return ToolParseResult{Content: trimmed}
	}
	name := strings.TrimSpace(inner[1:nameEnd])
	if name == "" || strings.ContainsAny(name, " \t\r\n/") {
		return ToolParseResult{Content: trimmed}
	}
	closeInnerTag := "</" + name + ">"
	closeInnerIdx := strings.LastIndex(inner, closeInnerTag)
	if closeInnerIdx < 0 {
		return ToolParseResult{Content: trimmed}
	}
	argText := strings.TrimSpace(inner[nameEnd+1 : closeInnerIdx])
	if argText == "" {
		return ToolParseResult{Content: trimmed}
	}
	var payload any
	if err := json.Unmarshal([]byte(argText), &payload); err != nil {
		return ToolParseResult{Content: trimmed}
	}
	argBytes, err := json.Marshal(payload)
	if err != nil {
		return ToolParseResult{Content: trimmed}
	}
	content := strings.TrimSpace(trimmed[:start] + trimmed[innerEnd+len(closeTag):])
	return ToolParseResult{
		ToolCalls: []ToolCall{{
			ID:        newID("call"),
			Name:      name,
			Arguments: string(argBytes),
		}},
		Content: content,
	}
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
	trimmedChoice := strings.TrimSpace(choice)
	payload := map[string]any{}
	payload["tools"] = tools
	if trimmedChoice != "" {
		payload["tool_choice"] = choice
	}
	payload["tc_protocol"] = "<<<TC>>>{\"tc\":[{\"id\":\"call_1\",\"n\":\"tool_name\",\"a\":{}}],\"c\":\"\"}<<<END>>>"
	switch {
	case trimmedChoice == "", strings.EqualFold(trimmedChoice, "auto"):
		payload["tc_instruction"] = "如需调用工具，使用 tc_protocol 格式输出；若无需调用工具，直接输出自然语言。"
	case strings.EqualFold(trimmedChoice, "none"):
		payload["tc_instruction"] = "不要调用工具；直接输出自然语言。"
		payload["tc_forbid"] = true
	default:
		payload["tc_instruction"] = "必须使用 tc_protocol 格式输出工具调用；不要输出自然语言；c 可为空。"
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

// sseEvent emits a typed Responses SSE event in the official example format:
//
//	event: <name>
//	data:  <json with {type:<name>, ...}>
func sseEvent(event string, obj map[string]any) string {
	if obj == nil {
		obj = map[string]any{}
	}
	// Enforce data.type == event for strict client/SDK compatibility.
	obj["type"] = event
	b, _ := json.Marshal(obj)
	return "event: " + event + "\n" + "data: " + string(b) + "\n\n"
}
