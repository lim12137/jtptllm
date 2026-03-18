package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/lim12137/jtptllm/internal/gateway"
	"github.com/lim12137/jtptllm/internal/session"
)

type stubGateway struct {
	createCount int
	deleteCount int
	lastRun     gateway.RunRequest
	runResp     map[string]any
	runErr      error
	streamResp  *http.Response
	streamErr   error
}

func (s *stubGateway) CreateSession(ctx context.Context) (string, error) {
	s.createCount++
	return fmt.Sprintf("s%d", s.createCount), nil
}

func (s *stubGateway) Run(ctx context.Context, req gateway.RunRequest) (*http.Response, map[string]any, error) {
	s.lastRun = req
	if req.Stream {
		return s.streamResp, nil, s.streamErr
	}
	return nil, s.runResp, s.runErr
}

func (s *stubGateway) DeleteSession(ctx context.Context, sessionID string) error {
	s.deleteCount++
	return nil
}

func newTestServer(gw Gateway) *Server {
	var pools *session.PoolManager
	if gw != nil {
		// Use pool size=1 to keep tests deterministic.
		pools = session.NewPoolManager(gw, 600, 1)
	}
	return NewServer(gw, pools, Options{DefaultModel: "agent"})
}

func TestHealth(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("cors header missing")
	}
	if v := rec.Header().Get("Access-Control-Allow-Credentials"); v != "" {
		t.Fatalf("allow-credentials=%q", v)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("ok=false")
	}
}

func TestModelEndpoints(t *testing.T) {
	srv := newTestServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/model", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/model status=%d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("/model decode: %v", err)
	}
	if body["model"] != "agent" {
		t.Fatalf("model=%v", body["model"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/models status=%d", rec.Code)
	}
	body = map[string]any{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("/v1/models decode: %v", err)
	}
	if body["object"] != "list" {
		t.Fatalf("object=%v", body["object"])
	}
	data, ok := body["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("models empty")
	}
}

func TestChatCompletionsNonStream(t *testing.T) {
	gw := &stubGateway{runResp: map[string]any{
		"success": true,
		"data": map[string]any{
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": map[string]any{"value": "智能体输出文本"}},
				},
			},
		},
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":    "agent",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if gw.createCount != 1 {
		t.Fatalf("createCount=%d", gw.createCount)
	}
	if gw.lastRun.Text != "user: hi" {
		t.Fatalf("prompt=%q", gw.lastRun.Text)
	}
	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	choices, ok := out["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("choices empty")
	}
	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)
	if msg["content"] != "智能体输出文本" {
		t.Fatalf("content=%v", msg["content"])
	}
}

func TestChatCompletionToolSentinelMapping(t *testing.T) {
	gw := &stubGateway{runResp: map[string]any{
		"data": map[string]any{
			"message": map[string]any{
				"text": "<<<TC>>>{\"tc\":[{\"n\":\"get_weather\",\"a\":{\"location\":\"Paris\"}}],\"c\":\"\"}<<<END>>>",
			},
		},
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":    "agent",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	choices, ok := out["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("choices empty")
	}
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["tool_calls"] == nil {
		t.Fatalf("missing tool_calls")
	}
}

func TestResponsesNonStream(t *testing.T) {
	gw := &stubGateway{runResp: map[string]any{
		"success": true,
		"data": map[string]any{
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": map[string]any{"value": "回应"}},
				},
			},
		},
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model": "agent",
		"input": "hi",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["output_text"] != "回应" {
		t.Fatalf("output_text=%v", out["output_text"])
	}
}

type errReader struct {
	err error
}

func (e *errReader) Read(_ []byte) (int, error) {
	return 0, e.err
}

func TestStreamErrorDoesNotWriteJSON(t *testing.T) {
	gw := &stubGateway{
		streamResp: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(&errReader{err: errors.New("boom")}),
		},
	}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":    "agent",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"stream":   true,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "\"error\"") {
		t.Fatalf("unexpected json error in sse stream: %s", rec.Body.String())
	}
}

func TestIOLoggingEnabled(t *testing.T) {
	buf := &bytes.Buffer{}
	old := log.Writer()
	log.SetOutput(buf)
	defer log.SetOutput(old)
	os.Setenv("PROXY_LOG_IO", "1")
	defer os.Unsetenv("PROXY_LOG_IO")

	gw := &stubGateway{runResp: map[string]any{
		"data": map[string]any{
			"message": map[string]any{"text": "ok"},
		},
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":    "agent",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if !strings.Contains(buf.String(), "IOLOG") {
		t.Fatalf("missing IOLOG")
	}
}

func TestChatCompletionToolSentinelStreamBuffered(t *testing.T) {
	streamText := "<<<TC>>>{\"tc\":[{\"n\":\"get_weather\",\"a\":{\"location\":\"Paris\"}}],\"c\":\"\"}<<<END>>>"
	evt := map[string]any{
		"data": map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": map[string]any{"value": streamText}},
			},
		},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sse := "data: " + string(b) + "\n\n" +
		"data: [DONE]\n\n"
	gw := &stubGateway{streamResp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(sse)),
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":    "agent",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "Get weather",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"location": map[string]any{"type": "string"}},
					"required":   []any{"location"},
				},
			},
		}},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": "get_weather"}},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type=%q", ct)
	}
	bodyText := rec.Body.String()
	if !strings.Contains(bodyText, "\"tool_calls\"") {
		t.Fatalf("missing tool_calls in sse body: %s", bodyText)
	}
	if !strings.Contains(bodyText, "[DONE]") {
		t.Fatalf("missing DONE in sse body: %s", bodyText)
	}
}
