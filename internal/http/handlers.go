package http

import (
	stdhttp "net/http"

	"github.com/lim12137/jtptllm/internal/gateway"
	"github.com/lim12137/jtptllm/internal/session"
)

type HandlerDeps struct {
	Client       *gateway.Client
	Sessions     *session.Manager
	DefaultModel string
}

func NewHandler(_ HandlerDeps) stdhttp.Handler {
	return stdhttp.NewServeMux()
}