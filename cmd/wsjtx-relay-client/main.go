package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/sydneyowl/wsjtx-relay/internal/client/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := cli.Execute(ctx, nil); err != nil {
		log.Fatalf("%v", err)
	}
}
