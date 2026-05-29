package http

import (
	"bytes"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/lim12137/jtptllm/internal/config"
	"github.com/lim12137/jtptllm/internal/gateway"
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

func TestChatCompletionsReturnsErrorOnEmptyAgentResponse(t *testing.T) {
	h := NewHandler(HandlerDeps{
		Client:       newTestGatewayClient(t, map[string]any{"data": map[string]any{"message": map[string]any{"content": []any{}}}}),
		DefaultModel: "agent",
	})

	req := httptest.NewRequest(stdhttp.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"agent","messages":[{"role":"user","content":"你好"}],"stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	assertErrorCode(t, rec.Body.Bytes(), emptyAgentResponseCode)
}

func TestResponsesReturnsErrorOnEmptyAgentResponse(t *testing.T) {
	h := NewHandler(HandlerDeps{
		Client:       newTestGatewayClient(t, map[string]any{"data": map[string]any{"message": map[string]any{"text": "   "}}}),
		DefaultModel: "agent",
	})

	req := httptest.NewRequest(stdhttp.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"agent","input":"你好","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	assertErrorCode(t, rec.Body.Bytes(), emptyAgentResponseCode)
}

func newTestGatewayClient(t *testing.T, runResponse map[string]any) *gateway.Client {
	t.Helper()

	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/createSession":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data":    map[string]any{"uniqueCode": "session-test"},
			})
		case "/run":
			_ = json.NewEncoder(w).Encode(runResponse)
		default:
			w.WriteHeader(stdhttp.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return gateway.NewClient(config.Config{
		BaseURL:   server.URL,
		AppKey:    "test-key",
		AgentCode: "agent-code",
	}, server.Client())
}

func assertErrorCode(t *testing.T, body []byte, expected string) {
	t.Helper()

	var resp struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&resp); err != nil && err != io.EOF {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error.Code != expected {
		t.Fatalf("expected error.code=%q, got %q", expected, resp.Error.Code)
	}
	if resp.Error.Type != expected {
		t.Fatalf("expected error.type=%q, got %q", expected, resp.Error.Type)
	}
}
