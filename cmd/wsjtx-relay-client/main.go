package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/sydneyowl/wsjtx-relay/internal/client/config"
	"github.com/sydneyowl/wsjtx-relay/internal/client/relay"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	client := relay.New(cfg)
	if err := client.Run(ctx); err != nil {
		log.Fatalf("relay client stopped with error: %v", err)
	}
}
