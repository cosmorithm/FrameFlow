package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/cosmorithm/FrameFlow/internal/feed"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if err := feed.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
