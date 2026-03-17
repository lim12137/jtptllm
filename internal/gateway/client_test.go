package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lim12137/jtptllm/internal/config"
)

func TestCreateSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/createSession" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer abc" {
			t.Fatalf("auth: %s", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["agentCode"] != "code" {
			t.Fatal("agentCode")
		}
		if payload["agentVersion"] != "v1" {
			t.Fatal("agentVersion")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"uniqueCode":"u1"}}`))
	}))
	defer srv.Close()

	cfg := config.Config{AppKey: "abc", AgentCode: "code", AgentVersion: "v1", BaseURL: srv.URL}
	cli := NewClient(cfg, nil)
	got, err := cli.CreateSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "u1" {
		t.Fatalf("uniqueCode: %s", got)
	}
}
