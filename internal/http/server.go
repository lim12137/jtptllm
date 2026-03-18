package http

import (
	"log"
	stdhttp "net/http"
	"os"

	"github.com/lim12137/jtptllm/internal/config"
	"github.com/lim12137/jtptllm/internal/gateway"
	"github.com/lim12137/jtptllm/internal/session"
)

const (
	defaultAPITxt          = "api.txt"
	defaultModelName       = "agent"
	defaultSessionTTL      = 600
	defaultSessionPoolSize = 10
)

// Run starts the HTTP server with default config.
func Run(addr string) error {
	cfg, err := loadConfig(defaultAPITxt)
	if err != nil {
		return err
	}
	gw := gateway.NewClient(cfg, nil)
	sessions := session.NewPoolManager(gw, defaultSessionTTL, defaultSessionPoolSize)
	server := NewServer(gw, sessions, Options{DefaultModel: defaultModelName})
	log.Printf("proxy server starting on %s", addr)
	return stdhttp.ListenAndServe(addr, server.Handler())
}

func loadConfig(path string) (config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config.Config{}, err
	}
	return config.ParseApiTxt(data)
}
