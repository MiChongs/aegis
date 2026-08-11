package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aegis/internal/bootstrap"
	"aegis/internal/config"
	"aegis/pkg/crashlog"
)

func main() {
	started := time.Now()

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		// 配置读不起来时横幅配置也是零值，用默认值兜底，见 config.DefaultBannerConfig
		cfg.Banner = config.DefaultBannerConfig()
	}
	cl := crashlog.New(cfg.CrashLog.Dir, cfg.CrashLog.MaxFiles, cfg.CrashLog.MaxSize)
	defer func() {
		if r := recover(); r != nil {
			cl.Write("main.fatal", r, false)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bootstrap.PrintBootBanner(cfg, bootstrap.RoleWorker)

	worker, err := bootstrap.NewWorkerApp(ctx, cl)
	if err != nil {
		panic(err)
	}
	defer worker.Close(context.Background())
	worker.Logger.Info("aegis worker starting")
	bootstrap.PrintReadyBanner(ctx, worker.BannerRuntimeOf(bootstrap.RoleWorker, time.Since(started)))

	if err := worker.Run(ctx); err != nil {
		panic(err)
	}
}
