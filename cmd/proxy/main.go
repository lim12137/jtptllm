package main

import (
	"log"

	server "github.com/lim12137/jtptllm/internal/http"
)

func main() {
	if err := server.Run(":8022"); err != nil {
		log.Fatal(err)
	}
}
