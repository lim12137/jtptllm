package main

import (
	"log"
	"os"

	server "github.com/lim12137/jtptllm/internal/http"
)

func main() {
	addr := os.Getenv("PROXY_ADDR")
	if addr == "" {
		addr = ":8022"
	}

	if err := server.Run(addr); err != nil {
		log.Fatal(err)
	}
}
