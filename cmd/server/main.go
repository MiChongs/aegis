package main

import (
	"aegis/internal/bootstrap"
	"aegis/internal/config"
	"aegis/pkg/crashlog"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
)

func main() {
	started := time.Now()

	// 最早初始化崩溃日志管理器（独立于 zap，确保 panic 时可写入）
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		// 配置没读起来时横幅配置也是零值（Enabled=false）。
		// 这种时候恰恰更需要横幅先证明进程活着，随后的装配错误再解释为什么起不来。
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
		case "import-dump":
			if err := bootstrap.RunImportDump(ctx, os.Args[2:]); err != nil {
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
		case "routes":
			if err := bootstrap.RunPrintRoutes(ctx, os.Args[2:]); err != nil {
				panic(err)
			}
			return
		case "promote-system-admin":
			if err := bootstrap.RunPromoteSystemAdmin(ctx, os.Args[2:]); err != nil {
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

	// AEGIS_ROLE 选择角色：all（默认，API+Worker 同进程）、api、worker。
	// 集群里同一个镜像按角色起成不同的服务，横向扩容时各自加副本。
	// 进程环境变量优先（编排系统按服务注入），其次 .env
	rawRole := os.Getenv("AEGIS_ROLE")
	if strings.TrimSpace(rawRole) == "" {
		rawRole = cfg.Cluster.Role
	}
	role, roleErr := resolveRole(rawRole)
	if roleErr != nil {
		panic(roleErr)
	}
	bootstrap.PrintBootBanner(cfg, role)

	var system *bootstrap.RuntimeSystem
	var err error
	switch role {
	case bootstrap.RoleAPI:
		system, err = bootstrap.NewAPISystem(ctx, cl)
	case bootstrap.RoleWorker:
		system, err = bootstrap.NewWorkerSystem(ctx, cl)
	default:
		system, err = bootstrap.NewUnifiedSystem(ctx, cl)
	}
	if err != nil {
		panic(err)
	}
	logger := zap.NewNop()
	switch {
	case system.API != nil:
		logger = system.API.Logger
	case system.Worker != nil:
		logger = system.Worker.Logger
	}

	// 监听停止信号文件（Windows 无法优雅发送 SIGINT 给后台进程）
	go watchStopFile(stop, ".runtime/run/server.stop")

	fields := []zap.Field{zap.String("role", string(role))}
	if system.API != nil {
		fields = append(fields, zap.Int("port", system.API.Config.HTTPPort))
	}
	logger.Info("aegis runtime starting", fields...)
	if err := system.StartWithTimeout(ctx); err != nil {
		_ = system.StopWithTimeout()
		panic(err)
	}
	// 就绪横幅在所有组件启动完成、端口已监听之后才打印
	if system.API != nil {
		bootstrap.PrintReadyBanner(ctx, system.API.BannerRuntimeOf(role, time.Since(started)))
	} else if system.Worker != nil {
		bootstrap.PrintReadyBanner(ctx, system.Worker.BannerRuntimeOf(role, time.Since(started)))
	}

	runErr := system.Wait(ctx)
	// 收到信号或组件出错都走同一条关停路径，且只走一次
	if err := system.StopWithTimeout(); err != nil {
		logger.Warn("graceful shutdown incomplete", zap.Error(err))
	}
	if runErr != nil {
		panic(runErr)
	}
}

func resolveRole(raw string) (bootstrap.Role, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "all", "unified", "server":
		return bootstrap.RoleUnified, nil
	case "api":
		return bootstrap.RoleAPI, nil
	case "worker":
		return bootstrap.RoleWorker, nil
	default:
		return "", fmt.Errorf("AEGIS_ROLE=%q 无效，可选 all / api / worker", raw)
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
