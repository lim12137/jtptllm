package http

import (
	"io"
	"strings"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	h := NewHandler(HandlerDeps{DefaultModel: "agent"})
	req := httptest.NewRequest(stdhttp.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestStreamChatCompletionEmitsToolCallsForFast(t *testing.T) {
	body := strings.Join([]string{
		`data: {"content":[{"type":"text","text":"before "}]} `,
		``,
		`data: {"content":[{"type":"text","text":"[function_calls][call:write_to_file]{\"filePath\":\"/tmp/a.txt\",\"content\":\"hello\"}[/call][/function_calls]"}]}`,
		``,
		`data: {"content":[{"type":"text","text":" after"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &stdhttp.Response{Body: io.NopCloser(strings.NewReader(body))}
	rec := httptest.NewRecorder()

	if err := streamChatCompletion(rec, resp, "fast"); err != nil {
		t.Fatalf("streamChatCompletion error: %v", err)
	}

	out := rec.Body.String()
	if !strings.Contains(out, `"tool_calls"`) {
		t.Fatalf("expected tool_calls in stream: %s", out)
	}
	if !strings.Contains(out, `"finish_reason":"tool_calls"`) {
		t.Fatalf("expected tool_calls finish reason: %s", out)
	}
	if strings.Contains(out, `[function_calls]`) {
		t.Fatalf("raw marker leaked: %s", out)
	}
}

func TestStreamChatCompletionKeepsPlainTextForOtherModels(t *testing.T) {
	body := strings.Join([]string{
		`data: {"content":[{"type":"text","text":"[function_calls][call:write_to_file]{\"filePath\":\"/tmp/a.txt\",\"content\":\"hello\"}[/call][/function_calls]"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &stdhttp.Response{Body: io.NopCloser(strings.NewReader(body))}
	rec := httptest.NewRecorder()

	if err := streamChatCompletion(rec, resp, "agent"); err != nil {
		t.Fatalf("streamChatCompletion error: %v", err)
	}

	out := rec.Body.String()
	if strings.Contains(out, `"tool_calls"`) {
		t.Fatalf("unexpected tool_calls for non-compat model: %s", out)
	}
	if !strings.Contains(out, `[function_calls]`) {
		t.Fatalf("expected raw content for non-compat model: %s", out)
	}
}
