package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"aegis/internal/config"
	"aegis/internal/db"
	legacyrepo "aegis/internal/repository/legacymysql"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/internal/service"
	httptransport "aegis/internal/transport/http"
	pkglogger "aegis/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// autoMigrate 启动时自动执行所有 SQL 迁移（幂等，失败不阻塞启动）
func autoMigrate(ctx context.Context, pool *pgxpool.Pool, log *zap.Logger) error {
	files, err := migrationFiles()
	if err != nil {
		return err
	}
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
			log.Warn("迁移跳过（可能已应用）", zap.String("file", filepath.Base(file)), zap.Error(err))
		}
	}
	log.Info("数据库迁移检查完成", zap.Int("files", len(files)))
	return nil
}

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

	files, err := migrationFiles()
	if err != nil {
		return err
	}
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

func migrationFiles() ([]string, error) {
	dir, err := resolveProjectPath(filepath.Join("migrations", "postgres"))
	if err != nil {
		return nil, err
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no migration files found in %s", dir)
	}
	return files, nil
}

func resolveProjectPath(relativePath string) (string, error) {
	searchRoots := make([]string, 0, 2)
	if wd, err := os.Getwd(); err == nil {
		searchRoots = append(searchRoots, wd)
	}
	if exePath, err := os.Executable(); err == nil {
		searchRoots = append(searchRoots, filepath.Dir(exePath))
	}

	seen := make(map[string]struct{}, len(searchRoots)*4)
	for _, root := range searchRoots {
		for _, dir := range parentDirs(root) {
			candidate := filepath.Join(dir, relativePath)
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf("cannot resolve project path %q from current working directory or executable path", relativePath)
}

func parentDirs(start string) []string {
	if strings.TrimSpace(start) == "" {
		return nil
	}
	absStart, err := filepath.Abs(start)
	if err != nil {
		absStart = start
	}

	dirs := make([]string, 0, 8)
	current := filepath.Clean(absStart)
	for {
		dirs = append(dirs, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return dirs
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
	if err := migrator.FinalizeLegacySync(ctx); err != nil {
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

	totalRequested := 0
	totalSynced := 0
	totalSkipped := 0
	totalFailed := 0
	batchCount := 0
	currentLastID := lastID

	for {
		result, err := migrator.SyncLegacyUsersBatch(ctx, currentLastID, limit)
		if err != nil {
			return err
		}

		batchCount++
		totalRequested += result.Requested
		totalSynced += result.Synced
		totalSkipped += result.Skipped
		totalFailed += result.Failed

		fmt.Printf("sync batch %d completed: requested=%d synced=%d skipped=%d failed=%d lastUserId=%d\n", batchCount, result.Requested, result.Synced, result.Skipped, result.Failed, result.LastUserID)

		if result.Requested == 0 {
			break
		}
		if result.LastUserID <= currentLastID {
			return fmt.Errorf("sync batch stalled: currentLastID=%d nextLastUserID=%d", currentLastID, result.LastUserID)
		}
		currentLastID = result.LastUserID
	}

	if totalRequested > 0 {
		if err := migrator.FinalizeLegacySync(ctx); err != nil {
			return err
		}
	}

	fmt.Printf("sync all batches completed: batches=%d requested=%d synced=%d skipped=%d failed=%d lastUserId=%d\n", batchCount, totalRequested, totalSynced, totalSkipped, totalFailed, currentLastID)
	return nil
}

func RunExportOpenAPI(_ context.Context, args []string) error {
	outputPath := ""
	if len(args) > 0 {
		outputPath = strings.TrimSpace(args[0])
	}

	gin.SetMode(gin.ReleaseMode)
	router, err := httptransport.NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, config.CORSConfig{})
	if err != nil {
		return err
	}

	spec, err := httptransport.BuildOpenAPISpec(router, httptransport.DefaultDocsOptions())
	if err != nil {
		return err
	}
	content, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')

	if outputPath == "" {
		_, err = os.Stdout.Write(content)
		return err
	}
	if err := os.WriteFile(outputPath, content, 0o644); err != nil {
		return err
	}
	fmt.Printf("exported openapi spec: %s\n", outputPath)
	return nil
}

func RunExportPostman(_ context.Context, args []string) error {
	outputPath := "docs/postman/aegis-api-cn.postman_collection.json"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		outputPath = strings.TrimSpace(args[0])
	}

	gin.SetMode(gin.ReleaseMode)
	router, err := httptransport.NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, config.CORSConfig{})
	if err != nil {
		return err
	}

	spec, err := httptransport.BuildOpenAPISpec(router, httptransport.DefaultDocsOptions())
	if err != nil {
		return err
	}
	collection, err := httptransport.BuildPostmanCollection(spec, httptransport.DefaultPostmanOptions())
	if err != nil {
		return err
	}
	content, err := json.MarshalIndent(collection, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')

	if outputPath == "" {
		_, err = os.Stdout.Write(content)
		return err
	}
	if dir := filepath.Dir(outputPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(outputPath, content, 0o644); err != nil {
		return err
	}
	fmt.Printf("exported postman collection: %s\n", outputPath)
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
	legacyDB, err := db.NewLegacyMySQL(ctx, cfg.LegacyMySQL)
	if err != nil {
		postgres.Close()
		return nil, nil, err
	}

	pg := pgrepo.New(postgres)
	legacy := legacyrepo.New(legacyDB)
	migrator := service.NewMigrationService(cfg, log, legacy, pg)
	cleanup := func() {
		_ = legacyDB.Close()
		postgres.Close()
		_ = log.Sync()
	}
	return migrator, cleanup, nil
}
