package main

import (
	"context"
	"os/signal"
	"syscall"

	"aegis/internal/bootstrap"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	worker, err := bootstrap.NewWorkerApp(ctx)
	if err != nil {
		panic(err)
	}
	defer worker.Close(context.Background())
	worker.Logger.Info("aegis worker starting")
	if err := worker.Run(ctx); err != nil {
		panic(err)
	}
}
