package http

import (
	"bytes"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
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

func TestModelsEndpointReturnsBuiltinModels(t *testing.T) {
	h := NewHandler(HandlerDeps{DefaultModel: "qwen3.6"})
	req := httptest.NewRequest(stdhttp.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	data, ok := body["data"].([]any)
	if !ok {
		t.Fatal("missing data")
	}
	if len(data) != 4 {
		t.Fatalf("expected 4 builtin models, got %d", len(data))
	}
	var deepseek map[string]any
	var qwen map[string]any
	var reranker map[string]any
	var bge map[string]any
	for _, item := range data {
		model := item.(map[string]any)
		switch model["id"] {
		case "DeepSeek-V3.2":
			deepseek = model
		case "Qwen3.6-27B":
			qwen = model
		case "Qwen3-Reranker-0.6B":
			reranker = model
		case "bge-m3-1024维":
			bge = model
		}
	}
	if deepseek["context_window"] != float64(65536) {
		t.Fatalf("deepseek context_window: %#v", deepseek["context_window"])
	}
	if qwen["context_window"] != float64(262144) {
		t.Fatalf("qwen context_window: %#v", qwen["context_window"])
	}
	caps, ok := qwen["capabilities"].([]any)
	if !ok {
		t.Fatal("qwen capabilities missing")
	}
	foundVision := false
	for _, cap := range caps {
		if cap == "vision" {
			foundVision = true
			break
		}
	}
	if !foundVision {
		t.Fatal("qwen capabilities should include vision")
	}
	if reranker["vector_dim"] != float64(1024) {
		t.Fatalf("reranker vector_dim: %#v", reranker["vector_dim"])
	}
	rerankerCaps, ok := reranker["capabilities"].([]any)
	if !ok || len(rerankerCaps) != 2 || rerankerCaps[0] != "rerank" || rerankerCaps[1] != "embedding" {
		t.Fatalf("reranker capabilities: %#v", reranker["capabilities"])
	}
	if bge["vector_dim"] != float64(1024) {
		t.Fatalf("bge vector_dim: %#v", bge["vector_dim"])
	}
}

func TestEmbeddingsEndpointUsesBGEByDefault(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{
				map[string]any{
					"object":    "embedding",
					"index":     0,
					"embedding": []float64{0.1, 0.2},
				},
			},
			"usage": map[string]any{
				"prompt_tokens": 1,
				"total_tokens":  1,
			},
		})
	}))
	defer server.Close()

	h := NewHandler(HandlerDeps{
		Client: gateway.NewClient(config.Config{
			BaseURL: server.URL + "/gateway/compatible-mode",
			AppKey:  "test-key",
			Mode:    config.BackendModeCompatible,
			ChatURL: server.URL + "/gateway/compatible-mode/v1/chat/completions",
		}, server.Client()),
	})

	req := httptest.NewRequest(stdhttp.MethodPost, "/v1/embeddings", bytes.NewBufferString(`{"input":"向量测试"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotPath != "/gateway/ai_version/rsv-tchvlgrj/v1/embeddings" {
		t.Fatalf("path: %s", gotPath)
	}
	if gotBody["input"] != "向量测试" {
		t.Fatalf("input: %#v", gotBody["input"])
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "rsv-tchvlgrj" {
		t.Fatalf("model: %#v", body["model"])
	}
}

func TestRerankEndpointUsesQwenRerankerByDefault(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{
				map[string]any{
					"index":           0,
					"relevance_score": 0.99,
					"document":        "北京今天晴，适合出行",
				},
			},
		})
	}))
	defer server.Close()

	h := NewHandler(HandlerDeps{
		Client: gateway.NewClient(config.Config{
			BaseURL: server.URL + "/gateway/compatible-mode",
			AppKey:  "test-key",
			Mode:    config.BackendModeCompatible,
			ChatURL: server.URL + "/gateway/compatible-mode/v1/chat/completions",
		}, server.Client()),
	})

	req := httptest.NewRequest(stdhttp.MethodPost, "/v1/rerank", bytes.NewBufferString(`{"query":"北京天气","documents":["北京今天晴，适合出行","上海今天有雨，注意带伞"],"top_n":2}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotPath != "/gateway/ai_version/rsv-11m4dmp2/v1/rerank" {
		t.Fatalf("path: %s", gotPath)
	}
	input := gotBody["input"].(map[string]any)
	if input["query"] != "北京天气" {
		t.Fatalf("query: %#v", input["query"])
	}
	docs := input["documents"].([]any)
	if len(docs) != 2 {
		t.Fatalf("documents len: %d", len(docs))
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "rsv-11m4dmp2" {
		t.Fatalf("model: %#v", body["model"])
	}
	results := body["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results len: %d", len(results))
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

func TestCompatibleChatCompletionsPassThrough(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path != "/gateway/compatible-mode/v1/chat/completions" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_1",
			"object":  "chat.completion",
			"created": 1,
			"model":   "qwen-7b",
			"choices": []any{
				map[string]any{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "ok",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	h := NewHandler(HandlerDeps{
		Client: gateway.NewClient(config.Config{
			BaseURL: server.URL + "/gateway/compatible-mode",
			AppKey:  "test-key",
			Mode:    config.BackendModeCompatible,
			ChatURL: server.URL + "/gateway/compatible-mode/v1/chat/completions",
		}, server.Client()),
		DefaultModel: "agent",
	})

	req := httptest.NewRequest(stdhttp.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"qwen-7b","messages":[{"role":"user","content":"你好"}],"stream":false,"temperature":0.9}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotBody["temperature"] != 0.9 {
		t.Fatalf("temperature: %#v", gotBody["temperature"])
	}
}

func TestCompatibleChatCompletionsLocalToolShim(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		callCount++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if callCount == 1 {
			if _, ok := payload["tools"]; ok {
				t.Fatal("planning request should not forward tools upstream")
			}
			if _, ok := payload["tool_choice"]; ok {
				t.Fatal("planning request should not forward tool_choice upstream")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl_plan",
				"object":  "chat.completion",
				"created": 1,
				"model":   "Qwen3.6-27B",
				"choices": []any{
					map[string]any{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": `{"need_tool":true,"tool_name":"get_weather","arguments":{"location":"北京"},"final_answer":""}`,
						},
						"finish_reason": "stop",
					},
				},
			})
			return
		}
		t.Fatalf("unexpected extra upstream call: %d", callCount)
	}))
	defer server.Close()

	h := NewHandler(HandlerDeps{
		Client: gateway.NewClient(config.Config{
			BaseURL: server.URL + "/gateway/compatible-mode",
			AppKey:  "test-key",
			Mode:    config.BackendModeCompatible,
			ChatURL: server.URL + "/gateway/compatible-mode/v1/chat/completions",
		}, server.Client()),
		DefaultModel: "agent",
	})

	req := httptest.NewRequest(stdhttp.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"rsv-q23123sde","messages":[{"role":"user","content":"查北京天气"}],"stream":false,"tool_choice":"auto","tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	choices := body["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if _, ok := msg["tool_calls"]; !ok {
		t.Fatal("expected tool_calls in response")
	}
}

func TestCompatibleChatCompletionsLocalToolShimNoTool(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload["tools"]; ok {
			t.Fatal("planning request should not forward tools upstream")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_plan",
			"object":  "chat.completion",
			"created": 1,
			"model":   "Qwen3.6-27B",
			"choices": []any{
				map[string]any{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "```json\n{\"need_tool\":false,\"tool_name\":\"\",\"arguments\":{},\"final_answer\":\"直接回答\"}\n```",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	h := NewHandler(HandlerDeps{
		Client: gateway.NewClient(config.Config{
			BaseURL: server.URL + "/gateway/compatible-mode",
			AppKey:  "test-key",
			Mode:    config.BackendModeCompatible,
			ChatURL: server.URL + "/gateway/compatible-mode/v1/chat/completions",
		}, server.Client()),
		DefaultModel: "agent",
	})

	req := httptest.NewRequest(stdhttp.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"Qwen3.6-27B","messages":[{"role":"user","content":"直接回答我"}],"stream":false,"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	choices := body["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "直接回答" {
		t.Fatalf("content: %#v", msg["content"])
	}
	if _, ok := msg["tool_calls"]; ok {
		t.Fatal("did not expect tool_calls")
	}
}

func TestCompatibleChatCompletionsLocalToolShimStream(t *testing.T) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload["tools"]; ok {
			t.Fatal("planning request should not forward tools upstream")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_plan",
			"object":  "chat.completion",
			"created": 1,
			"model":   "Qwen3.6-27B",
			"choices": []any{
				map[string]any{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"need_tool":true,"tool_name":"get_weather","arguments":{"location":"北京"},"final_answer":""}`,
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	h := NewHandler(HandlerDeps{
		Client: gateway.NewClient(config.Config{
			BaseURL: server.URL + "/gateway/compatible-mode",
			AppKey:  "test-key",
			Mode:    config.BackendModeCompatible,
			ChatURL: server.URL + "/gateway/compatible-mode/v1/chat/completions",
		}, server.Client()),
		DefaultModel: "agent",
	})

	req := httptest.NewRequest(stdhttp.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"rsv-q23123sde","messages":[{"role":"user","content":"查北京天气"}],"stream":true,"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"tool_calls"`) {
		t.Fatalf("expected tool_calls in stream body: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Fatalf("expected tool_calls finish reason in stream body: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected DONE marker in stream body: %s", body)
	}
}

func TestCompatibleChatCompletionsRealToolRoundTrip(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		callCount++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload["tools"]; ok {
			t.Fatal("planning request should not forward tools upstream")
		}

		msgs, _ := payload["messages"].([]any)
		if callCount == 1 {
			if len(msgs) != 2 {
				t.Fatalf("first planning messages len: %d", len(msgs))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl_plan_1",
				"object":  "chat.completion",
				"created": 1,
				"model":   "Qwen3.6-27B",
				"choices": []any{
					map[string]any{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": `{"need_tool":true,"tool_name":"get_weather","arguments":{"location":"北京"},"final_answer":""}`,
						},
						"finish_reason": "stop",
					},
				},
			})
			return
		}
		if callCount == 2 {
			raw, _ := json.Marshal(msgs)
			text := string(raw)
			if !strings.Contains(text, "晴，26C") {
				t.Fatalf("second planning request should include tool result, got: %s", text)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl_plan_2",
				"object":  "chat.completion",
				"created": 2,
				"model":   "Qwen3.6-27B",
				"choices": []any{
					map[string]any{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": `{"need_tool":false,"tool_name":"","arguments":{},"final_answer":"北京当前晴，26C。"}`,
						},
						"finish_reason": "stop",
					},
				},
			})
			return
		}
		t.Fatalf("unexpected upstream call count: %d", callCount)
	}))
	defer server.Close()

	h := NewHandler(HandlerDeps{
		Client: gateway.NewClient(config.Config{
			BaseURL: server.URL + "/gateway/compatible-mode",
			AppKey:  "test-key",
			Mode:    config.BackendModeCompatible,
			ChatURL: server.URL + "/gateway/compatible-mode/v1/chat/completions",
		}, server.Client()),
		DefaultModel: "agent",
	})

	firstReq := httptest.NewRequest(stdhttp.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"rsv-q23123sde","messages":[{"role":"user","content":"查北京天气"}],"stream":false,"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}]}`))
	firstReq.Header.Set("Content-Type", "application/json")
	firstRec := httptest.NewRecorder()
	h.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != stdhttp.StatusOK {
		t.Fatalf("first round expected 200, got %d", firstRec.Code)
	}

	var firstBody map[string]any
	if err := json.NewDecoder(firstRec.Body).Decode(&firstBody); err != nil {
		t.Fatal(err)
	}
	choices := firstBody["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	toolCalls := msg["tool_calls"].([]any)
	toolCall := toolCalls[0].(map[string]any)
	fn := toolCall["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Fatalf("tool name: %#v", fn["name"])
	}

	toolResult := "北京当前晴，26C"
	secondPayload := map[string]any{
		"model":  "rsv-q23123sde",
		"stream": false,
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":       "get_weather",
					"parameters": map[string]any{"type": "object"},
				},
			},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": "查北京天气"},
			map[string]any{
				"role":       "assistant",
				"content":    "",
				"tool_calls": toolCalls,
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": toolCall["id"],
				"content":      toolResult,
			},
		},
	}
	secondData, _ := json.Marshal(secondPayload)
	secondReq := httptest.NewRequest(stdhttp.MethodPost, "/v1/chat/completions", bytes.NewReader(secondData))
	secondReq.Header.Set("Content-Type", "application/json")
	secondRec := httptest.NewRecorder()
	h.ServeHTTP(secondRec, secondReq)
	if secondRec.Code != stdhttp.StatusOK {
		t.Fatalf("second round expected 200, got %d", secondRec.Code)
	}

	var secondBody map[string]any
	if err := json.NewDecoder(secondRec.Body).Decode(&secondBody); err != nil {
		t.Fatal(err)
	}
	secondChoices := secondBody["choices"].([]any)
	secondMsg := secondChoices[0].(map[string]any)["message"].(map[string]any)
	if secondMsg["content"] != "北京当前晴，26C。" {
		t.Fatalf("final answer: %#v", secondMsg["content"])
	}
	if secondChoices[0].(map[string]any)["finish_reason"] != "stop" {
		t.Fatalf("finish_reason: %#v", secondChoices[0].(map[string]any)["finish_reason"])
	}
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

func TestWithAuthDisabledWhenTokenEmpty(t *testing.T) {
	// 无 token：鉴权完全关闭，/v1/* 也能直接访问（向后兼容）。
	h := NewHandler(HandlerDeps{DefaultModel: "agent", AuthToken: ""})
	req := httptest.NewRequest(stdhttp.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200 when auth disabled, got %d", rec.Code)
	}
}

func TestWithAuthRejectsMissingToken(t *testing.T) {
	h := NewHandler(HandlerDeps{DefaultModel: "agent", AuthToken: "secret"})
	req := httptest.NewRequest(stdhttp.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
	assertErrorCode(t, rec.Body.Bytes(), unauthorizedCode)
}

func TestWithAuthRejectsWrongToken(t *testing.T) {
	h := NewHandler(HandlerDeps{DefaultModel: "agent", AuthToken: "secret"})
	req := httptest.NewRequest(stdhttp.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", rec.Code)
	}
}

func TestWithAuthAcceptsCorrectToken(t *testing.T) {
	h := NewHandler(HandlerDeps{DefaultModel: "agent", AuthToken: "secret"})
	req := httptest.NewRequest(stdhttp.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200 with correct token, got %d", rec.Code)
	}
}

func TestWithAuthAcceptsCorrectTokenCaseInsensitiveScheme(t *testing.T) {
	// "bearer" 小写前缀也应被接受（RFC 允许大小写不敏感）。
	h := NewHandler(HandlerDeps{DefaultModel: "agent", AuthToken: "secret"})
	req := httptest.NewRequest(stdhttp.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200 with lowercase bearer scheme, got %d", rec.Code)
	}
}

func TestWithAuthDoesNotProtectNonV1Paths(t *testing.T) {
	// /health 不在 /v1/* 下，即使开启了鉴权也不应被拦截。
	h := NewHandler(HandlerDeps{DefaultModel: "agent", AuthToken: "secret"})
	req := httptest.NewRequest(stdhttp.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("expected /health to bypass auth (200), got %d", rec.Code)
	}
}

func TestMaskKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "****"},
		{"abc", "****"},
		{"12345678", "****"},          // 恰好 8 位，不暴露
		{"123456789", "1234****6789"}, // 9 位：前4+后4
		{"FRQ6udhCtWPb4VpIWnA3WLBwZ3K84qKO", "FRQ6****4qKO"},
		{"  FRQ6udhCtWPb4VpIWnA3WLBwZ3K84qKO  ", "FRQ6****4qKO"}, // 自动 trim
	}
	for _, c := range cases {
		if got := maskKey(c.in); got != c.want {
			t.Fatalf("maskKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
