package openai

import "strings"

const functionCallsMarker = "[function_calls]"

type ToolStreamState struct {
	ContentBuffer    string
	IsBuffering      bool
	ToolCallIndex    int
	HasEmittedTool   bool
}

type StreamDelta struct {
	Content   string
	ToolCalls []ToolCall
}

type StreamParseResult struct {
	Deltas      []StreamDelta
	ShouldFlush bool
}

func ParseToolCallsStream(content string, state *ToolStreamState, model string) StreamParseResult {
	result := StreamParseResult{}
	if content == "" {
		return result
	}
	if !supportsToolCallCompat(model) {
		result.Deltas = append(result.Deltas, StreamDelta{Content: content})
		result.ShouldFlush = true
		return result
	}

	state.ContentBuffer += content

	if !state.IsBuffering {
		markerIdx := strings.Index(state.ContentBuffer, functionCallsMarker)
		if markerIdx >= 0 {
			state.IsBuffering = true
			if markerIdx > 0 {
				result.Deltas = append(result.Deltas, StreamDelta{Content: state.ContentBuffer[:markerIdx]})
				state.ContentBuffer = state.ContentBuffer[markerIdx:]
			}
		} else {
			prefixIdx := findMarkerPrefixIndex(state.ContentBuffer)
			if prefixIdx >= 0 {
				state.IsBuffering = true
				if prefixIdx > 0 {
					result.Deltas = append(result.Deltas, StreamDelta{Content: state.ContentBuffer[:prefixIdx]})
					state.ContentBuffer = state.ContentBuffer[prefixIdx:]
				}
				return result
			}
			result.Deltas = append(result.Deltas, StreamDelta{Content: state.ContentBuffer})
			state.ContentBuffer = ""
			result.ShouldFlush = true
			return result
		}
	}

	if state.IsBuffering {
		hasFullMarker := strings.Contains(state.ContentBuffer, functionCallsMarker)
		isPrefix := strings.HasPrefix(functionCallsMarker, state.ContentBuffer)
		if !hasFullMarker && !isPrefix {
			result.Deltas = append(result.Deltas, StreamDelta{Content: state.ContentBuffer})
			state.ContentBuffer = ""
			state.IsBuffering = false
			result.ShouldFlush = true
			return result
		}

		parsed := ParseToolCallsFromText(state.ContentBuffer, model)
		if len(parsed.ToolCalls) > 0 {
			for _, tc := range parsed.ToolCalls {
				tc.Index = state.ToolCallIndex
				state.ToolCallIndex++
				result.Deltas = append(result.Deltas, StreamDelta{ToolCalls: []ToolCall{tc}})
			}
			state.HasEmittedTool = true
			if parsed.Content != "" {
				result.Deltas = append(result.Deltas, StreamDelta{Content: parsed.Content})
			}
			if strings.Contains(state.ContentBuffer, "[/function_calls]") {
				state.IsBuffering = false
				state.ContentBuffer = ""
				result.ShouldFlush = true
				return result
			}
		}

		if len(state.ContentBuffer) > 1024*1024 {
			result.Deltas = append(result.Deltas, StreamDelta{Content: state.ContentBuffer})
			state.ContentBuffer = ""
			state.IsBuffering = false
			result.ShouldFlush = true
			return result
		}
	}

	return result
}

func FlushToolCallBuffer(state *ToolStreamState, model string) []StreamDelta {
	if state.ContentBuffer == "" {
		return nil
	}

	parsed := ParseToolCallsFromText(state.ContentBuffer, model)
	var deltas []StreamDelta
	if len(parsed.ToolCalls) > 0 {
		for _, tc := range parsed.ToolCalls {
			tc.Index = state.ToolCallIndex
			state.ToolCallIndex++
			deltas = append(deltas, StreamDelta{ToolCalls: []ToolCall{tc}})
		}
		state.HasEmittedTool = true
		if parsed.Content != "" {
			deltas = append(deltas, StreamDelta{Content: parsed.Content})
		}
	} else if !state.HasEmittedTool {
		deltas = append(deltas, StreamDelta{Content: state.ContentBuffer})
	}

	state.ContentBuffer = ""
	state.IsBuffering = false
	return deltas
}

func findMarkerPrefixIndex(text string) int {
	for i := 0; i < len(text); i++ {
		if text[i] == '[' && strings.HasPrefix(functionCallsMarker, text[i:]) {
			return i
		}
	}
	return -1
}
