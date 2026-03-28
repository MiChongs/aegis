package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"aegis/internal/bootstrap"
	"aegis/internal/config"
	"aegis/pkg/crashlog"
	"go.uber.org/zap"
)

func main() {
	cfg, _ := config.Load()
	cl := crashlog.New(cfg.CrashLog.Dir, cfg.CrashLog.MaxFiles, cfg.CrashLog.MaxSize)
	defer func() {
		if r := recover(); r != nil {
			cl.Write("main.fatal", r, false)
			os.Exit(1)
		}
	}()

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

	app, err := bootstrap.NewAPIApp(ctx, cl)
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
