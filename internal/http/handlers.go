package http

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	stdhttp "net/http"
	"os"
	"strings"
	"time"

	"github.com/lim12137/jtptllm/internal/gateway"
	"github.com/lim12137/jtptllm/internal/openai"
	"github.com/lim12137/jtptllm/internal/session"
)

const (
	emptyAgentResponseCode = "empty_agent_response"
	unauthorizedCode       = "unauthorized"
)

type HandlerDeps struct {
	Client       *gateway.Client
	Sessions     *session.Manager
	DefaultModel string
	AuthToken    string
}

func NewHandler(deps HandlerDeps) stdhttp.Handler {
	model := strings.TrimSpace(deps.DefaultModel)
	if model == "" {
		model = "agent"
	}

	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/health", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/model", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, stdhttp.StatusOK, map[string]any{"model": openai.DisplayModelName(model)})
	})
	mux.HandleFunc("/v1/models", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodGet {
			methodNotAllowed(w)
			return
		}
		models := openai.BuiltinModels()
		data := make([]any, 0, len(models))
		for _, spec := range models {
			data = append(data, map[string]any{
				"id":             spec.Name,
				"object":         "model",
				"name":           spec.Name,
				"owned_by":       "jtptllm",
				"context_window": spec.ContextWindow,
				"capabilities":   spec.Capabilities,
				"vector_dim":     spec.VectorDim,
			})
		}
		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"object": "list",
			"data":   data,
		})
	})
	mux.HandleFunc("/v1/chat/completions", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodPost {
			methodNotAllowed(w)
			return
		}
		handleChatCompletions(w, r, deps, model)
	})
	mux.HandleFunc("/v1/embeddings", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodPost {
			methodNotAllowed(w)
			return
		}
		handleEmbeddings(w, r, deps)
	})
	mux.HandleFunc("/v1/rerank", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodPost {
			methodNotAllowed(w)
			return
		}
		handleRerank(w, r, deps)
	})

	return withAuth(deps.AuthToken, withCORS(mux))
}

// withAuth 在内层对 /v1/* 路径强制 Bearer token 鉴权。
// token 为空时鉴权完全关闭，保持向后兼容（本地调试无需改调用方）。
// /health、/model 等非 /v1/* 路径不做鉴权，方便健康检查。
func withAuth(token string, next stdhttp.Handler) stdhttp.Handler {
	token = strings.TrimSpace(token)
	if token == "" {
		return next
	}
	expected := []byte(token)
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		provided := extractBearerToken(r.Header.Get("Authorization"))
		if len(provided) == 0 || subtle.ConstantTimeCompare(provided, expected) != 1 {
			writeJSON(w, stdhttp.StatusUnauthorized, map[string]any{
				"error": map[string]any{
					"type":    unauthorizedCode,
					"code":    unauthorizedCode,
					"message": "missing or invalid Authorization: Bearer <token>",
				},
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractBearerToken(header string) []byte {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return nil
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return nil
	}
	return []byte(token)
}

// maskKey 把网关 key 脱敏为「前4 + **** + 后4」形式，避免明文进入日志/错误。
func maskKey(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

func withCORS(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, x-agent-session, x-client-id, x-agent-session-reset, x-agent-session-close")

		if r.Method == stdhttp.MethodOptions {
			w.WriteHeader(stdhttp.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleChatCompletions(w stdhttp.ResponseWriter, r *stdhttp.Request, deps HandlerDeps, defaultModel string) {
	payload, err := decodeJSON(r)
	if err != nil {
		writeError(w, stdhttp.StatusBadRequest, err.Error())
		return
	}
	if deps.Client != nil && deps.Client.IsCompatibleMode() {
		proxyCompatibleChatCompletions(w, r, deps, payload)
		return
	}
	parsed := openai.ParseChatRequest(payload)
	if strings.TrimSpace(parsed.Prompt) == "" {
		writeError(w, stdhttp.StatusBadRequest, "messages 为空，无法生成 prompt")
		return
	}
	model := parsed.Model
	if model == "" {
		model = defaultModel
	}

	sessionID, deleteOnFinish, key, err := getSession(r.Context(), r, deps, headerBool(r, "x-agent-session-reset"))
	if err != nil {
		writeGatewayError(w, err)
		return
	}
	closeAfter := headerBool(r, "x-agent-session-close")
	defer cleanupSession(r.Context(), deps, sessionID, deleteOnFinish, key, closeAfter)

	if !parsed.Stream {
		_, runResp, err := deps.Client.Run(r.Context(), gateway.RunRequest{
			SessionID: sessionID,
			Text:      parsed.Prompt,
			Stream:    false,
			Delta:     true,
		})
		if err != nil {
			writeGatewayError(w, err)
			return
		}
		text := extractGatewayTextFromNonStream(runResp)
		if strings.TrimSpace(text) == "" {
			writeGatewayErrorCode(w, stdhttp.StatusBadGateway, emptyAgentResponseCode, "agent 返回为空")
			return
		}
		usage := openai.BuildUsage(parsed.Prompt, text, model)
		writeJSON(w, stdhttp.StatusOK, openai.BuildChatCompletionResponseWithUsage(text, model, usage))
		return
	}

	resp, _, err := deps.Client.Run(r.Context(), gateway.RunRequest{
		SessionID: sessionID,
		Text:      parsed.Prompt,
		Stream:    true,
		Delta:     true,
	})
	if err != nil {
		writeGatewayError(w, err)
		return
	}
	if err := streamChatCompletion(w, resp, model, parsed.Prompt); err != nil {
		if deps.Sessions != nil {
			deps.Sessions.Invalidate(key)
		}
	}
}

func proxyCompatibleChatCompletions(w stdhttp.ResponseWriter, r *stdhttp.Request, deps HandlerDeps, payload map[string]any) {
	model := openai.StrOrModel(payload["model"], "agent")
	payload["model"] = model
	useToolShim := openai.ShouldUseLocalToolShim(model, payload)
	if os.Getenv("SHIM_DUMP") == "1" {
		toolCount := 0
		if tools, ok := payload["tools"].([]any); ok {
			toolCount = len(tools)
		}
		data, _ := json.MarshalIndent(map[string]any{
			"model":         model,
			"tool_count":    toolCount,
			"use_tool_shim": useToolShim,
		}, "", "  ")
		_ = os.WriteFile("shim_route_dump.json", data, 0o644)
	}
	if useToolShim {
		handleLocalToolShimChatCompletions(w, r, deps, payload, model)
		return
	}
	stream := boolOr(payload["stream"], false)
	resp, body, err := deps.Client.ChatCompletions(r.Context(), payload, stream)
	if err != nil {
		writeGatewayError(w, err)
		return
	}
	if stream {
		writeStreamProxyResponse(w, resp)
		return
	}
	writeRawJSONResponse(w, resp, body)
}

func handleLocalToolShimChatCompletions(w stdhttp.ResponseWriter, r *stdhttp.Request, deps HandlerDeps, payload map[string]any, model string) {
	planningReq := openai.BuildToolPlanningChatRequest(payload)
	if openai.IsToolPlanningRequest(planningReq) {
		if openai.DebugPlanningMetrics(planningReq, payload) {
			// 仅在 CODE_AGENT_DEBUG=1 时由内部打日志
		}
	}
	resp, body, err := deps.Client.ChatCompletions(r.Context(), planningReq, false)
	if err != nil {
		writeGatewayError(w, err)
		return
	}
	if resp != nil {
		defer resp.Body.Close()
	}

	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		writeGatewayError(w, err)
		return
	}
	text := openai.ExtractTextFromChatCompletionResponse(out)
	plan, err := openai.ParseToolPlan(text)
	if err != nil {
		writeGatewayErrorCode(w, stdhttp.StatusBadGateway, "tool_plan_parse_error", err.Error())
		return
	}

	// 工具循环兜底：若已判定为循环/超限，即使 planner 仍返回 need_tool=true 也强制改写为总结。
	rawMsgs := openai.AsRawMessages(payload["messages"])
	if decision := openai.AnalyzeToolLoop(rawMsgs); decision.ForceStop {
		plan = openai.ForceStopPlan(plan, rawMsgs, decision.Reason)
	}

	if boolOr(payload["stream"], false) {
		writeSSEStrings(w, openai.IterToolCallChatCompletionSSE(plan, model))
		return
	}
	writeJSON(w, stdhttp.StatusOK, openai.BuildToolCallChatCompletionResponse(plan, model))
}

func handleEmbeddings(w stdhttp.ResponseWriter, r *stdhttp.Request, deps HandlerDeps) {
	payload, err := decodeJSON(r)
	if err != nil {
		writeError(w, stdhttp.StatusBadRequest, err.Error())
		return
	}
	if msg := openai.ValidateEmbeddingsPayload(payload); msg != "" {
		writeError(w, stdhttp.StatusBadRequest, msg)
		return
	}
	modelID := openai.EmbeddingModelID(openai.StrOrModel(payload["model"], "embedding"))
	resp, body, err := deps.Client.Embeddings(r.Context(), modelID, openai.BuildEmbeddingsRequest(payload))
	if err != nil {
		writeGatewayError(w, err)
		return
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		writeGatewayErrorCode(w, stdhttp.StatusBadGateway, "embedding_decode_error", err.Error())
		return
	}
	writeJSON(w, stdhttp.StatusOK, openai.BuildEmbeddingsResponse(out, modelID))
}

func handleRerank(w stdhttp.ResponseWriter, r *stdhttp.Request, deps HandlerDeps) {
	payload, err := decodeJSON(r)
	if err != nil {
		writeError(w, stdhttp.StatusBadRequest, err.Error())
		return
	}
	if msg := openai.ValidateRerankPayload(payload); msg != "" {
		writeError(w, stdhttp.StatusBadRequest, msg)
		return
	}
	modelID := openai.RerankModelID(openai.StrOrModel(payload["model"], "rerank"))
	resp, body, err := deps.Client.Rerank(r.Context(), modelID, openai.BuildRerankRequest(payload))
	if err != nil {
		writeGatewayError(w, err)
		return
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		writeGatewayErrorCode(w, stdhttp.StatusBadGateway, "rerank_decode_error", err.Error())
		return
	}
	writeJSON(w, stdhttp.StatusOK, openai.BuildRerankResponse(out, modelID))
}

func getSession(ctx context.Context, r *stdhttp.Request, deps HandlerDeps, forceNew bool) (string, bool, string, error) {
	if deps.Client == nil {
		return "", false, "", errors.New("gateway client not configured")
	}
	key := sessionKey(r)
	if deps.Sessions == nil {
		id, err := deps.Client.CreateSession(ctx)
		return id, true, key, err
	}
	if forceNew {
		deps.Sessions.Invalidate(key)
	}
	id := deps.Sessions.Get(key)
	if id == "" {
		var err error
		id, err = deps.Client.CreateSession(ctx)
		if err != nil {
			return "", false, key, err
		}
		deps.Sessions.Set(key, id)
		return id, false, key, nil
	}
	deps.Sessions.Set(key, id)
	return id, false, key, nil
}

func cleanupSession(ctx context.Context, deps HandlerDeps, sessionID string, deleteOnFinish bool, key string, closeAfter bool) {
	if sessionID == "" {
		return
	}
	if deleteOnFinish {
		_ = deps.Client.DeleteSession(ctx, sessionID)
		return
	}
	if closeAfter && deps.Sessions != nil {
		deps.Sessions.Invalidate(key)
	}
}

func streamChatCompletion(w stdhttp.ResponseWriter, resp *stdhttp.Response, model string, prompt string) error {
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(stdhttp.StatusOK)

	flusher, ok := w.(stdhttp.Flusher)
	if !ok {
		return errors.New("streaming unsupported")
	}

	created := time.Now().Unix()
	cid := newID("chatcmpl")
	first := map[string]any{
		"id":      cid,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil}},
	}
	_, _ = w.Write([]byte(sseData(first)))
	flusher.Flush()

	full := ""
	err := streamGateway(resp, func(chunk string) error {
		delta := diffChunk(&full, chunk)
		if delta == "" {
			return nil
		}
		payload := map[string]any{
			"id":      cid,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": delta}, "finish_reason": nil}},
		}
		_, _ = w.Write([]byte(sseData(payload)))
		flusher.Flush()
		return nil
	})
	if err != nil {
		return err
	}

	// 流式结束时，按完整文本估算 usage 并附在最终 chunk 上（OpenAI 在 finish chunk 携带 usage）。
	final := map[string]any{
		"id":      cid,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		"usage":   openai.BuildUsage(prompt, full, model),
	}
	_, _ = w.Write([]byte(sseData(final)))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
	return nil
}

func streamGateway(resp *stdhttp.Response, onChunk func(string) error) error {
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if line == "" {
			continue
		}
		if line == "[DONE]" {
			break
		}
		var evt map[string]any
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		if isGatewayEndEvent(evt) {
			break
		}
		chunk := extractGatewayTextDelta(evt)
		if chunk == "" {
			continue
		}
		if err := onChunk(chunk); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func extractGatewayTextFromNonStream(runResp map[string]any) string {
	if runResp == nil {
		return ""
	}
	if data, ok := runResp["data"].(map[string]any); ok {
		if msg, ok := data["message"].(map[string]any); ok {
			return extractTextFromMessage(msg)
		}
		return extractTextFromMessage(data)
	}
	return extractTextFromMessage(runResp)
}

func extractTextFromMessage(msg map[string]any) string {
	content := msg["content"]
	if list, ok := content.([]any); ok {
		var parts []string
		for _, c := range list {
			if cobj, ok := c.(map[string]any); ok {
				parts = append(parts, extractTextFromContentObj(cobj))
			}
		}
		return strings.Join(parts, "")
	}
	if cobj, ok := content.(map[string]any); ok {
		return extractTextFromContentObj(cobj)
	}
	if text, ok := msg["text"].(string); ok {
		return text
	}
	return ""
}

func extractGatewayTextDelta(evt map[string]any) string {
	if data, ok := evt["data"].(map[string]any); ok {
		if _, ok := data["content"]; ok {
			evt = data
		}
	}
	content := evt["content"]
	if list, ok := content.([]any); ok {
		var parts []string
		for _, c := range list {
			if cobj, ok := c.(map[string]any); ok {
				parts = append(parts, extractTextFromContentObj(cobj))
			}
		}
		return strings.Join(parts, "")
	}
	if cobj, ok := content.(map[string]any); ok {
		return extractTextFromContentObj(cobj)
	}
	return ""
}

func extractTextFromContentObj(content map[string]any) string {
	if t, ok := content["type"].(string); ok && t != "text" {
		return ""
	}
	switch v := content["text"].(type) {
	case string:
		return v
	case map[string]any:
		if s, ok := v["value"].(string); ok {
			return s
		}
	}
	return ""
}

func isGatewayEndEvent(evt map[string]any) bool {
	if v, ok := evt["end"].(bool); ok && v {
		return true
	}
	if data, ok := evt["data"].(map[string]any); ok {
		if v, ok := data["end"].(bool); ok && v {
			return true
		}
	}
	return false
}

func sessionKey(r *stdhttp.Request) string {
	if v := strings.TrimSpace(r.Header.Get("x-agent-session")); v != "" {
		return "hdr:" + v
	}
	if v := strings.TrimSpace(r.Header.Get("x-client-id")); v != "" {
		return "cid:" + v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		host = r.RemoteAddr
	}
	if strings.TrimSpace(host) == "" {
		host = "unknown"
	}
	return "ip:" + host
}

func headerBool(r *stdhttp.Request, name string) bool {
	v := strings.TrimSpace(strings.ToLower(r.Header.Get(name)))
	switch v {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func boolOr(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func decodeJSON(r *stdhttp.Request) (map[string]any, error) {
	dec := json.NewDecoder(r.Body)
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return nil, errors.New("invalid json")
	}
	return payload, nil
}

func writeJSON(w stdhttp.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w stdhttp.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
		},
	})
}

func writeGatewayError(w stdhttp.ResponseWriter, err error) {
	writeGatewayErrorCode(w, stdhttp.StatusBadGateway, "agent_gateway_error", err.Error())
}

func writeGatewayErrorCode(w stdhttp.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"type":    code,
			"code":    code,
			"message": message,
		},
	})
}

func methodNotAllowed(w stdhttp.ResponseWriter) {
	w.WriteHeader(stdhttp.StatusMethodNotAllowed)
}

func writeRawJSONResponse(w stdhttp.ResponseWriter, resp *stdhttp.Response, body []byte) {
	if resp != nil {
		defer resp.Body.Close()
		if ct := strings.TrimSpace(resp.Header.Get("Content-Type")); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(resp.StatusCode)
	}
	_, _ = w.Write(body)
}

func writeStreamProxyResponse(w stdhttp.ResponseWriter, resp *stdhttp.Response) {
	defer resp.Body.Close()
	if ct := strings.TrimSpace(resp.Header.Get("Content-Type")); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func writeSSEStrings(w stdhttp.ResponseWriter, events []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(stdhttp.StatusOK)
	for _, evt := range events {
		_, _ = w.Write([]byte(evt))
	}
}

func diffChunk(full *string, chunk string) string {
	if chunk == "" {
		return ""
	}
	if *full != "" && strings.HasPrefix(chunk, *full) {
		delta := chunk[len(*full):]
		*full = chunk
		return delta
	}
	*full = *full + chunk
	return chunk
}

func sseData(obj map[string]any) string {
	b, _ := json.Marshal(obj)
	return "data: " + string(b) + "\n\n"
}

func newID(prefix string) string {
	return prefix + "_" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "")
}
