package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"fmt"

	"aegis/internal/bootstrap"
	"aegis/internal/config"
	"aegis/pkg/crashlog"
	"go.uber.org/zap"
)

func main() {
	// 最早初始化崩溃日志管理器（独立于 zap，确保 panic 时可写入）
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
		case "mock-users":
			if err := bootstrap.RunGenerateMockUsers(ctx, os.Args[2:]); err != nil {
				panic(err)
			}
			return
		}
	}

	printBanner()

	app, err := bootstrap.NewUnifiedApp(ctx, cl)
	if err != nil {
		panic(err)
	}
	defer app.Close(context.Background())

	app.API.Logger.Info("aegis unified runtime starting",
		zap.Int("port", app.API.Config.HTTPPort),
		zap.String("mode", "api+worker"),
	)

	// 监听停止信号文件（Windows 无法优雅发送 SIGINT 给后台进程）
	go watchStopFile(stop, ".runtime/run/server.stop")

	go func() {
		<-ctx.Done()
		app.Close(context.Background())
	}()

	if err := app.Run(ctx); err != nil {
		panic(err)
	}
}

// watchStopFile 定时检测停止信号文件，发现后触发 context 取消实现优雅关闭。
// PowerShell stop-stack.ps1 创建该文件，Go 进程检测到后自行关闭（exit 0）。
func watchStopFile(cancel context.CancelFunc, path string) {
	// 支持绝对路径和相对路径
	if !filepath.IsAbs(path) {
		if wd, err := os.Getwd(); err == nil {
			path = filepath.Join(wd, path)
		}
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		if _, err := os.Stat(path); err == nil {
			// 信号文件存在，触发优雅关闭
			_ = os.Remove(path)
			cancel()
			return
		}
	}
}

func printBanner() {
	const banner = `
    ___    _____ ______________
   /   |  / ____/ ____/  _/ ___/
  / /| | / __/ / / __ / /  \__ \
 / ___ |/ /___/ /_/ // /_ ___/ /
/_/  |_/_____/\____/___//____/
`
	fmt.Print(banner)
}
