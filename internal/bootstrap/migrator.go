package bootstrap

import (
	"aegis/internal/cluster"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// 迁移的执行规则（多副本安全）：
//
//   - 整个过程持有 advisory lock「aegis.migrations」。N 个副本同时启动时依次进入，
//     后进入的看到版本都已记录，直接跳过，不会并发执行 DDL。
//   - 版本表 schema_migrations 记录每个 *.up.sql 的文件名与内容摘要。摘要没变的文件不再执行；
//     摘要变了（有人原地改了旧迁移）就再执行一次 —— 这与改造前「每次启动全部重跑」对被改文件的效果一致。
//   - 首次接入版本表时（表是空的）库里其实已经执行过全部旧迁移。此时逐个重跑一遍，
//     报错的按「已应用」记账（note=assumed），之后不再重复执行。
//   - strict（`aegis-server migrate` 任务）下，**从未记录过**的新迁移失败会中止并返回错误，
//     让发布流水线停在升级 API 之前；启动时的自动迁移（非 strict）只记日志，不阻塞启动。
const migrationLockKey = "aegis.migrations"

const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     TEXT PRIMARY KEY,
    checksum    TEXT NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms INTEGER NOT NULL DEFAULT 0,
    applied_by  TEXT NOT NULL DEFAULT '',
    note        TEXT NOT NULL DEFAULT ''
)`

type migrationFile struct {
	Version  string
	Path     string
	SQL      string
	Checksum string
}

type migrationReport struct {
	Total   int
	Applied []string
	Skipped int
	Assumed []string // 首次接入时执行报错、按已应用记账的
	Failed  []string // 非 strict 下失败、未记账的
}

func loadMigrationFiles() ([]migrationFile, error) {
	paths, err := migrationFiles()
	if err != nil {
		return nil, err
	}
	out := make([]migrationFile, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sql := strings.TrimSpace(string(content))
		if sql == "" {
			continue
		}
		sum := sha256.Sum256([]byte(sql))
		out = append(out, migrationFile{
			Version:  strings.TrimSuffix(filepath.Base(path), ".up.sql"),
			Path:     path,
			SQL:      sql,
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	return out, nil
}

// migrationPlan 纯函数：给出已记录的版本 → 摘要，算出哪些文件需要执行。
func migrationPlan(files []migrationFile, recorded map[string]string) (pending []migrationFile, skipped int) {
	for _, f := range files {
		if sum, ok := recorded[f.Version]; ok && sum == f.Checksum {
			skipped++
			continue
		}
		pending = append(pending, f)
	}
	return pending, skipped
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool, log *zap.Logger, strict bool, onProgress func(file string)) (migrationReport, error) {
	if log == nil {
		log = zap.NewNop()
	}
	files, err := loadMigrationFiles()
	if err != nil {
		return migrationReport{}, err
	}
	report := migrationReport{Total: len(files)}
	holder, _ := os.Hostname()

	err = cluster.WithAdvisoryLock(ctx, pool, migrationLockKey, func(ctx context.Context, conn *pgxpool.Conn) error {
		if _, err := conn.Exec(ctx, schemaMigrationsDDL); err != nil {
			return fmt.Errorf("创建 schema_migrations 失败: %w", err)
		}
		recorded := map[string]string{}
		rows, err := conn.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var version, sum string
			if err := rows.Scan(&version, &sum); err != nil {
				rows.Close()
				return err
			}
			recorded[version] = sum
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		firstAdoption := len(recorded) == 0
		pending, skipped := migrationPlan(files, recorded)
		report.Skipped = skipped
		if firstAdoption && len(pending) > 0 {
			log.Info("首次接入迁移版本表，逐个核对已有迁移", zap.Int("files", len(pending)))
		}

		for _, f := range pending {
			_, known := recorded[f.Version]
			started := time.Now()
			_, execErr := conn.Exec(ctx, f.SQL)
			elapsed := time.Since(started)
			note := ""
			if execErr != nil {
				switch {
				case firstAdoption:
					note = "assumed"
					report.Assumed = append(report.Assumed, f.Version)
					log.Debug("迁移按已应用记账", zap.String("file", f.Version), zap.Error(execErr))
				case strict && !known:
					return fmt.Errorf("执行迁移 %s 失败: %w", f.Version, execErr)
				default:
					// 被改过的旧迁移重跑失败、或启动时的自动迁移失败：保持改造前的宽松行为，只记日志，
					// 下次启动还会再试。
					report.Failed = append(report.Failed, f.Version)
					log.Warn("迁移执行失败（可能已应用）", zap.String("file", f.Version), zap.Error(execErr))
					continue
				}
			}
			if _, err := conn.Exec(ctx, `
INSERT INTO schema_migrations (version, checksum, applied_at, duration_ms, applied_by, note)
VALUES ($1, $2, now(), $3, $4, $5)
ON CONFLICT (version) DO UPDATE
   SET checksum = EXCLUDED.checksum, applied_at = EXCLUDED.applied_at,
       duration_ms = EXCLUDED.duration_ms, applied_by = EXCLUDED.applied_by, note = EXCLUDED.note`,
				f.Version, f.Checksum, elapsed.Milliseconds(), holder, note); err != nil {
				return fmt.Errorf("记录迁移 %s 失败: %w", f.Version, err)
			}
			if execErr == nil {
				report.Applied = append(report.Applied, f.Version)
			}
			if onProgress != nil {
				onProgress(f.Version)
			}
		}
		return nil
	})
	return report, err
}
