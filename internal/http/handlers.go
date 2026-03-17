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
	sessions     *session.Manager
	defaultModel string
}

func NewServer(gateway Gateway, sessions *session.Manager, opts Options) *Server {
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
	mux.HandleFunc("/model", s.handleModel)
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

func (s *Server) handleModel(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		writeJSON(w, stdhttp.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, stdhttp.StatusOK, map[string]any{"model": s.defaultModel})
}

func (s *Server) handleModels(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		writeJSON(w, stdhttp.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, stdhttp.StatusOK, map[string]any{
		"object": "list",
		"data": []any{
			map[string]any{"id": s.defaultModel, "object": "model"},
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

	sessionID, deleteAfter, closeAfter, sessionKey, err := s.ensureSession(r.Context(), r)
	if err != nil {
		writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer s.finishSession(r.Context(), sessionID, deleteAfter, closeAfter, sessionKey)

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
		writeJSON(w, stdhttp.StatusOK, openai.BuildChatCompletionResponse(text, parsed.Model))
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
	if err := streamChatCompletion(w, resp.Body, parsed.Model); err != nil {
		log.Printf("stream chat completions error: %v", err)
		return
	}
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

	sessionID, deleteAfter, closeAfter, sessionKey, err := s.ensureSession(r.Context(), r)
	if err != nil {
		writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer s.finishSession(r.Context(), sessionID, deleteAfter, closeAfter, sessionKey)

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
		writeJSON(w, stdhttp.StatusOK, openai.BuildResponsesResponse(text, parsed.Model))
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
	if err := streamResponses(w, resp.Body, parsed.Model); err != nil {
		log.Printf("stream responses error: %v", err)
		return
	}
}

func (s *Server) ensureSession(ctx context.Context, r *stdhttp.Request) (string, bool, bool, string, error) {
	if s.gateway == nil {
		return "", false, false, "", errors.New("gateway client not configured")
	}
	forceNew := headerTruthy(r, "x-agent-session-reset")
	closeAfter := headerTruthy(r, "x-agent-session-close")
	if s.sessions == nil {
		sessionID, err := s.gateway.CreateSession(ctx)
		return sessionID, true, closeAfter, "", err
	}
	key := sessionKey(r)
	var sessionID string
	if !forceNew {
		sessionID = s.sessions.Get(key)
	}
	if sessionID == "" {
		created, err := s.gateway.CreateSession(ctx)
		if err != nil {
			return "", false, closeAfter, key, err
		}
		s.sessions.Set(key, created)
		sessionID = created
	} else {
		s.sessions.Set(key, sessionID)
	}
	return sessionID, false, closeAfter, key, nil
}

func (s *Server) finishSession(ctx context.Context, sessionID string, deleteAfter bool, closeAfter bool, sessionKey string) {
	if sessionID == "" || s.gateway == nil {
		return
	}
	if deleteAfter {
		_ = s.gateway.DeleteSession(ctx, sessionID)
		return
	}
	if closeAfter && s.sessions != nil {
		s.sessions.Invalidate(sessionKey)
		_ = s.gateway.DeleteSession(ctx, sessionID)
	}
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

func extractGatewayTextDelta(evt map[string]any) string {
	if evt == nil {
		return ""
	}
	if data, ok := evt["data"].(map[string]any); ok {
		if _, has := data["content"]; has {
			evt = data
		}
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

func streamChatCompletion(w stdhttp.ResponseWriter, body io.Reader, model string) error {
	flusher, ok := w.(stdhttp.Flusher)
	if !ok {
		return errors.New("streaming not supported")
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
		return err
	}
	flusher.Flush()

	if err := streamGatewayDeltas(body, func(delta string) error {
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
		return err
	}

	final := map[string]any{
		"id":      chatID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
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

func streamResponses(w stdhttp.ResponseWriter, body io.Reader, model string) error {
	flusher, ok := w.(stdhttp.Flusher)
	if !ok {
		return errors.New("streaming not supported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	created := time.Now().Unix()
	respID := newID("resp")
	createdEvt := map[string]any{"type": "response.created", "response": map[string]any{"id": respID, "model": model, "created_at": created}}
	if _, err := io.WriteString(w, sseData(createdEvt)); err != nil {
		return err
	}
	flusher.Flush()

	if err := streamGatewayDeltas(body, func(delta string) error {
		chunk := map[string]any{"type": "response.output_text.delta", "delta": delta, "response_id": respID}
		if _, err := io.WriteString(w, sseData(chunk)); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}); err != nil {
		return err
	}

	doneEvt := map[string]any{"type": "response.completed", "response_id": respID}
	if _, err := io.WriteString(w, sseData(doneEvt)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
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
