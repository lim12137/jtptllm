package http

import (
	"bufio"
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
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	runHook     func(req gateway.RunRequest) (*http.Response, map[string]any, error)
}

func (s *stubGateway) CreateSession(ctx context.Context) (string, error) {
	s.createCount++
	return fmt.Sprintf("s%d", s.createCount), nil
}

func (s *stubGateway) Run(ctx context.Context, req gateway.RunRequest) (*http.Response, map[string]any, error) {
	s.lastRun = req
	if s.runHook != nil {
		return s.runHook(req)
	}
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
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/model status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/models status=%d", rec.Code)
	}
	body := map[string]any{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("/v1/models decode: %v", err)
	}
	if body["object"] != "list" {
		t.Fatalf("object=%v", body["object"])
	}
	data, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("models missing")
	}
	if len(data) != 3 {
		t.Fatalf("models len=%d", len(data))
	}
	found := map[string]bool{}
	for _, item := range data {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := m["id"].(string); ok {
			found[id] = true
		}
	}
	if !found["fast"] || !found["deepseek"] || !found["qingyuan"] {
		t.Fatalf("models=%v", found)
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

func TestChatCompletionsNonStreamPassesThroughUsage(t *testing.T) {
	gw := &stubGateway{runResp: map[string]any{
		"success": true,
		"data": map[string]any{
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": map[string]any{"value": "ok"}},
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     11,
				"completion_tokens": 22,
				"total_tokens":      33,
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
	usage, ok := out["usage"].(map[string]any)
	if !ok {
		t.Fatalf("missing usage: %v", out["usage"])
	}
	if usage["prompt_tokens"] != float64(11) || usage["completion_tokens"] != float64(22) || usage["total_tokens"] != float64(33) {
		t.Fatalf("usage=%v", usage)
	}
}

func TestChatCompletionsNonStreamFallsBackToCharCountUsageWhenMissing(t *testing.T) {
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
	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	usage, ok := out["usage"].(map[string]any)
	if !ok {
		t.Fatalf("missing usage: %v", out["usage"])
	}
	// prompt is "user: hi" (8 runes), completion is "智能体输出文本" (7 runes), with fallback K=2.
	if usage["prompt_tokens"] != float64(16) || usage["completion_tokens"] != float64(14) || usage["total_tokens"] != float64(30) {
		t.Fatalf("usage=%v", usage)
	}
}

func TestChatCompletionsNonStreamTreatsUsableTextAsSuccess(t *testing.T) {
	gw := &stubGateway{runResp: map[string]any{
		"success": false,
		"data": map[string]any{
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": map[string]any{"value": "tiny ok"}},
				},
			},
			"error": map[string]any{
				"content": map[string]any{
					"errorMsg": "upstream failed",
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
	if msg["content"] != "tiny ok" {
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

func TestResponsesNonStreamPassesThroughUsage(t *testing.T) {
	gw := &stubGateway{runResp: map[string]any{
		"success": true,
		"data": map[string]any{
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": map[string]any{"value": "回应"}},
				},
			},
			"usage": map[string]any{
				"input_tokens":  5,
				"output_tokens": 7,
				"total_tokens":  12,
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
	usage, ok := out["usage"].(map[string]any)
	if !ok {
		t.Fatalf("missing usage: %v", out["usage"])
	}
	if usage["input_tokens"] != float64(5) || usage["output_tokens"] != float64(7) || usage["total_tokens"] != float64(12) {
		t.Fatalf("usage=%v", usage)
	}
}

func TestResponsesNonStreamFallsBackToCharCountUsageWhenMissing(t *testing.T) {
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
	usage, ok := out["usage"].(map[string]any)
	if !ok {
		t.Fatalf("missing usage: %v", out["usage"])
	}
	// input is "hi" (2 runes), output is "回应" (2 runes), with fallback K=2.
	if usage["input_tokens"] != float64(4) || usage["output_tokens"] != float64(4) || usage["total_tokens"] != float64(8) {
		t.Fatalf("usage=%v", usage)
	}
}

func TestResponsesNonStreamTreatsUsableTextAsSuccess(t *testing.T) {
	gw := &stubGateway{runResp: map[string]any{
		"success": false,
		"data": map[string]any{
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": map[string]any{"value": "回应 ok"}},
				},
			},
			"error": map[string]any{
				"content": map[string]any{
					"errorMsg": "upstream failed",
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
	if out["output_text"] != "回应 ok" {
		t.Fatalf("output_text=%v", out["output_text"])
	}
}

func TestChatCompletionsNonStreamUpstreamError(t *testing.T) {
	gw := &stubGateway{runResp: map[string]any{
		"data": map[string]any{
			"error": map[string]any{
				"content": map[string]any{
					"errorMsg": "upstream failed",
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

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d", rec.Code)
	}
	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object")
	}
	if errObj["type"] != "upstream_error" {
		t.Fatalf("type=%v", errObj["type"])
	}
	if errObj["code"] != "upstream_run_failed" {
		t.Fatalf("code=%v", errObj["code"])
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "upstream") {
		t.Fatalf("message=%v", errObj["message"])
	}
}

func TestResponsesNonStreamUpstreamError(t *testing.T) {
	gw := &stubGateway{runResp: map[string]any{
		"data": map[string]any{
			"error": map[string]any{
				"content": map[string]any{
					"errorMsg": "upstream failed",
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

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d", rec.Code)
	}
	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object")
	}
	if errObj["type"] != "upstream_error" {
		t.Fatalf("type=%v", errObj["type"])
	}
	if errObj["code"] != "upstream_run_failed" {
		t.Fatalf("code=%v", errObj["code"])
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "upstream") {
		t.Fatalf("message=%v", errObj["message"])
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

func TestChatCompletionsStreamMessageTextDelta(t *testing.T) {
	evt := map[string]any{
		"data": map[string]any{
			"message": map[string]any{"text": "hello"},
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
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "\"content\":\"hello\"") {
		t.Fatalf("missing content delta: %s", rec.Body.String())
	}
}

func TestChatCompletionsStreamMessageContentDelta(t *testing.T) {
	evt := map[string]any{
		"data": map[string]any{
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": map[string]any{"value": "hello"}},
				},
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
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "\"content\":\"hello\"") {
		t.Fatalf("missing content delta: %s", rec.Body.String())
	}
}

func TestChatCompletionsStreamUpstreamErrorDoesNotFinish(t *testing.T) {
	evt := map[string]any{
		"data": map[string]any{
			"error": map[string]any{
				"content": map[string]any{"errorMsg": "boom"},
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
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "\"finish_reason\":\"stop\"") {
		t.Fatalf("unexpected finish_reason stop: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("unexpected DONE marker: %s", rec.Body.String())
	}
}

type sseEventRecord struct {
	Event string
	Data  map[string]any
}

func parseSSEEvents(t *testing.T, body string) []sseEventRecord {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(body))
	var out []sseEventRecord
	curEvent := ""
	curData := ""
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimRight(scanner.Text(), "\r"))
		if strings.HasPrefix(line, "event:") {
			curEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			curData = payload
			continue
		}
		if line != "" {
			continue
		}
		if curEvent == "" && curData == "" {
			continue
		}
		if curEvent == "" || curData == "" {
			t.Fatalf("incomplete sse event: event=%q data=%q", curEvent, curData)
		}
		var evt map[string]any
		if err := json.Unmarshal([]byte(curData), &evt); err != nil {
			t.Fatalf("bad sse json: %v payload=%q", err, curData)
		}
		if evt["type"] != curEvent {
			t.Fatalf("sse type mismatch: event=%q type=%v payload=%q", curEvent, evt["type"], curData)
		}
		out = append(out, sseEventRecord{Event: curEvent, Data: evt})
		curEvent = ""
		curData = ""
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan sse: %v", err)
	}
	return out
}

func eventsByType(evts []sseEventRecord, typ string) []sseEventRecord {
	var out []sseEventRecord
	for _, e := range evts {
		if e.Data["type"] == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestResponsesStreamConformsToResponseEvents(t *testing.T) {
	evt := map[string]any{
		"data": map[string]any{
			"message": map[string]any{"text": "hello"},
		},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	end, _ := json.Marshal(map[string]any{"end": true})
	sse := "data: " + string(b) + "\n\n" +
		"data: " + string(end) + "\n\n"
	gw := &stubGateway{streamResp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(sse)),
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":  "agent",
		"stream": true,
		"input":  "hi",
	}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	bodyText := rec.Body.String()
	if strings.Contains(bodyText, "[DONE]") {
		t.Fatalf("responses stream must not include [DONE]: %s", bodyText)
	}

	// Each event must have "event:" + "data:" lines, and data.type == event.
	evts := parseSSEEvents(t, bodyText)

	// Required event sequence (delta may repeat).
	var got []string
	for _, e := range evts {
		got = append(got, e.Event)
	}
	if len(got) < 9 {
		t.Fatalf("too few events: %v", got)
	}
	if got[0] != "response.created" ||
		got[1] != "response.in_progress" ||
		got[2] != "response.output_item.added" ||
		got[3] != "response.content_part.added" {
		t.Fatalf("bad prefix order: %v", got)
	}
	i := 4
	deltaCount := 0
	for i < len(got) && got[i] == "response.output_text.delta" {
		deltaCount++
		i++
	}
	if deltaCount < 1 {
		t.Fatalf("missing output_text.delta: %v", got)
	}
	wantTail := []string{
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.completed",
	}
	if len(got[i:]) != len(wantTail) || strings.Join(got[i:], ",") != strings.Join(wantTail, ",") {
		t.Fatalf("bad tail order: %v", got)
	}

	createdEvts := eventsByType(evts, "response.created")
	if len(createdEvts) != 1 {
		t.Fatalf("missing response.created: %s", bodyText)
	}
	inProgEvts := eventsByType(evts, "response.in_progress")
	if len(inProgEvts) != 1 {
		t.Fatalf("missing response.in_progress: %s", bodyText)
	}

	createdResp, ok := createdEvts[0].Data["response"].(map[string]any)
	if !ok {
		t.Fatalf("response.created missing response: %v", createdEvts[0].Data)
	}
	if createdResp["usage"] != nil {
		t.Fatalf("created response.usage must be null: %v", createdResp["usage"])
	}
	inProgResp, ok := inProgEvts[0].Data["response"].(map[string]any)
	if !ok {
		t.Fatalf("response.in_progress missing response: %v", inProgEvts[0].Data)
	}
	if inProgResp["usage"] != nil {
		t.Fatalf("in_progress response.usage must be null: %v", inProgResp["usage"])
	}

	added := eventsByType(evts, "response.output_item.added")
	if len(added) != 1 {
		t.Fatalf("missing response.output_item.added: %s", bodyText)
	}
	addedItem, ok := added[0].Data["item"].(map[string]any)
	if !ok {
		t.Fatalf("output_item.added missing item: %v", added[0].Data)
	}
	addedContent, _ := addedItem["content"].([]any)
	if len(addedContent) != 0 {
		t.Fatalf("output_item.added item.content must be empty array: %v", addedItem["content"])
	}

	partAdded := eventsByType(evts, "response.content_part.added")
	if len(partAdded) != 1 {
		t.Fatalf("missing response.content_part.added: %s", bodyText)
	}

	deltas := eventsByType(evts, "response.output_text.delta")
	for _, d := range deltas {
		if _, ok := d.Data["sequence_number"]; !ok {
			t.Fatalf("delta missing sequence_number: %v", d.Data)
		}
		if _, ok := d.Data["item_id"]; !ok {
			t.Fatalf("delta missing item_id: %v", d.Data)
		}
		if _, ok := d.Data["output_index"]; !ok {
			t.Fatalf("delta missing output_index: %v", d.Data)
		}
		if _, ok := d.Data["content_index"]; !ok {
			t.Fatalf("delta missing content_index: %v", d.Data)
		}
	}

	if len(eventsByType(evts, "response.output_text.done")) != 1 {
		t.Fatalf("missing response.output_text.done: %s", bodyText)
	}
	if len(eventsByType(evts, "response.content_part.done")) != 1 {
		t.Fatalf("missing response.content_part.done: %s", bodyText)
	}

	itemDone := eventsByType(evts, "response.output_item.done")
	if len(itemDone) != 1 {
		t.Fatalf("missing response.output_item.done: %s", bodyText)
	}
	doneItem, ok := itemDone[0].Data["item"].(map[string]any)
	if !ok {
		t.Fatalf("output_item.done missing item: %v", itemDone[0].Data)
	}
	if doneItem["status"] != "completed" {
		t.Fatalf("output_item.done item.status must be completed: %v", doneItem["status"])
	}
	doneContent, _ := doneItem["content"].([]any)
	if len(doneContent) == 0 {
		t.Fatalf("output_item.done item.content must include output_text: %v", doneItem["content"])
	}

	completed := eventsByType(evts, "response.completed")
	if len(completed) != 1 {
		t.Fatalf("missing response.completed: %s", bodyText)
	}
	resp, ok := completed[0].Data["response"].(map[string]any)
	if !ok {
		t.Fatalf("response.completed missing response object: %v", completed[0].Data)
	}
	usage, ok := resp["usage"].(map[string]any)
	if !ok {
		t.Fatalf("response.completed.response missing usage object: %v", resp)
	}
	if _, ok := usage["input_tokens"]; !ok {
		t.Fatalf("usage missing input_tokens: %v", usage)
	}
	if _, ok := usage["output_tokens"]; !ok {
		t.Fatalf("usage missing output_tokens: %v", usage)
	}
	if _, ok := usage["total_tokens"]; !ok {
		t.Fatalf("usage missing total_tokens: %v", usage)
	}
}

func TestResponsesStreamPassesThroughUsage(t *testing.T) {
	evt := map[string]any{
		"data": map[string]any{
			"message": map[string]any{"text": "hello"},
		},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	end, _ := json.Marshal(map[string]any{
		"end": true,
		"usage": map[string]any{
			"input_tokens":  9,
			"output_tokens": 8,
			"total_tokens":  17,
		},
	})
	sse := "data: " + string(b) + "\n\n" +
		"data: " + string(end) + "\n\n"
	gw := &stubGateway{streamResp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(sse)),
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":  "agent",
		"stream": true,
		"input":  "hi",
	}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	evts := parseSSEEvents(t, rec.Body.String())
	completed := eventsByType(evts, "response.completed")
	if len(completed) != 1 {
		t.Fatalf("expected 1 response.completed, got %d", len(completed))
	}
	resp, ok := completed[0].Data["response"].(map[string]any)
	if !ok {
		t.Fatalf("missing response object: %v", completed[0].Data)
	}
	usage, ok := resp["usage"].(map[string]any)
	if !ok {
		t.Fatalf("missing usage: %v", resp["usage"])
	}
	if usage["input_tokens"] != float64(9) || usage["output_tokens"] != float64(8) || usage["total_tokens"] != float64(17) {
		t.Fatalf("usage=%v", usage)
	}
}

func TestResponsesStreamFallsBackToCharCountUsageWhenMissing(t *testing.T) {
	evt := map[string]any{
		"data": map[string]any{
			"message": map[string]any{"text": "hello"},
		},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	end, _ := json.Marshal(map[string]any{
		"end": true,
	})
	sse := "data: " + string(b) + "\n\n" +
		"data: " + string(end) + "\n\n"
	gw := &stubGateway{streamResp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(sse)),
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":  "agent",
		"stream": true,
		"input":  "hi",
	}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	evts := parseSSEEvents(t, rec.Body.String())
	completed := eventsByType(evts, "response.completed")
	if len(completed) != 1 {
		t.Fatalf("expected 1 response.completed, got %d", len(completed))
	}
	resp, ok := completed[0].Data["response"].(map[string]any)
	if !ok {
		t.Fatalf("missing response object: %v", completed[0].Data)
	}
	usage, ok := resp["usage"].(map[string]any)
	if !ok {
		t.Fatalf("missing usage: %v", resp["usage"])
	}
	// input is "hi" (2 runes), output is "hello" (5 runes), with fallback K=2.
	if usage["input_tokens"] != float64(4) || usage["output_tokens"] != float64(10) || usage["total_tokens"] != float64(14) {
		t.Fatalf("usage=%v", usage)
	}
}

func TestResponsesStreamToolCallUsesFunctionEventsNotOutputText(t *testing.T) {
	streamText := "<<<TC>>>{\"tc\":[{\"id\":\"call_1\",\"n\":\"spawn_agent\",\"a\":{\"task\":\"rebuild\"}}],\"c\":\"\"}<<<END>>>"
	evt := map[string]any{
		"data": map[string]any{
			"message": map[string]any{"text": streamText},
		},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	end, _ := json.Marshal(map[string]any{"end": true})
	sse := "data: " + string(b) + "\n\n" +
		"data: " + string(end) + "\n\n"
	gw := &stubGateway{streamResp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(sse)),
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":  "agent",
		"stream": true,
		"input":  "run tool",
	}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	evts := parseSSEEvents(t, rec.Body.String())

	if len(eventsByType(evts, "response.output_text.delta")) != 0 {
		t.Fatalf("tool call stream must not emit response.output_text.delta: %s", rec.Body.String())
	}
	if len(eventsByType(evts, "response.function_call_arguments.delta")) < 1 {
		t.Fatalf("missing response.function_call_arguments.delta: %s", rec.Body.String())
	}
	done := eventsByType(evts, "response.function_call_arguments.done")
	if len(done) != 1 {
		t.Fatalf("missing response.function_call_arguments.done: %s", rec.Body.String())
	}
	if done[0].Data["name"] != "spawn_agent" {
		t.Fatalf("function name=%v", done[0].Data["name"])
	}
	itemDone := eventsByType(evts, "response.output_item.done")
	if len(itemDone) < 1 {
		t.Fatalf("missing response.output_item.done: %s", rec.Body.String())
	}
	seenFC := false
	for _, evt := range itemDone {
		item, ok := evt.Data["item"].(map[string]any)
		if !ok {
			continue
		}
		if item["type"] == "function_call" {
			seenFC = true
			break
		}
	}
	if !seenFC {
		t.Fatalf("expected function_call item.done: %s", rec.Body.String())
	}
}

func TestResponsesStreamRawFunctionCallJSONUsesFunctionEventsNotOutputText(t *testing.T) {
	streamText := "{\"function_call\":{\"name\":\"spawn_agent\",\"arguments\":{\"task\":\"rebuild\"}}}"
	evt := map[string]any{
		"data": map[string]any{
			"message": map[string]any{"text": streamText},
		},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	end, _ := json.Marshal(map[string]any{"end": true})
	sse := "data: " + string(b) + "\n\n" +
		"data: " + string(end) + "\n\n"
	gw := &stubGateway{streamResp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(sse)),
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":  "agent",
		"stream": true,
		"input":  "run tool",
	}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	evts := parseSSEEvents(t, rec.Body.String())

	if len(eventsByType(evts, "response.output_text.delta")) != 0 {
		t.Fatalf("tool call stream must not emit response.output_text.delta: %s", rec.Body.String())
	}
	if len(eventsByType(evts, "response.function_call_arguments.delta")) < 1 {
		t.Fatalf("missing response.function_call_arguments.delta: %s", rec.Body.String())
	}
	done := eventsByType(evts, "response.function_call_arguments.done")
	if len(done) != 1 {
		t.Fatalf("missing response.function_call_arguments.done: %s", rec.Body.String())
	}
	if done[0].Data["name"] != "spawn_agent" {
		t.Fatalf("function name=%v", done[0].Data["name"])
	}
}

func TestResponsesStreamTagWrappedToolCallUsesFunctionEventsNotOutputText(t *testing.T) {
	streamText := "<tool_call><multi_tool_use.parallel>{\"tool_uses\":[{\"recipient_name\":\"functions.shell_command\",\"parameters\":{\"command\":\"Get-Date\"}}]}</multi_tool_use.parallel></tool_call>"
	evt := map[string]any{
		"data": map[string]any{
			"message": map[string]any{"text": streamText},
		},
	}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	end, _ := json.Marshal(map[string]any{"end": true})
	sse := "data: " + string(b) + "\n\n" +
		"data: " + string(end) + "\n\n"
	gw := &stubGateway{streamResp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(sse)),
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":  "agent",
		"stream": true,
		"input":  "run tool",
	}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	evts := parseSSEEvents(t, rec.Body.String())

	if len(eventsByType(evts, "response.output_text.delta")) != 0 {
		t.Fatalf("tool call stream must not emit response.output_text.delta: %s", rec.Body.String())
	}
	if len(eventsByType(evts, "response.function_call_arguments.delta")) < 1 {
		t.Fatalf("missing response.function_call_arguments.delta: %s", rec.Body.String())
	}
	done := eventsByType(evts, "response.function_call_arguments.done")
	if len(done) != 1 {
		t.Fatalf("missing response.function_call_arguments.done: %s", rec.Body.String())
	}
	if done[0].Data["name"] != "multi_tool_use.parallel" {
		t.Fatalf("function name=%v", done[0].Data["name"])
	}
}

func TestResponsesStreamFragmentedTagWrappedToolCallUsesFunctionEventsNotOutputText(t *testing.T) {
	fragments := []string{
		"<tool_call><multi_tool_use.parallel>",
		"{\"tool_uses\":[{\"recipient_name\":\"functions.shell_command\",",
		"\"parameters\":{\"command\":\"Get-Date\"}}]}",
		"</multi_tool_use.parallel></tool_call>",
	}
	var sse strings.Builder
	for _, part := range fragments {
		evt := map[string]any{
			"data": map[string]any{
				"message": map[string]any{"text": part},
			},
		}
		b, err := json.Marshal(evt)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		sse.WriteString("data: ")
		sse.WriteString(string(b))
		sse.WriteString("\n\n")
	}
	end, _ := json.Marshal(map[string]any{"end": true})
	sse.WriteString("data: ")
	sse.WriteString(string(end))
	sse.WriteString("\n\n")

	gw := &stubGateway{streamResp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(sse.String())),
	}}
	srv := newTestServer(gw)

	payload := map[string]any{
		"model":  "agent",
		"stream": true,
		"input":  "run tool",
	}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	evts := parseSSEEvents(t, rec.Body.String())

	if len(eventsByType(evts, "response.output_text.delta")) != 0 {
		t.Fatalf("tool call stream must not emit response.output_text.delta: %s", rec.Body.String())
	}
	if len(eventsByType(evts, "response.function_call_arguments.delta")) < 1 {
		t.Fatalf("missing response.function_call_arguments.delta: %s", rec.Body.String())
	}
	done := eventsByType(evts, "response.function_call_arguments.done")
	if len(done) != 1 {
		t.Fatalf("missing response.function_call_arguments.done: %s", rec.Body.String())
	}
	if done[0].Data["name"] != "multi_tool_use.parallel" {
		t.Fatalf("function name=%v", done[0].Data["name"])
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

func TestGlobalConcurrencyGateSeventeenthRequestWaits(t *testing.T) {
	release := make(chan struct{})
	var started int32
	gw := &stubGateway{
		runHook: func(req gateway.RunRequest) (*http.Response, map[string]any, error) {
			atomic.AddInt32(&started, 1)
			<-release
			return nil, map[string]any{
				"data": map[string]any{
					"message": map[string]any{"text": "ok"},
				},
			}, nil
		},
	}
	srv := newTestServer(gw)

	handler := srv.Handler()
	var wg sync.WaitGroup
	responses := make([]*httptest.ResponseRecorder, 17)

	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := bytes.NewBufferString(`{"model":"agent","messages":[{"role":"user","content":"hi"}]}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
			req.Header.Set("x-client-id", fmt.Sprintf("gate-chat-%d", idx))
			rec := httptest.NewRecorder()
			responses[idx] = rec
			handler.ServeHTTP(rec, req)
		}(i)
	}

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&started) < 16 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&started); got != 16 {
		t.Fatalf("started=%d want 16", got)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		body := bytes.NewBufferString(`{"model":"agent","messages":[{"role":"user","content":"hi"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
		req.Header.Set("x-client-id", "gate-chat-17")
		rec := httptest.NewRecorder()
		responses[16] = rec
		handler.ServeHTTP(rec, req)
	}()

	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt32(&started); got != 16 {
		t.Fatalf("17th request should wait, started=%d", got)
	}

	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&started); got != 17 {
		t.Fatalf("all requests should eventually run, started=%d", got)
	}
	if responses[16] == nil || responses[16].Code != http.StatusOK {
		t.Fatalf("17th status=%v", responses[16].Code)
	}
}

func TestGlobalConcurrencyGateSharedByChatAndResponses(t *testing.T) {
	release := make(chan struct{})
	started := make(chan string, 2)
	gw := &stubGateway{
		runHook: func(req gateway.RunRequest) (*http.Response, map[string]any, error) {
			started <- req.Text
			<-release
			return nil, map[string]any{
				"data": map[string]any{
					"message": map[string]any{"text": "ok"},
				},
			}, nil
		},
	}
	srv := newTestServer(gw)
	srv.globalGate = make(chan struct{}, 1)
	handler := srv.Handler()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"chat"}]}`))
		req.Header.Set("x-client-id", "shared-chat")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not start")
	}

	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"input":"resp"}`))
		req.Header.Set("x-client-id", "shared-resp")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}()

	select {
	case msg := <-started:
		t.Fatalf("second request should wait, started=%q", msg)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	wg.Wait()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("second request did not start after release")
	}
}

func TestGlobalConcurrencyGateReleasesOnError(t *testing.T) {
	releaseFirst := make(chan struct{})
	var callCount int32
	gw := &stubGateway{
		runHook: func(req gateway.RunRequest) (*http.Response, map[string]any, error) {
			n := atomic.AddInt32(&callCount, 1)
			if n == 1 {
				<-releaseFirst
				return nil, nil, errors.New("boom")
			}
			return nil, map[string]any{
				"data": map[string]any{
					"message": map[string]any{"text": "ok"},
				},
			}, nil
		},
	}
	srv := newTestServer(gw)
	srv.globalGate = make(chan struct{}, 1)
	handler := srv.Handler()

	var wg sync.WaitGroup
	firstRec := httptest.NewRecorder()
	secondRec := httptest.NewRecorder()

	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"one"}]}`))
		req.Header.Set("x-client-id", "error-first")
		handler.ServeHTTP(firstRec, req)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&callCount) < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("first request did not start")
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"two"}]}`))
		req.Header.Set("x-client-id", "error-second")
		handler.ServeHTTP(secondRec, req)
	}()

	time.Sleep(200 * time.Millisecond)
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("second request should still be waiting, callCount=%d", atomic.LoadInt32(&callCount))
	}

	close(releaseFirst)
	wg.Wait()

	if atomic.LoadInt32(&callCount) != 2 {
		t.Fatalf("callCount=%d", atomic.LoadInt32(&callCount))
	}
	if firstRec.Code != http.StatusBadGateway {
		t.Fatalf("first status=%d", firstRec.Code)
	}
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second status=%d", secondRec.Code)
	}
}
