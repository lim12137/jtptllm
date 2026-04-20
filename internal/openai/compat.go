package openai

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	modelMarkerRe         = regexp.MustCompile(`\*\*model\s*=\s*[^*]+\*\*`)
	thinkingBlockRe       = regexp.MustCompile(`(?is)<thinking\b[^>]*>.*?</thinking>`)
	thinkingSelfClosingRe = regexp.MustCompile(`(?is)<thinking\b[^>]*/>`)
	toolCallTagBlockRe    = regexp.MustCompile(`(?is)<tool_call\b[^>]*>.*?</tool_call>`)
	toolCallOpenTagRe     = regexp.MustCompile(`(?is)<tool_call\b[^>]*>`)
	tcSentinelBlockRe     = regexp.MustCompile(`(?is)(?:<<<TC>>>|<<TC>>).*?(?:<<<END>>>|<<END>>)`)
	taggedToolNameRe      = regexp.MustCompile(`(?is)<tool_call\b[^>]*>\s*<([A-Za-z0-9_.:-]+)\b`)
	toolNameValueRe       = regexp.MustCompile(`(?is)<tool_name>\s*([^<]+?)\s*</tool_name>`)
	jsonToolNRe           = regexp.MustCompile(`(?is)"n"\s*:\s*"([^"]+)"`)
	jsonToolNameRe        = regexp.MustCompile(`(?is)"name"\s*:\s*"([^"]+)"`)
	jsonToolKeyRe         = regexp.MustCompile(`(?is)"tool(?:Name)?"\s*:\s*"([^"]+)"`)
)

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
				if line := messageToPromptLine("user", iv); line != "" {
					lines = append(lines, line)
				}
			case map[string]any:
				role, _ := iv["role"].(string)
				contentSrc := any(iv)
				if c, ok := iv["content"]; ok {
					contentSrc = c
				}
				content := coerceContentToStr(contentSrc)
				if line := messageToPromptLine(role, content); line != "" {
					lines = append(lines, line)
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

type tokenEstimateStats struct {
	cjkCount            int
	asciiLetterCount    int
	nonASCIILetterCount int
	digitCount          int
	whitespaceCount     int
	punctCount          int
	jsonSyntaxCount     int
	xmlSyntaxCount      int
	newlineCount        int
	wordCount           int
	longWordCount       int
	roleLineCount       int
	modelMarkerCount    int
	toolPrefixSignals   int
}

const (
	minNonEmptyTokenEstimate = 1
	cjkTokenWeight           = 1
	asciiWordWeight          = 1
	asciiLongWordExtra       = 2
	digitTokenWeight         = 1
	punctTokenWeight         = 1
	structureTokenWeight     = 2
	newlineTokenWeight       = 2
	shortTextBaseWeight      = 2
	roleLineOverheadWeight   = 1
	modelMarkerOverhead      = 1
	toolPrefixOverhead       = 2

	longWordThreshold       = 7
	shortProseWordThreshold = 8
	shortCJKThreshold       = 6
	cjkCompressionThreshold = 16
	cjkCompressionDivisor   = 4
	mixedScriptOverhead     = 4
	structuredWordThreshold = 8
	standalonePunctDivisor  = 2
	standalonePunctBase     = 2
)

func collectTokenEstimateStats(text string) tokenEstimateStats {
	var stats tokenEstimateStats
	if text == "" {
		return stats
	}

	inWord := false
	currentWordLen := 0
	finalizeWord := func() {
		if !inWord {
			return
		}
		stats.wordCount++
		if currentWordLen >= longWordThreshold {
			stats.longWordCount++
		}
		inWord = false
		currentWordLen = 0
	}

	for _, r := range text {
		switch r {
		case '\n':
			finalizeWord()
			stats.newlineCount++
			stats.whitespaceCount++
			continue
		case '\r', '\t', ' ':
			finalizeWord()
			stats.whitespaceCount++
			continue
		}

		if isJSONSyntaxRune(r) {
			finalizeWord()
			stats.jsonSyntaxCount++
			continue
		}
		if isXMLSyntaxRune(r) {
			finalizeWord()
			stats.xmlSyntaxCount++
			continue
		}
		if isCJKTokenRune(r) {
			finalizeWord()
			stats.cjkCount++
			continue
		}
		if r >= '0' && r <= '9' {
			finalizeWord()
			stats.digitCount++
			continue
		}
		if isTokenWordRune(r) {
			inWord = true
			currentWordLen++
			if r < utf8.RuneSelf {
				stats.asciiLetterCount++
			} else {
				stats.nonASCIILetterCount++
			}
			continue
		}
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			finalizeWord()
			stats.punctCount++
			continue
		}

		finalizeWord()
	}
	finalizeWord()

	lowerText := strings.ToLower(text)
	for _, line := range strings.Split(lowerText, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "system:"),
			strings.HasPrefix(trimmed, "user:"),
			strings.HasPrefix(trimmed, "assistant:"),
			strings.HasPrefix(trimmed, "tool:"):
			stats.roleLineCount++
		}
		if strings.HasPrefix(trimmed, "**model = ") {
			stats.modelMarkerCount++
		}
	}

	for _, needle := range []string{
		"<tool_call>",
		"</tool_call>",
		"<tool_name>",
		"tc_protocol",
		"tool_choice",
		"\"tools\"",
		"assistant_tool_call:",
	} {
		if strings.Contains(lowerText, needle) {
			stats.toolPrefixSignals++
		}
	}

	return stats
}

func isTokenWordRune(r rune) bool {
	if r == '_' {
		return true
	}
	if isCJKTokenRune(r) {
		return false
	}
	return unicode.IsLetter(r)
}

func isCJKTokenRune(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

func isJSONSyntaxRune(r rune) bool {
	switch r {
	case '{', '}', '[', ']', '"', ':', ',', '\\':
		return true
	default:
		return false
	}
}

func isXMLSyntaxRune(r rune) bool {
	switch r {
	case '<', '>', '/', '=':
		return true
	default:
		return false
	}
}

func estimateTextTokens(text string) int {
	if text == "" {
		return 0
	}

	stats := collectTokenEstimateStats(text)
	score := 0
	structureCount := stats.jsonSyntaxCount + stats.xmlSyntaxCount
	jsonHeavy := stats.jsonSyntaxCount >= 10
	xmlHeavy := stats.xmlSyntaxCount >= 8
	codeLike := stats.newlineCount > 0 && structureCount >= 6
	structuredText := jsonHeavy || xmlHeavy || stats.toolPrefixSignals > 0

	if stats.cjkCount > 0 {
		cjkScore := stats.cjkCount * cjkTokenWeight
		if stats.cjkCount > cjkCompressionThreshold {
			cjkScore -= (stats.cjkCount - cjkCompressionThreshold) / cjkCompressionDivisor
		}
		if stats.cjkCount <= shortCJKThreshold {
			cjkScore += shortTextBaseWeight
		}
		score += cjkScore
	}

	score += stats.wordCount * asciiWordWeight
	score += stats.longWordCount / asciiLongWordExtra

	if stats.digitCount > 0 {
		if !(stats.wordCount > 0 && stats.digitCount == 1 && !structuredText && !codeLike) {
			score += max(1, stats.digitCount/digitTokenWeight)
		}
	}
	if stats.punctCount > 0 && (stats.wordCount > 0 || structuredText || codeLike) {
		punctContribution := stats.punctCount * punctTokenWeight
		score += punctContribution
	}
	if stats.newlineCount > 0 {
		score += stats.newlineCount * newlineTokenWeight
	}

	if structuredText || codeLike {
		score += ceilDiv(structureCount, structureTokenWeight)
		if structuredText {
			score++
		}
		if jsonHeavy {
			score++
			if stats.wordCount >= structuredWordThreshold {
				score += 2
			}
		}
		if xmlHeavy {
			score += 4
		}
		if stats.xmlSyntaxCount > 0 && stats.toolPrefixSignals > 0 {
			score += stats.toolPrefixSignals * toolPrefixOverhead
		}
	}

	if stats.wordCount > 0 && stats.cjkCount == 0 && !structuredText && !codeLike {
		if stats.wordCount <= shortProseWordThreshold {
			score += shortTextBaseWeight
		} else {
			score++
		}
	}
	if stats.wordCount > shortProseWordThreshold && stats.roleLineCount == 0 && stats.punctCount > 0 && !structuredText && !codeLike {
		score++
	}

	if stats.wordCount > 0 && stats.cjkCount > 0 {
		score += mixedScriptOverhead
	}
	if stats.punctCount > 0 && stats.wordCount == 0 && stats.cjkCount == 0 && stats.digitCount == 0 && !structuredText && !codeLike {
		score += max(standalonePunctBase, ceilDiv(stats.punctCount, standalonePunctDivisor))
	}
	if stats.roleLineCount > 0 {
		score += stats.roleLineCount
	}
	if stats.roleLineCount >= 3 {
		score++
	}

	if score < minNonEmptyTokenEstimate {
		return minNonEmptyTokenEstimate
	}
	return score
}

func estimateChatPromptTokens(prompt string) int {
	if prompt == "" {
		return 0
	}
	stats := collectTokenEstimateStats(prompt)
	score := estimateTextTokens(prompt)
	score += stats.roleLineCount * roleLineOverheadWeight
	score += stats.modelMarkerCount * modelMarkerOverhead
	if score < minNonEmptyTokenEstimate {
		return minNonEmptyTokenEstimate
	}
	return score
}

func ceilDiv(n int, d int) int {
	if n <= 0 || d <= 0 {
		return 0
	}
	return (n + d - 1) / d
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

// ChatUsageFromHeuristicFallback estimates chat usage when upstream usage is missing.
func ChatUsageFromHeuristicFallback(prompt string, completion string) map[string]any {
	p := estimateChatPromptTokens(prompt)
	c := estimateTextTokens(completion)
	return map[string]any{
		"prompt_tokens":     p,
		"completion_tokens": c,
		"total_tokens":      p + c,
	}
}

// ChatUsageFromCharCount is kept as a compatibility wrapper around the heuristic fallback estimator.
func ChatUsageFromCharCount(prompt string, completion string) map[string]any {
	return ChatUsageFromHeuristicFallback(prompt, completion)
}

// ResponsesUsageFromHeuristicFallback estimates responses usage when upstream usage is missing.
func ResponsesUsageFromHeuristicFallback(input string, output string) map[string]any {
	in := estimateTextTokens(input)
	out := estimateTextTokens(output)
	return map[string]any{
		"input_tokens":  in,
		"output_tokens": out,
		"total_tokens":  in + out,
	}
}

// ResponsesUsageFromCharCount is kept as a compatibility wrapper around the heuristic fallback estimator.
func ResponsesUsageFromCharCount(input string, output string) map[string]any {
	return ResponsesUsageFromHeuristicFallback(input, output)
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
		if line := messageToPromptLine(m.Role, coerceContentToStr(m.Content)); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func messageToPromptLine(role string, content string) string {
	normalizedRole := strings.TrimSpace(role)
	if normalizedRole == "" {
		normalizedRole = "user"
	}
	normalizedContent := strings.TrimSpace(normalizeMessageContentForPrompt(normalizedRole, content))
	if normalizedContent == "" {
		return ""
	}
	return normalizedRole + ": " + normalizedContent
}

func normalizeMessageContentForPrompt(role string, content string) string {
	if !strings.EqualFold(strings.TrimSpace(role), "assistant") {
		return content
	}
	return normalizeAssistantHistoryContent(content)
}

func normalizeAssistantHistoryContent(content string) string {
	current := strings.TrimSpace(stripThinkingContent(content))
	if current == "" {
		return ""
	}

	toolCalls := make([]ToolCall, 0, 2)
	for i := 0; i < 8; i++ {
		parsed := ParseToolSentinel(current)
		if len(parsed.ToolCalls) == 0 {
			break
		}
		toolCalls = append(toolCalls, parsed.ToolCalls...)
		nextSource := parsed.Content
		if parsed.HasSentinel {
			// If sentinel is embedded in free-form text, keep the surrounding text.
			outside := strings.TrimSpace(tcSentinelBlockRe.ReplaceAllString(current, " "))
			if outside != "" {
				nextSource = outside
			}
		}
		next := strings.TrimSpace(stripThinkingContent(nextSource))
		if next == current {
			current = next
			break
		}
		current = next
		if current == "" {
			break
		}
	}

	var rawNames []string
	var foundRaw bool
	parsedCallCount := len(toolCalls)
	strippedCurrent, strippedNames, foundRaw := stripRawToolCallArtifacts(current)
	if foundRaw {
		if !toolCallRawDebugPreserveEnabled() {
			current = strippedCurrent
			rawNames = strippedNames
			for _, name := range rawNames {
				if strings.TrimSpace(name) == "" {
					continue
				}
				toolCalls = append(toolCalls, ToolCall{Name: name})
			}
		}
	}

	current = strings.TrimSpace(current)
	if current != "" {
		if foundRaw && parsedCallCount == 0 && len(rawNames) > 0 {
			return summarizeAssistantToolCalls(toolCalls)
		}
		return current
	}
	return summarizeAssistantToolCalls(toolCalls)
}

func toolCallRawDebugPreserveEnabled() bool {
	v := strings.TrimSpace(os.Getenv("PROXY_LOG_IO"))
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func stripThinkingContent(content string) string {
	out := thinkingBlockRe.ReplaceAllString(content, " ")
	out = thinkingSelfClosingRe.ReplaceAllString(out, " ")
	return out
}

func stripRawToolCallArtifacts(content string) (string, []string, bool) {
	var names []string
	found := false
	replaceFn := func(block string) string {
		found = true
		if name := inferToolNameFromArtifact(block); name != "" {
			names = append(names, name)
		}
		return " "
	}
	out := toolCallTagBlockRe.ReplaceAllStringFunc(content, replaceFn)
	out = tcSentinelBlockRe.ReplaceAllStringFunc(out, replaceFn)
	if cleaned, extraNames, ok := stripUnclosedToolCallArtifacts(out); ok {
		found = true
		out = cleaned
		names = append(names, extraNames...)
	}
	if !found {
		return content, nil, false
	}
	return strings.TrimSpace(out), uniqueNonEmptyNames(names), true
}

func stripUnclosedToolCallArtifacts(content string) (string, []string, bool) {
	lower := strings.ToLower(content)
	opens := toolCallOpenTagRe.FindAllStringIndex(content, -1)
	for _, idx := range opens {
		start := idx[0]
		end := idx[1]
		if strings.Contains(lower[end:], "</tool_call>") {
			continue
		}
		block := content[start:]
		name := strings.TrimSpace(inferToolNameFromArtifact(block))
		if name == "" {
			continue
		}
		return strings.TrimSpace(content[:start]), []string{name}, true
	}
	return content, nil, false
}

func inferToolNameFromArtifact(block string) string {
	parsed := ParseToolSentinel(strings.TrimSpace(block))
	for _, call := range parsed.ToolCalls {
		if name := strings.TrimSpace(call.Name); name != "" {
			return name
		}
	}
	if m := toolNameValueRe.FindStringSubmatch(block); len(m) == 2 {
		if name := strings.TrimSpace(m[1]); name != "" {
			return name
		}
	}
	if m := taggedToolNameRe.FindStringSubmatch(block); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if m := jsonToolNRe.FindStringSubmatch(block); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if m := jsonToolNameRe.FindStringSubmatch(block); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if m := jsonToolKeyRe.FindStringSubmatch(block); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func summarizeAssistantToolCalls(calls []ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		if name := strings.TrimSpace(call.Name); name != "" {
			names = append(names, name)
		}
	}
	names = uniqueNonEmptyNames(names)
	if len(names) == 0 {
		return "assistant_tool_call: unknown"
	}
	return "assistant_tool_call: " + names[0]
}

func uniqueNonEmptyNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
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
	_, _, raw, ok := extractSentinelPayload(text)
	if !ok {
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

func extractSentinelPayload(text string) (int, int, string, bool) {
	type marker struct {
		open  string
		close string
	}
	markers := []marker{
		{open: "<<<TC>>>", close: "<<<END>>>"},
		{open: "<<TC>>", close: "<<END>>"},
	}
	for _, m := range markers {
		start := strings.Index(text, m.open)
		if start < 0 {
			continue
		}
		payloadStart := start + len(m.open)
		endOffset := strings.Index(text[payloadStart:], m.close)
		if endOffset < 0 {
			continue
		}
		payloadEnd := payloadStart + endOffset
		if payloadEnd <= payloadStart {
			continue
		}
		return start, payloadEnd, text[payloadStart:payloadEnd], true
	}
	return -1, -1, "", false
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
	name, argBytes, ok := parseTaggedToolCall(inner)
	if !ok {
		return ToolParseResult{Content: trimmed}
	}
	left := trimmed[:start]
	right := trimmed[innerEnd+len(closeTag):]
	// If removing the tag would create a doubled space at the join point,
	// drop exactly one leading space from the right side (don't globally normalize whitespace).
	if strings.HasSuffix(left, " ") && !strings.HasSuffix(left, "  ") && strings.HasPrefix(right, " ") {
		right = strings.TrimPrefix(right, " ")
	}
	content := strings.TrimSpace(left + right)
	return ToolParseResult{
		ToolCalls: []ToolCall{{
			ID:        newID("call"),
			Name:      name,
			Arguments: string(argBytes),
		}},
		Content: content,
	}
}

func parseTaggedToolCall(inner string) (string, []byte, bool) {
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" || !strings.HasPrefix(trimmed, "<") {
		return "", nil, false
	}

	if name, argBytes, ok := parseToolNameTaggedToolCall(trimmed); ok {
		return name, argBytes, true
	}

	if strings.HasSuffix(trimmed, "/>") {
		name, argBytes, ok := parseSelfClosingTaggedToolCall(trimmed)
		if ok {
			return name, argBytes, true
		}
	}

	nameEnd := strings.Index(trimmed, ">")
	if nameEnd <= 1 {
		return "", nil, false
	}
	name := strings.TrimSpace(trimmed[1:nameEnd])
	if name == "" || strings.ContainsAny(name, " \t\r\n/") {
		return "", nil, false
	}
	closeInnerTag := "</" + name + ">"
	closeInnerIdx := strings.LastIndex(trimmed, closeInnerTag)
	if closeInnerIdx < 0 {
		return "", nil, false
	}
	argText := strings.TrimSpace(trimmed[nameEnd+1 : closeInnerIdx])
	if argText == "" {
		return "", nil, false
	}
	argBytes, ok := parseTaggedToolArguments(argText)
	if !ok {
		return "", nil, false
	}
	return name, argBytes, true
}

func parseToolNameTaggedToolCall(inner string) (string, []byte, bool) {
	trimmed := strings.TrimSpace(inner)
	lower := strings.ToLower(trimmed)
	const openTag = "<tool_name>"
	const closeTag = "</tool_name>"
	if !strings.HasPrefix(lower, openTag) {
		return "", nil, false
	}
	closeIdx := strings.Index(lower, closeTag)
	if closeIdx < 0 {
		return "", nil, false
	}
	name := strings.TrimSpace(trimmed[len(openTag):closeIdx])
	if name == "" {
		return "", nil, false
	}
	argText := strings.TrimSpace(trimmed[closeIdx+len(closeTag):])
	if argText == "" {
		return "", nil, false
	}
	argBytes, ok := parseTaggedToolArguments(argText)
	if !ok {
		return "", nil, false
	}
	return name, argBytes, true
}

func parseSelfClosingTaggedToolCall(inner string) (string, []byte, bool) {
	decoder := xml.NewDecoder(strings.NewReader(inner))
	token, err := decoder.Token()
	if err != nil {
		return "", nil, false
	}

	start, ok := token.(xml.StartElement)
	if !ok {
		return "", nil, false
	}
	name := strings.TrimSpace(start.Name.Local)
	if name == "" {
		return "", nil, false
	}

	args := map[string]any{}
	for _, attr := range start.Attr {
		key := strings.TrimSpace(attr.Name.Local)
		if key == "" {
			continue
		}
		mergeXMLToolArg(args, key, attr.Value)
	}

	seenEnd := false
	for {
		token, err = decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, false
		}
		switch tok := token.(type) {
		case xml.EndElement:
			if strings.TrimSpace(tok.Name.Local) != name {
				return "", nil, false
			}
			seenEnd = true
		case xml.CharData:
			if strings.TrimSpace(string(tok)) != "" {
				return "", nil, false
			}
		case xml.StartElement:
			return "", nil, false
		}
	}

	if !seenEnd {
		return "", nil, false
	}
	if len(args) == 0 {
		return name, []byte("{}"), true
	}

	argBytes, err := json.Marshal(args)
	if err != nil {
		return "", nil, false
	}
	return name, argBytes, true
}

func parseTaggedToolArguments(argText string) ([]byte, bool) {
	trimmed := strings.TrimSpace(argText)
	if trimmed == "" {
		return nil, false
	}

	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		b, err := json.Marshal(payload)
		if err == nil {
			return b, true
		}
	}

	return parseXMLToolArguments(trimmed)
}

type xmlToolArgNode struct {
	XMLName  xml.Name
	Content  string           `xml:",chardata"`
	Children []xmlToolArgNode `xml:",any"`
}

func parseXMLToolArguments(argText string) ([]byte, bool) {
	wrapped := "<tool_args>" + argText + "</tool_args>"
	var root xmlToolArgNode
	if err := xml.Unmarshal([]byte(wrapped), &root); err != nil {
		return nil, false
	}
	if len(root.Children) == 0 {
		return nil, false
	}

	args := map[string]any{}
	for _, child := range root.Children {
		key := strings.TrimSpace(child.XMLName.Local)
		if key == "" {
			continue
		}
		mergeXMLToolArg(args, key, xmlToolArgNodeValue(child))
	}
	if len(args) == 0 {
		return nil, false
	}

	b, err := json.Marshal(args)
	if err != nil {
		return nil, false
	}
	return b, true
}

func xmlToolArgNodeValue(node xmlToolArgNode) any {
	if len(node.Children) == 0 {
		return strings.TrimSpace(node.Content)
	}
	obj := map[string]any{}
	for _, child := range node.Children {
		key := strings.TrimSpace(child.XMLName.Local)
		if key == "" {
			continue
		}
		mergeXMLToolArg(obj, key, xmlToolArgNodeValue(child))
	}
	if len(obj) == 0 {
		return strings.TrimSpace(node.Content)
	}
	return obj
}

func mergeXMLToolArg(dst map[string]any, key string, value any) {
	if existing, ok := dst[key]; ok {
		if arr, ok := existing.([]any); ok {
			dst[key] = append(arr, value)
			return
		}
		dst[key] = []any{existing, value}
		return
	}
	dst[key] = value
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
	payload["tc_protocol"] = "<tool_call><tool_name>{\"arg\":\"value\"}</tool_name></tool_call>"
	switch {
	case trimmedChoice == "", strings.EqualFold(trimmedChoice, "auto"):
		payload["tc_instruction"] = "When a tool is needed, output exactly one tool call using tc_protocol. Otherwise respond with normal natural language."
	case strings.EqualFold(trimmedChoice, "none"):
		payload["tc_instruction"] = "Do not call tools. Respond with natural language only."
		payload["tc_forbid"] = true
	default:
		payload["tc_instruction"] = "You must respond with exactly one tool call using tc_protocol and no natural-language text."
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
