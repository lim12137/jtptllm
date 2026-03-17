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
	Model  string
	Prompt string
	Stream bool
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
	return ParsedRequest{Model: model, Prompt: prompt, Stream: stream}
}

func ParseResponsesRequest(payload map[string]any) ParsedRequest {
	model := strOr(payload["model"], "agent")
	stream := boolOr(payload["stream"], false)
	prompt := PromptFromResponses(payload)
	return ParsedRequest{Model: model, Prompt: prompt, Stream: stream}
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
