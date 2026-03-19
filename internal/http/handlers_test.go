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
	"sync"
	"sync/atomic"
	"strings"
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
