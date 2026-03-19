package http

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/rand"
	"net"
	stdhttp "net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lim12137/jtptllm/internal/gateway"
	"github.com/lim12137/jtptllm/internal/openai"
	"github.com/lim12137/jtptllm/internal/session"
)

type Gateway interface {
	CreateSession(ctx context.Context) (string, error)
	Run(ctx context.Context, req gateway.RunRequest) (*stdhttp.Response, map[string]any, error)
	DeleteSession(ctx context.Context, sessionID string) error
}

type Options struct {
	DefaultModel string
}

type Server struct {
	gateway      Gateway
	sessions     *session.PoolManager
	defaultModel string
}

func NewServer(gateway Gateway, sessions *session.PoolManager, opts Options) *Server {
	model := strings.TrimSpace(opts.DefaultModel)
	if model == "" {
		model = "agent"
	}
	return &Server{
		gateway:      gateway,
		sessions:     sessions,
		defaultModel: model,
	}
}

func (s *Server) Handler() stdhttp.Handler {
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/responses", s.handleResponses)
	return withCORS(mux)
}

func (s *Server) handleHealth(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		writeJSON(w, stdhttp.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleModels(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		writeJSON(w, stdhttp.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, stdhttp.StatusOK, map[string]any{
		"object": "list",
		"data": []any{
			map[string]any{"id": "fast", "object": "model"},
			map[string]any{"id": "deepseek", "object": "model"},
			map[string]any{"id": "qingyuan", "object": "model"},
		},
	})
}

func (s *Server) handleChatCompletions(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		writeJSON(w, stdhttp.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	payload, err := decodeJSON(r.Body)
	if err != nil {
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	parsed := openai.ParseChatRequest(payload)
	if strings.TrimSpace(parsed.Prompt) == "" {
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "messages 为空，无法生成 prompt"})
		return
	}

	sessionID, release, closeAfter, sessionKey, err := s.ensureSession(r.Context(), r)
	if err != nil {
		writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer func() { release(closeAfter) }()
	logIO(map[string]any{
		"dir":         "in",
		"path":        r.URL.Path,
		"stream":      parsed.Stream,
		"session_id":  sessionID,
		"session_key": sessionKey,
		"model":       parsed.Model,
		"payload":     payload,
		"prompt":      parsed.Prompt,
	})

	if !parsed.Stream {
		_, runResp, err := s.gateway.Run(r.Context(), gateway.RunRequest{
			SessionID: sessionID,
			Text:      parsed.Prompt,
			Stream:    false,
			Delta:     false,
		})
		if err != nil {
			writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		text := extractGatewayTextFromNonStream(runResp)
		if text == "" {
			if msg, ok := gatewayRunError(runResp); ok {
				writeJSON(w, stdhttp.StatusBadGateway, openaiUpstreamError(msg))
				return
			}
		}
		respPayload := openai.BuildChatCompletionResponseFromText(text, parsed.Model)
		logIO(map[string]any{
			"dir":         "out",
			"path":        r.URL.Path,
			"stream":      false,
			"session_id":  sessionID,
			"session_key": sessionKey,
			"model":       parsed.Model,
			"gateway":     runResp,
			"output":      respPayload,
		})
		writeJSON(w, stdhttp.StatusOK, respPayload)
		return
	}

	if parsed.HasTools {
		resp, _, err := s.gateway.Run(r.Context(), gateway.RunRequest{
			SessionID: sessionID,
			Text:      parsed.Prompt,
			Stream:    true,
			Delta:     true,
		})
		if err != nil {
			writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		streamText, err := collectStreamText(resp.Body)
		if err != nil {
			writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		respPayload := openai.BuildChatCompletionResponseFromText(streamText, parsed.Model)
		logIO(map[string]any{
			"dir":           "out",
			"path":          r.URL.Path,
			"stream":        true,
			"session_id":    sessionID,
			"session_key":   sessionKey,
			"model":         parsed.Model,
			"stream_output": streamText,
			"output":        respPayload,
		})
		if err := streamChatCompletionFromResponse(w, respPayload); err != nil {
			log.Printf("stream chat completions error: %v", err)
		}
		return
	}

	resp, _, err := s.gateway.Run(r.Context(), gateway.RunRequest{
		SessionID: sessionID,
		Text:      parsed.Prompt,
		Stream:    true,
		Delta:     true,
	})
	if err != nil {
		writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	streamOutput, err := streamChatCompletion(w, resp.Body, parsed.Model)
	if err != nil {
		log.Printf("stream chat completions error: %v", err)
		return
	}
	logIO(map[string]any{
		"dir":           "out",
		"path":          r.URL.Path,
		"stream":        true,
		"session_id":    sessionID,
		"session_key":   sessionKey,
		"model":         parsed.Model,
		"stream_output": streamOutput,
	})
}

func (s *Server) handleResponses(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		writeJSON(w, stdhttp.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	payload, err := decodeJSON(r.Body)
	if err != nil {
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	parsed := openai.ParseResponsesRequest(payload)
	if strings.TrimSpace(parsed.Prompt) == "" {
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "input/messages 为空，无法生成 prompt"})
		return
	}

	sessionID, release, closeAfter, sessionKey, err := s.ensureSession(r.Context(), r)
	if err != nil {
		writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer func() { release(closeAfter) }()
	logIO(map[string]any{
		"dir":         "in",
		"path":        r.URL.Path,
		"stream":      parsed.Stream,
		"session_id":  sessionID,
		"session_key": sessionKey,
		"model":       parsed.Model,
		"payload":     payload,
		"prompt":      parsed.Prompt,
	})

	if !parsed.Stream {
		_, runResp, err := s.gateway.Run(r.Context(), gateway.RunRequest{
			SessionID: sessionID,
			Text:      parsed.Prompt,
			Stream:    false,
			Delta:     false,
		})
		if err != nil {
			writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		text := extractGatewayTextFromNonStream(runResp)
		if text == "" {
			if msg, ok := gatewayRunError(runResp); ok {
				writeJSON(w, stdhttp.StatusBadGateway, openaiUpstreamError(msg))
				return
			}
		}
		respPayload := openai.BuildResponsesResponseFromText(text, parsed.Model)
		logIO(map[string]any{
			"dir":         "out",
			"path":        r.URL.Path,
			"stream":      false,
			"session_id":  sessionID,
			"session_key": sessionKey,
			"model":       parsed.Model,
			"gateway":     runResp,
			"output":      respPayload,
		})
		writeJSON(w, stdhttp.StatusOK, respPayload)
		return
	}

	if parsed.HasTools {
		resp, _, err := s.gateway.Run(r.Context(), gateway.RunRequest{
			SessionID: sessionID,
			Text:      parsed.Prompt,
			Stream:    true,
			Delta:     true,
		})
		if err != nil {
			writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		streamText, err := collectStreamText(resp.Body)
		if err != nil {
			writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		respPayload := openai.BuildResponsesResponseFromText(streamText, parsed.Model)
		logIO(map[string]any{
			"dir":           "out",
			"path":          r.URL.Path,
			"stream":        true,
			"session_id":    sessionID,
			"session_key":   sessionKey,
			"model":         parsed.Model,
			"stream_output": streamText,
			"output":        respPayload,
		})
		writeJSON(w, stdhttp.StatusOK, respPayload)
		return
	}

	resp, _, err := s.gateway.Run(r.Context(), gateway.RunRequest{
		SessionID: sessionID,
		Text:      parsed.Prompt,
		Stream:    true,
		Delta:     true,
	})
	if err != nil {
		writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	streamOutput, err := streamResponses(w, resp.Body, parsed.Model)
	if err != nil {
		log.Printf("stream responses error: %v", err)
		return
	}
	logIO(map[string]any{
		"dir":           "out",
		"path":          r.URL.Path,
		"stream":        true,
		"session_id":    sessionID,
		"session_key":   sessionKey,
		"model":         parsed.Model,
		"stream_output": streamOutput,
	})
}

func (s *Server) ensureSession(ctx context.Context, r *stdhttp.Request) (string, func(bool), bool, string, error) {
	if s.gateway == nil {
		return "", func(bool) {}, false, "", errors.New("gateway client not configured")
	}
	forceNew := headerTruthy(r, "x-agent-session-reset")
	closeAfter := headerTruthy(r, "x-agent-session-close")
	if s.sessions == nil {
		sessionID, err := s.gateway.CreateSession(ctx)
		release := func(close bool) {
			_ = s.gateway.DeleteSession(ctx, sessionID)
		}
		return sessionID, release, closeAfter, "", err
	}
	key := sessionKey(r)
	lease, err := s.sessions.Acquire(ctx, key, forceNew)
	if err != nil {
		return "", func(bool) {}, closeAfter, key, err
	}
	release := func(close bool) {
		lease.Release(ctx, close)
	}
	return lease.SessionID(), release, closeAfter, key, nil
}

func withCORS(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == stdhttp.MethodOptions {
			w.WriteHeader(stdhttp.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(body io.ReadCloser) (map[string]any, error) {
	defer body.Close()
	var payload map[string]any
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

func writeJSON(w stdhttp.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func ioLogEnabled() bool {
	v := strings.TrimSpace(os.Getenv("PROXY_LOG_IO"))
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func logIO(fields map[string]any) {
	if !ioLogEnabled() {
		return
	}
	if fields == nil {
		fields = map[string]any{}
	}
	b, err := json.Marshal(fields)
	if err != nil {
		log.Printf("IOLOG {\"error\":%q}", err.Error())
		return
	}
	log.Printf("IOLOG %s", string(b))
}

func headerTruthy(r *stdhttp.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.Header.Get(name))) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func sessionKey(r *stdhttp.Request) string {
	if v := strings.TrimSpace(r.Header.Get("x-agent-session")); v != "" {
		return "hdr:" + v
	}
	if v := strings.TrimSpace(r.Header.Get("x-client-id")); v != "" {
		return "cid:" + v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "ip:" + r.RemoteAddr
	}
	return "ip:" + host
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

func gatewayRunError(runResp map[string]any) (string, bool) {
	if runResp == nil {
		return "", false
	}
	if ok, has := runResp["success"].(bool); has && !ok {
		if msg := extractGatewayErrorMessage(runResp); msg != "" {
			return msg, true
		}
		return "upstream run failed", true
	}
	if msg := extractGatewayErrorMessage(runResp); msg != "" {
		return msg, true
	}
	if data, ok := runResp["data"].(map[string]any); ok {
		if _, has := data["error"]; has {
			return "upstream run failed", true
		}
	}
	return "", false
}

func extractGatewayErrorMessage(runResp map[string]any) string {
	if runResp == nil {
		return ""
	}
	if errObj, ok := runResp["error"].(map[string]any); ok {
		if msg := extractErrorMessage(errObj); msg != "" {
			return msg
		}
	}
	if data, ok := runResp["data"].(map[string]any); ok {
		if errObj, ok := data["error"].(map[string]any); ok {
			if msg := extractErrorMessage(errObj); msg != "" {
				return msg
			}
		}
		if msg, ok := data["errorMsg"].(string); ok && strings.TrimSpace(msg) != "" {
			return msg
		}
	}
	if msg, ok := runResp["errorMsg"].(string); ok && strings.TrimSpace(msg) != "" {
		return msg
	}
	return ""
}

func extractErrorMessage(errObj map[string]any) string {
	if errObj == nil {
		return ""
	}
	if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return msg
	}
	if msg, ok := errObj["errorMsg"].(string); ok && strings.TrimSpace(msg) != "" {
		return msg
	}
	if content, ok := errObj["content"].(map[string]any); ok {
		if msg, ok := content["errorMsg"].(string); ok && strings.TrimSpace(msg) != "" {
			return msg
		}
		if msg, ok := content["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return msg
		}
	}
	if name, ok := errObj["errorName"].(string); ok && strings.TrimSpace(name) != "" {
		return name
	}
	if code, ok := errObj["errorCode"].(string); ok && strings.TrimSpace(code) != "" {
		return code
	}
	return ""
}

func openaiUpstreamError(message string) map[string]any {
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "upstream run failed"
	}
	return map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "upstream_error",
			"code":    "upstream_run_failed",
		},
	}
}

func extractGatewayTextDelta(evt map[string]any) string {
	if evt == nil {
		return ""
	}
	if data, ok := evt["data"].(map[string]any); ok {
		if msg, ok := data["message"].(map[string]any); ok {
			return extractTextFromMessage(msg)
		}
		if _, has := data["content"]; has {
			evt = data
		}
	}
	if msg, ok := evt["message"].(map[string]any); ok {
		return extractTextFromMessage(msg)
	}
	content := evt["content"]
	switch v := content.(type) {
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if text := extractTextFromContentObj(m); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		return extractTextFromContentObj(v)
	default:
		return ""
	}
}

func isGatewayEndEvent(evt map[string]any) bool {
	if evt == nil {
		return false
	}
	if end, ok := evt["end"].(bool); ok && end {
		return true
	}
	if data, ok := evt["data"].(map[string]any); ok {
		if end, ok := data["end"].(bool); ok && end {
			return true
		}
	}
	return false
}

func extractTextFromMessage(msg map[string]any) string {
	if msg == nil {
		return ""
	}
	if content, ok := msg["content"]; ok {
		switch v := content.(type) {
		case []any:
			parts := make([]string, 0, len(v))
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					if text := extractTextFromContentObj(m); text != "" {
						parts = append(parts, text)
					}
				}
			}
			return strings.Join(parts, "")
		case map[string]any:
			return extractTextFromContentObj(v)
		case string:
			return v
		}
	}
	if text, ok := msg["text"].(string); ok {
		return text
	}
	return ""
}

func extractTextFromContentObj(content map[string]any) string {
	if content == nil {
		return ""
	}
	if t, _ := content["type"].(string); t != "" && t != "text" {
		return ""
	}
	if txt, ok := content["text"].(string); ok {
		return txt
	}
	if textObj, ok := content["text"].(map[string]any); ok {
		if v, ok := textObj["value"].(string); ok {
			return v
		}
	}
	if v, ok := content["value"].(string); ok {
		return v
	}
	return ""
}

func streamChatCompletion(w stdhttp.ResponseWriter, body io.Reader, model string) (string, error) {
	flusher, ok := w.(stdhttp.Flusher)
	if !ok {
		return "", errors.New("streaming not supported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	created := time.Now().Unix()
	chatID := newID("chatcmpl")
	first := map[string]any{
		"id":      chatID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil}},
	}
	if _, err := io.WriteString(w, sseData(first)); err != nil {
		return "", err
	}
	flusher.Flush()

	var full strings.Builder
	if err := streamGatewayDeltas(body, func(delta string) error {
		full.WriteString(delta)
		chunk := map[string]any{
			"id":      chatID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": delta}, "finish_reason": nil}},
		}
		if _, err := io.WriteString(w, sseData(chunk)); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}); err != nil {
		return full.String(), err
	}

	final := map[string]any{
		"id":      chatID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
	}
	if _, err := io.WriteString(w, sseData(final)); err != nil {
		return full.String(), err
	}
	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		return full.String(), err
	}
	flusher.Flush()
	return full.String(), nil
}

func streamChatCompletionFromResponse(w stdhttp.ResponseWriter, resp map[string]any) error {
	flusher, ok := w.(stdhttp.Flusher)
	if !ok {
		return errors.New("streaming not supported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	id := stringValue(resp["id"])
	if id == "" {
		id = "chatcmpl_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	model := stringValue(resp["model"])
	if strings.TrimSpace(model) == "" {
		model = "agent"
	}
	created := time.Now().Unix()
	if v, ok := resp["created"].(float64); ok {
		created = int64(v)
	} else if v, ok := resp["created"].(int64); ok {
		created = v
	}

	msg := firstChoiceMessage(resp)
	if msg == nil {
		return errors.New("missing message")
	}
	if calls, ok := msg["tool_calls"].([]any); ok && len(calls) > 0 {
		toolCalls := make([]any, 0, len(calls))
		for i, raw := range calls {
			call, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			fn, _ := call["function"].(map[string]any)
			toolCalls = append(toolCalls, map[string]any{
				"index": i,
				"id":    call["id"],
				"type":  "function",
				"function": map[string]any{
					"name":      stringValue(fn["name"]),
					"arguments": stringValue(fn["arguments"]),
				},
			})
		}
		chunk := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": toolCalls}, "finish_reason": nil}},
		}
		if _, err := io.WriteString(w, sseData(chunk)); err != nil {
			return err
		}
		final := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}},
		}
		if _, err := io.WriteString(w, sseData(final)); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	content := stringValue(msg["content"])
	lines := openai.IterChatCompletionSSE([]string{content}, model, id)
	for _, line := range lines {
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
	}
	flusher.Flush()
	return nil
}

func streamResponses(w stdhttp.ResponseWriter, body io.Reader, model string) (string, error) {
	flusher, ok := w.(stdhttp.Flusher)
	if !ok {
		return "", errors.New("streaming not supported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	created := time.Now().Unix()
	respID := newID("resp")
	createdEvt := map[string]any{"type": "response.created", "response": map[string]any{"id": respID, "model": model, "created_at": created}}
	if _, err := io.WriteString(w, sseData(createdEvt)); err != nil {
		return "", err
	}
	flusher.Flush()

	var full strings.Builder
	if err := streamGatewayDeltas(body, func(delta string) error {
		full.WriteString(delta)
		chunk := map[string]any{"type": "response.output_text.delta", "delta": delta, "response_id": respID}
		if _, err := io.WriteString(w, sseData(chunk)); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}); err != nil {
		return full.String(), err
	}

	doneEvt := map[string]any{"type": "response.completed", "response_id": respID}
	if _, err := io.WriteString(w, sseData(doneEvt)); err != nil {
		return full.String(), err
	}
	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		return full.String(), err
	}
	flusher.Flush()
	return full.String(), nil
}

func collectStreamText(body io.Reader) (string, error) {
	var full strings.Builder
	if err := streamGatewayDeltas(body, func(delta string) error {
		full.WriteString(delta)
		return nil
	}); err != nil {
		return full.String(), err
	}
	return full.String(), nil
}

func streamGatewayDeltas(body io.Reader, emit func(string) error) error {
	scanner := bufio.NewScanner(body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	full := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		payload := line
		if strings.HasPrefix(payload, "data:") {
			payload = strings.TrimSpace(strings.TrimPrefix(payload, "data:"))
		}
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}
		var evt map[string]any
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}
		if msg, ok := gatewayRunError(evt); ok {
			if strings.TrimSpace(msg) == "" {
				msg = "upstream run failed"
			}
			return errors.New(msg)
		}
		if isGatewayEndEvent(evt) {
			break
		}
		raw := extractGatewayTextDelta(evt)
		delta := smartDelta(&full, raw)
		if delta == "" {
			continue
		}
		if err := emit(delta); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func firstChoiceMessage(resp map[string]any) map[string]any {
	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil
	}
	first, ok := choices[0].(map[string]any)
	if !ok {
		return nil
	}
	msg, _ := first["message"].(map[string]any)
	return msg
}

func stringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		if t == nil {
			return ""
		}
		return ""
	}
}

func smartDelta(full *string, chunk string) string {
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
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return prefix + "_" + string(b)
}
