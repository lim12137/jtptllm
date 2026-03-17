package main

import (
	"flag"
	"log"
	"net"
	"strconv"

	server "github.com/lim12137/jtptllm/internal/http"
)

func main() {
	var apiTxt string
	var baseURL string
	var defaultModel string
	var host string
	var port int
	var sessionTTL int

	flag.StringVar(&apiTxt, "api-txt", "api.txt", "Path to api.txt (key/agentCode/agentVersion)")
	flag.StringVar(&baseURL, "base-url", "", "Override gateway base url")
	flag.StringVar(&defaultModel, "default-model", "agent", "Default model name")
	flag.StringVar(&host, "host", "0.0.0.0", "Listen host")
	flag.IntVar(&port, "port", 8022, "Listen port")
	flag.IntVar(&sessionTTL, "session-ttl", 600, "Session idle TTL seconds (0=disable reuse)")
	flag.Parse()

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	if err := server.Run(server.Options{
		Addr:         addr,
		ApiTxt:       apiTxt,
		BaseURL:      baseURL,
		DefaultModel: defaultModel,
		SessionTTL:   sessionTTL,
	}); err != nil {
		log.Fatal(err)
	}
}