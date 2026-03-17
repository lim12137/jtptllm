package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lim12137/jtptllm/internal/gateway"
	"github.com/lim12137/jtptllm/internal/session"
)

type stubGateway struct {
	createCount int
	deleteCount int
	lastRun     gateway.RunRequest
	runResp     map[string]any
}

func (s *stubGateway) CreateSession(ctx context.Context) (string, error) {
	s.createCount++
	return "s1", nil
}

func (s *stubGateway) Run(ctx context.Context, req gateway.RunRequest) (*http.Response, map[string]any, error) {
	s.lastRun = req
	return nil, s.runResp, nil
}

func (s *stubGateway) DeleteSession(ctx context.Context, sessionID string) error {
	s.deleteCount++
	return nil
}

func newTestServer(gw Gateway) *Server {
	return NewServer(gw, session.NewManager(600), Options{DefaultModel: "agent"})
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
