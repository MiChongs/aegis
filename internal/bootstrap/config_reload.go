package bootstrap

import (
	"reflect"

	"aegis/internal/config"
	"aegis/internal/middleware"
	"aegis/internal/service"
	"go.uber.org/zap"
)

func registerAPIConfigHotReload(manager *config.Manager, log *zap.Logger, firewall *middleware.Firewall, security *service.SecurityService, autoSign *service.AutoSignService, accountBan *service.AccountBanService, risk *service.RiskService) {
	if manager == nil {
		return
	}
	manager.OnChange(func(event config.ChangeEvent) {
		handleConfigReloadEvent(log, "api", event)
		if event.Err != nil {
			return
		}
		if firewall != nil {
			if err := firewall.Reload(event.Current.Firewall); err != nil {
				log.Warn("api config hot reload apply failed", zap.String("component", "firewall"), zap.Error(err))
			}
		}
		if security != nil {
			if err := security.Reload(event.Current.Security); err != nil {
				log.Warn("api config hot reload apply failed", zap.String("component", "security"), zap.Error(err))
			}
		}
		if autoSign != nil {
			autoSign.Reload(event.Current.AutoSign)
		}
		if accountBan != nil {
			if err := accountBan.Reload(event.Current.AccountBan); err != nil {
				log.Warn("api config hot reload apply failed", zap.String("component", "account_ban"), zap.Error(err))
			}
		}
		if risk != nil {
			risk.Reload(event.Current.Risk)
		}
		logConfigReloadSummary(log, "api", event.Previous, event.Current, []string{"default_timezone", "firewall", "security", "auto_sign", "account_ban", "risk"})
	})
	if manager.Start() {
		log.Info("config hot reload watcher started", zap.String("runtime", "api"), zap.String("configFile", manager.ConfigFile()))
	}
}

func registerWorkerConfigHotReload(manager *config.Manager, log *zap.Logger, autoSign *service.AutoSignService) {
	if manager == nil {
		return
	}
	manager.OnChange(func(event config.ChangeEvent) {
		handleConfigReloadEvent(log, "worker", event)
		if event.Err != nil {
			return
		}
		if autoSign != nil {
			autoSign.Reload(event.Current.AutoSign)
		}
		logConfigReloadSummary(log, "worker", event.Previous, event.Current, []string{"default_timezone", "auto_sign"})
	})
	if manager.Start() {
		log.Info("config hot reload watcher started", zap.String("runtime", "worker"), zap.String("configFile", manager.ConfigFile()))
	}
}

func handleConfigReloadEvent(log *zap.Logger, runtime string, event config.ChangeEvent) {
	if event.Err != nil {
		log.Warn("config hot reload rejected", zap.String("runtime", runtime), zap.String("path", event.Path), zap.Error(event.Err))
		return
	}
	log.Info("config file changed", zap.String("runtime", runtime), zap.String("path", event.Path), zap.Time("changedAt", event.ChangedAt))
}

func logConfigReloadSummary(log *zap.Logger, runtime string, previous, current config.Config, reloaded []string) {
	restartRequired := immutableConfigSections(previous, current)
	fields := []zap.Field{
		zap.String("runtime", runtime),
		zap.Strings("reloaded", reloaded),
	}
	if len(restartRequired) > 0 {
		fields = append(fields, zap.Strings("restartRequired", restartRequired))
	}
	log.Info("config hot reload applied", fields...)
}

func immutableConfigSections(previous, current config.Config) []string {
	changed := make([]string, 0, 12)
	if previous.AppName != current.AppName || previous.AppEnv != current.AppEnv {
		changed = append(changed, "app")
	}
	if previous.HTTPPort != current.HTTPPort || previous.ReadTimeout != current.ReadTimeout || previous.WriteTimeout != current.WriteTimeout || previous.ShutdownTimeout != current.ShutdownTimeout || !reflect.DeepEqual(previous.CORS, current.CORS) {
		changed = append(changed, "http")
	}
	if !reflect.DeepEqual(previous.JWT, current.JWT) || !reflect.DeepEqual(previous.OAuth, current.OAuth) {
		changed = append(changed, "auth_tokens")
	}
	if !reflect.DeepEqual(previous.Postgres, current.Postgres) {
		changed = append(changed, "postgres")
	}
	if !reflect.DeepEqual(previous.LegacyMySQL, current.LegacyMySQL) {
		changed = append(changed, "legacy_mysql")
	}
	if !reflect.DeepEqual(previous.Redis, current.Redis) {
		changed = append(changed, "redis")
	}
	if !reflect.DeepEqual(previous.NATS, current.NATS) || !reflect.DeepEqual(previous.Temporal, current.Temporal) {
		changed = append(changed, "runtime_backends")
	}
	if !reflect.DeepEqual(previous.ReplayProtection, current.ReplayProtection) {
		changed = append(changed, "replay_protection")
	}
	if !reflect.DeepEqual(previous.GeoIP, current.GeoIP) {
		changed = append(changed, "geoip")
	}
	if !reflect.DeepEqual(previous.PaymentBillExport, current.PaymentBillExport) {
		changed = append(changed, "payment_bill_export")
	}
	if !reflect.DeepEqual(previous.AdminUserSearch, current.AdminUserSearch) {
		changed = append(changed, "admin_user_search")
	}
	if !reflect.DeepEqual(previous.Memory, current.Memory) {
		changed = append(changed, "memory")
	}
	if !reflect.DeepEqual(previous.Lottery, current.Lottery) {
		changed = append(changed, "lottery")
	}
	if !reflect.DeepEqual(previous.CrashLog, current.CrashLog) {
		changed = append(changed, "crashlog")
	}
	return changed
}
