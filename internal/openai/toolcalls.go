package openai

import (
	"encoding/json"
	"regexp"
	"strings"
)

type ToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ParsedToolCallText struct {
	Content   string
	ToolCalls []ToolCall
}

func ParseToolCallsFromText(text string, model string) ParsedToolCallText {
	text = strings.TrimSpace(text)
	if text == "" {
		return ParsedToolCallText{}
	}

	if !supportsToolCallCompat(model) {
		return ParsedToolCallText{Content: text}
	}

	text = normalizeFunctionCalls(text)
	if !strings.Contains(text, "[function_calls]") {
		return ParsedToolCallText{Content: text}
	}

	callRe := regexp.MustCompile(`\[call[:=]?\s*([a-zA-Z0-9_:-]+)\]`)
	blockRe := regexp.MustCompile(`\[function_calls\]([\s\S]*?)(?:\[\/function_calls\]|$)`)

	var toolCalls []ToolCall
	clean := text
	blocks := blockRe.FindAllStringSubmatch(text, -1)
	for _, blockMatch := range blocks {
		block := blockMatch[1]
		blockClean := block

		locs := callRe.FindAllStringSubmatchIndex(block, -1)
		for _, loc := range locs {
			name := block[loc[2]:loc[3]]
			start := loc[1]
			args, rawLen := parseToolArgs(block[start:])
			if args == "" {
				continue
			}

			rawCall := block[loc[0] : start+rawLen]
			if closeIdx := strings.Index(block[start+rawLen:], "[/call]"); closeIdx >= 0 {
				rawCall = block[loc[0] : start+rawLen+closeIdx+len("[/call]")]
			}
			toolCalls = append(toolCalls, ToolCall{
				Index: len(toolCalls),
				ID:    newID("call"),
				Type:  "function",
				Function: ToolCallFunction{
					Name:      name,
					Arguments: args,
				},
			})
			blockClean = strings.Replace(blockClean, rawCall, "", 1)
		}

		clean = strings.Replace(clean, blockMatch[0], strings.ReplaceAll(blockClean, "[/function_calls]", ""), 1)
	}

	clean = strings.ReplaceAll(clean, "[function_calls]", "")
	clean = strings.ReplaceAll(clean, "[/function_calls]", "")
	return ParsedToolCallText{Content: strings.TrimSpace(clean), ToolCalls: toolCalls}
}

func normalizeFunctionCalls(text string) string {
	re := regexp.MustCompile(`(^|[^/\[])(function_calls\])`)
	if strings.Contains(text, "[function_calls]") || !re.MatchString(text) {
		return text
	}
	return re.ReplaceAllString(text, "$1[$2")
}

func supportsToolCallCompat(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false
	}
	if strings.Contains(m, "qwen3") {
		return true
	}
	if m == "fast" || strings.Contains(m, "fast") {
		return true
	}
	if strings.Contains(m, "deepseek") && strings.Contains(m, "v3.2") {
		return true
	}
	return false
}

func parseToolArgs(text string) (string, int) {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if trimmed == "" {
		return "", 0
	}

	if strings.HasPrefix(trimmed, "```") {
		if end := strings.Index(trimmed[3:], "```"); end >= 0 {
			trimmed = strings.TrimSpace(trimmed[3 : end+3])
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "json"))
		}
	}

	if raw := balancedJSON(trimmed); raw != "" {
		if normalized := normalizeJSON(raw); normalized != "" {
			return normalized, strings.Index(trimmed, raw) + len(raw)
		}
	}

	if raw := toolFallbackJSON(trimmed); raw != "" {
		return raw, len(trimmed)
	}

	return "", 0
}

func balancedJSON(text string) string {
	start := strings.Index(text, "{")
	if start < 0 {
		return ""
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		c := text[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}

func normalizeJSON(raw string) string {
	raw = strings.ReplaceAll(raw, "\r", "")
	raw = regexp.MustCompile(`([{,]\s*)([a-zA-Z_][a-zA-Z0-9_]*)\s*:`).ReplaceAllString(raw, `$1"$2":`)
	raw = strings.ReplaceAll(raw, "'", `"`)

	var out any
	if json.Unmarshal([]byte(raw), &out) == nil {
		return mustJSON(out)
	}

	raw = strings.ReplaceAll(raw, "\n", "\\n")
	raw = strings.ReplaceAll(raw, "\t", "\\t")
	if json.Unmarshal([]byte(raw), &out) == nil {
		return mustJSON(out)
	}
	return ""
}

func toolFallbackJSON(text string) string {
	if looksLikeWriteTool(text) {
		filePath := captureQuotedField(text, "filePath")
		content := captureQuotedField(text, "content")
		if filePath != "" && content != "" {
			return mustJSON(map[string]any{"filePath": filePath, "content": content})
		}
	}
	if looksLikeReplaceTool(text) {
		filePath := captureQuotedField(text, "filePath")
		oldStr := captureQuotedField(text, "old_str")
		newStr := captureQuotedField(text, "new_str")
		if filePath != "" && oldStr != "" && newStr != "" {
			return mustJSON(map[string]any{
				"filePath": filePath,
				"old_str":  oldStr,
				"new_str":  newStr,
			})
		}
	}
	return ""
}

func looksLikeWriteTool(text string) bool {
	return strings.Contains(text, `"filePath"`) && strings.Contains(text, `"content"`)
}

func looksLikeReplaceTool(text string) bool {
	return strings.Contains(text, `"filePath"`) && strings.Contains(text, `"old_str"`) && strings.Contains(text, `"new_str"`)
}

func captureQuotedField(text, key string) string {
	token := `"` + key + `"`
	idx := strings.Index(text, token)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(token):]
	first := strings.Index(rest, `"`)
	if first < 0 {
		return ""
	}
	rest = rest[first+1:]
	var b strings.Builder
	escaped := false
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if escaped {
			switch c {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte(c)
			}
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			return b.String()
		}
		b.WriteByte(c)
	}
	return ""
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
