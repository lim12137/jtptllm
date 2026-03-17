package http

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	stdhttp "net/http"
	"strings"
	"time"

	"github.com/lim12137/jtptllm/internal/gateway"
	"github.com/lim12137/jtptllm/internal/openai"
	"github.com/lim12137/jtptllm/internal/session"
)

type HandlerDeps struct {
	Client       *gateway.Client
	Sessions     *session.Manager
	DefaultModel string
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
		writeJSON(w, stdhttp.StatusOK, map[string]any{"model": model})
	})
	mux.HandleFunc("/v1/models", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"object": "list",
			"data": []any{
				map[string]any{"id": model, "object": "model"},
			},
		})
	})
	mux.HandleFunc("/v1/chat/completions", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodPost {
			methodNotAllowed(w)
			return
		}
		handleChatCompletions(w, r, deps, model)
	})
	mux.HandleFunc("/v1/responses", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodPost {
			methodNotAllowed(w)
			return
		}
		handleResponses(w, r, deps, model)
	})

	return withCORS(mux)
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
		writeJSON(w, stdhttp.StatusOK, openai.BuildChatCompletionResponse(text, model))
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
	if err := streamChatCompletion(w, resp, model); err != nil {
		if deps.Sessions != nil {
			deps.Sessions.Invalidate(key)
		}
	}
}

func handleResponses(w stdhttp.ResponseWriter, r *stdhttp.Request, deps HandlerDeps, defaultModel string) {
	payload, err := decodeJSON(r)
	if err != nil {
		writeError(w, stdhttp.StatusBadRequest, err.Error())
		return
	}
	parsed := openai.ParseResponsesRequest(payload)
	if strings.TrimSpace(parsed.Prompt) == "" {
		writeError(w, stdhttp.StatusBadRequest, "input/messages 为空，无法生成 prompt")
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
		writeJSON(w, stdhttp.StatusOK, openai.BuildResponsesResponse(text, model))
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
	if err := streamResponses(w, resp, model); err != nil {
		if deps.Sessions != nil {
			deps.Sessions.Invalidate(key)
		}
	}
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

func streamChatCompletion(w stdhttp.ResponseWriter, resp *stdhttp.Response, model string) error {
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

	final := map[string]any{
		"id":      cid,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
	}
	_, _ = w.Write([]byte(sseData(final)))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
	return nil
}

func streamResponses(w stdhttp.ResponseWriter, resp *stdhttp.Response, model string) error {
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
	rid := newID("resp")
	createdEvt := map[string]any{
		"type":     "response.created",
		"response": map[string]any{"id": rid, "model": model, "created_at": created},
	}
	_, _ = w.Write([]byte(sseData(createdEvt)))
	flusher.Flush()

	full := ""
	err := streamGateway(resp, func(chunk string) error {
		delta := diffChunk(&full, chunk)
		if delta == "" {
			return nil
		}
		payload := map[string]any{
			"type":        "response.output_text.delta",
			"delta":       delta,
			"response_id": rid,
		}
		_, _ = w.Write([]byte(sseData(payload)))
		flusher.Flush()
		return nil
	})
	if err != nil {
		return err
	}

	final := map[string]any{"type": "response.completed", "response_id": rid}
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
	writeJSON(w, stdhttp.StatusBadGateway, map[string]any{
		"error": map[string]any{
			"type":    "agent_gateway_error",
			"message": err.Error(),
		},
	})
}

func methodNotAllowed(w stdhttp.ResponseWriter) {
	w.WriteHeader(stdhttp.StatusMethodNotAllowed)
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