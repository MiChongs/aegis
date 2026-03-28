package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"aegis/internal/bootstrap"
	"aegis/internal/config"
	"aegis/pkg/crashlog"
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
	worker, err := bootstrap.NewWorkerApp(ctx, cl)
	if err != nil {
		panic(err)
	}
	defer worker.Close(context.Background())
	worker.Logger.Info("aegis worker starting")
	if err := worker.Run(ctx); err != nil {
		panic(err)
	}
}
