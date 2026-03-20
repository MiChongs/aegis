package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"aegis/internal/config"
	"aegis/internal/db"
	"aegis/internal/event"
	legacyrepo "aegis/internal/repository/legacymysql"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	"aegis/internal/service"
	pkglogger "aegis/pkg/logger"
)

func RunMigrations(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := db.NewPostgres(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()

	files, err := filepath.Glob("migrations/postgres/*.up.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		sql := strings.TrimSpace(string(content))
		if sql == "" {
			continue
		}
		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("apply migration %s failed: %w", file, err)
		}
		fmt.Println("applied migration:", file)
	}
	return nil
}

func RunSyncLegacyUser(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: go run ./cmd/server sync-legacy-user <user_id>")
	}
	userID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return err
	}
	migrator, cleanup, err := newMigrationService(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := migrator.SyncLegacyUserByID(ctx, userID); err != nil {
		return err
	}
	fmt.Printf("synced legacy user %d\n", userID)
	return nil
}

func RunSyncLegacyBatch(ctx context.Context, args []string) error {
	var lastID int64
	var limit int
	var err error
	if len(args) > 0 {
		lastID, err = strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return err
		}
	}
	if len(args) > 1 {
		limit, err = strconv.Atoi(args[1])
		if err != nil {
			return err
		}
	}
	migrator, cleanup, err := newMigrationService(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	result, err := migrator.SyncLegacyUsersBatch(ctx, lastID, limit)
	if err != nil {
		return err
	}
	fmt.Printf("sync batch completed: requested=%d synced=%d failed=%d lastUserId=%d\n", result.Requested, result.Synced, result.Failed, result.LastUserID)
	return nil
}

func newMigrationService(ctx context.Context) (*service.MigrationService, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	if cfg.LegacyMySQL.DSN == "" {
		return nil, nil, fmt.Errorf("LEGACY_MYSQL_DSN is required for legacy migration commands")
	}

	log, err := pkglogger.New(cfg.AppEnv)
	if err != nil {
		return nil, nil, err
	}
	postgres, err := db.NewPostgres(ctx, cfg.Postgres)
	if err != nil {
		return nil, nil, err
	}
	redisClient := db.NewRedis(ctx, cfg.Redis)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		postgres.Close()
		return nil, nil, err
	}
	legacyDB, err := db.NewLegacyMySQL(ctx, cfg.LegacyMySQL)
	if err != nil {
		postgres.Close()
		_ = redisClient.Close()
		return nil, nil, err
	}
	natsConn, js, err := db.NewNATS(ctx, cfg.NATS)
	if err != nil {
		postgres.Close()
		_ = redisClient.Close()
		_ = legacyDB.Close()
		return nil, nil, err
	}

	pg := pgrepo.New(postgres)
	legacy := legacyrepo.New(legacyDB)
	sessions := redisrepo.NewSessionRepository(redisClient, cfg.Redis.KeyPrefix)
	publisher := event.NewPublisher(js)
	migrator := service.NewMigrationService(cfg, log, legacy, pg, sessions, publisher)
	cleanup := func() {
		natsConn.Drain()
		natsConn.Close()
		_ = legacyDB.Close()
		_ = redisClient.Close()
		postgres.Close()
		_ = log.Sync()
	}
	return migrator, cleanup, nil
}
