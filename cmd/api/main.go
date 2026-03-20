package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"aegis/internal/bootstrap"
	"go.uber.org/zap"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			if err := bootstrap.RunMigrations(ctx); err != nil {
				panic(err)
			}
			return
		case "sync-legacy-user":
			if err := bootstrap.RunSyncLegacyUser(ctx, os.Args[2:]); err != nil {
				panic(err)
			}
			return
		case "sync-legacy-batch":
			if err := bootstrap.RunSyncLegacyBatch(ctx, os.Args[2:]); err != nil {
				panic(err)
			}
			return
		}
	}

	app, err := bootstrap.NewAPIApp(ctx)
	if err != nil {
		panic(err)
	}
	defer app.Close(context.Background())

	app.Logger.Info("aegis api starting", zap.Int("port", app.Config.HTTPPort))
	go func() {
		<-ctx.Done()
		app.Close(context.Background())
	}()
	if err := app.Server.ListenAndServe(); err != nil && err.Error() != "http: Server closed" {
		panic(err)
	}
}
