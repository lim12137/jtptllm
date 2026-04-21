package http

import (
	"log"
	stdhttp "net/http"
	"os"
	"strconv"

	"github.com/lim12137/jtptllm/internal/config"
	"github.com/lim12137/jtptllm/internal/gateway"
	"github.com/lim12137/jtptllm/internal/openai"
	"github.com/lim12137/jtptllm/internal/session"
)

const (
	defaultAPITxt          = "api.txt"
	defaultSessionTTL      = 600
	defaultSessionPoolSize = 10
	defaultGlobalHTTPLimit = 16
)

// Run starts the HTTP server with default config.
func Run(addr string) error {
	cfg, err := loadConfig(defaultAPITxt)
	if err != nil {
		return err
	}
	httpLimit := defaultGlobalHTTPLimit
	if v := os.Getenv("PROXY_HTTP_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			httpLimit = n
		}
	}
	gw := gateway.NewClient(cfg, nil)
	sessions := session.NewPoolManager(gw, defaultSessionTTL, defaultSessionPoolSize)
	server := NewServer(gw, sessions, Options{DefaultModel: openai.DefaultModel, HTTPLimit: httpLimit})
	log.Printf("proxy server starting on %s (http_limit=%d)", addr, httpLimit)
	return stdhttp.ListenAndServe(addr, server.Handler())
}

func loadConfig(path string) (config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config.Config{}, err
	}
	return config.ParseApiTxt(data)
}
