package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lim12137/jtptllm/internal/config"
)

const defaultTimeout = 120 * time.Second

type Client struct {
	baseURL      string
	appKey       string
	agentCode    string
	agentVersion string
	httpClient   *http.Client
}

func NewClient(cfg config.Config, httpClient *http.Client) *Client {
	cli := httpClient
	if cli == nil {
		cli = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		appKey:       cfg.AppKey,
		agentCode:    cfg.AgentCode,
		agentVersion: cfg.AgentVersion,
		httpClient:   cli,
	}
}

func (c *Client) CreateSession(ctx context.Context) (string, error) {
	payload := map[string]any{"agentCode": c.agentCode}
	if c.agentVersion != "" {
		payload["agentVersion"] = c.agentVersion
	}
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			UniqueCode string `json:"uniqueCode"`
		} `json:"data"`
	}
	if err := c.postJSON(ctx, "/createSession", payload, &out); err != nil {
		return "", err
	}
	if !out.Success {
		return "", errors.New("createSession failed")
	}
	if strings.TrimSpace(out.Data.UniqueCode) == "" {
		return "", errors.New("createSession missing uniqueCode")
	}
	return out.Data.UniqueCode, nil
}

func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	payload := map[string]any{"sessionId": sessionID}
	var out struct {
		Success bool `json:"success"`
	}
	if err := c.postJSON(ctx, "/deleteSession", payload, &out); err != nil {
		return err
	}
	if !out.Success {
		return errors.New("deleteSession failed")
	}
	return nil
}

type RunRequest struct {
	SessionID   string
	Text        string
	Stream      bool
	Delta       bool
	Trace       bool
	Metadata    map[string]any
	Attachments []map[string]any
}

func (c *Client) Run(ctx context.Context, req RunRequest) (*http.Response, map[string]any, error) {
	payload := map[string]any{
		"sessionId": req.SessionID,
		"stream":    req.Stream,
		"delta":     req.Delta,
		"message": map[string]any{
			"text":        req.Text,
			"metadata":    firstMap(req.Metadata),
			"attachments": firstSlice(req.Attachments),
		},
	}
	if req.Trace {
		payload["trace"] = true
	}

	if req.Stream {
		return c.postStream(ctx, "/run", payload)
	}
	var out map[string]any
	if err := c.postJSON(ctx, "/run", payload, &out); err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload any, out any) error {
	resp, body, err := c.doPost(ctx, path, payload, false)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
	return nil
}

func (c *Client) postStream(ctx context.Context, path string, payload any) (*http.Response, map[string]any, error) {
	resp, _, err := c.doPost(ctx, path, payload, true)
	if err != nil {
		return nil, nil, err
	}
	return resp, nil, nil
}

func (c *Client) doPost(ctx context.Context, path string, payload any, stream bool) (*http.Response, []byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	url := c.baseURL + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", normalizeBearer(c.appKey))
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}
	if stream {
		return resp, nil, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		return nil, nil, err
	}
	return resp, body, nil
}

func normalizeBearer(appKey string) string {
	v := strings.TrimSpace(appKey)
	if v == "" {
		return v
	}
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return v
	}
	return "Bearer " + v
}

func firstMap(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	return v
}

func firstSlice(v []map[string]any) []map[string]any {
	if v == nil {
		return []map[string]any{}
	}
	return v
}
