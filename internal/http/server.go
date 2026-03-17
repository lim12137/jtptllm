package http

import (
	"log"
	stdhttp "net/http"
	"strings"

	"github.com/lim12137/jtptllm/internal/config"
	"github.com/lim12137/jtptllm/internal/gateway"
	"github.com/lim12137/jtptllm/internal/session"
)

type Options struct {
	Addr         string
	ApiTxt       string
	BaseURL      string
	DefaultModel string
	SessionTTL   int
}

func Run(opts Options) error {
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = ":8022"
	}
	apiTxt := strings.TrimSpace(opts.ApiTxt)
	if apiTxt == "" {
		apiTxt = "api.txt"
	}
	model := strings.TrimSpace(opts.DefaultModel)
	if model == "" {
		model = "agent"
	}
	if opts.SessionTTL < 0 {
		opts.SessionTTL = 0
	}

	cfg, err := config.LoadFromFile(apiTxt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.BaseURL) != "" {
		cfg.BaseURL = strings.TrimSpace(opts.BaseURL)
	}

	client := gateway.NewClient(cfg, nil)
	var mgr *session.Manager
	if opts.SessionTTL > 0 {
		mgr = session.NewManager(opts.SessionTTL)
	}

	h := NewHandler(HandlerDeps{Client: client, Sessions: mgr, DefaultModel: model})
	srv := &stdhttp.Server{Addr: addr, Handler: h}
	log.Printf("proxy listening on %s", addr)
	return srv.ListenAndServe()
}