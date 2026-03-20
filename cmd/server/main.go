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
		case "openapi":
			if err := bootstrap.RunExportOpenAPI(ctx, os.Args[2:]); err != nil {
				panic(err)
			}
			return
		case "postman":
			if err := bootstrap.RunExportPostman(ctx, os.Args[2:]); err != nil {
				panic(err)
			}
			return
		}
	}

	app, err := bootstrap.NewUnifiedApp(ctx)
	if err != nil {
		panic(err)
	}
	defer app.Close(context.Background())

	app.API.Logger.Info("aegis unified runtime starting",
		zap.Int("port", app.API.Config.HTTPPort),
		zap.String("mode", "api+worker"),
	)

	go func() {
		<-ctx.Done()
		app.Close(context.Background())
	}()

	if err := app.Run(ctx); err != nil {
		panic(err)
	}
}
