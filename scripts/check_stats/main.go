package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

func main() {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://aegis:aegis@127.0.0.1:5432/aegis?sslmode=disable"
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fmt.Println("connect error:", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	queries := map[string]string{
		"total_users":    "SELECT COUNT(*) FROM users WHERE appid = 10000",
		"enabled_users":  "SELECT COUNT(*) FROM users WHERE appid = 10000 AND enabled = true",
		"banners":        "SELECT COUNT(*) FROM banners WHERE appid = 10000",
		"notices":        "SELECT COUNT(*) FROM notices WHERE appid = 10000",
		"oauth_bindings": "SELECT COUNT(*) FROM oauth_bindings WHERE appid = 10000",
		"login_audit":    "SELECT COUNT(*) FROM login_audit_logs WHERE appid = 10000 AND status = 'success' AND created_at >= date_trunc('day', NOW())",
	}
	for name, q := range queries {
		var count int64
		err := conn.QueryRow(ctx, q).Scan(&count)
		if err != nil {
			fmt.Printf("  %s: ERROR: %v\n", name, err)
		} else {
			fmt.Printf("  %s: %d\n", name, count)
		}
	}

	// 测试完整查询
	fmt.Println("\nFull stats query:")
	fullQuery := `SELECT
	$1 AS appid,
	(SELECT COUNT(*) FROM users WHERE appid = $1) AS total_users,
	(SELECT COUNT(*) FROM users WHERE appid = $1 AND enabled = true) AS enabled_users,
	(SELECT COUNT(*) FROM users WHERE appid = $1 AND enabled = false) AS disabled_users,
	(SELECT COUNT(*) FROM banners WHERE appid = $1) AS banner_count,
	(SELECT COUNT(*) FROM notices WHERE appid = $1) AS notice_count,
	(SELECT COUNT(*) FROM oauth_bindings WHERE appid = $1) AS oauth_bind_count,
	(SELECT COUNT(*) FROM users WHERE appid = $1 AND created_at >= date_trunc('day', NOW())) AS new_users_today,
	(SELECT COUNT(*) FROM users WHERE appid = $1 AND created_at >= date_trunc('day', NOW()) - INTERVAL '6 day') AS new_users_last_7_days,
	(SELECT COUNT(*) FROM users WHERE appid = $1 AND created_at >= date_trunc('day', NOW()) - INTERVAL '29 day') AS new_users_last_30_days,
	(SELECT COUNT(*) FROM login_audit_logs WHERE appid = $1 AND status = 'success' AND created_at >= date_trunc('day', NOW())) AS login_success_today,
	(SELECT COUNT(*) FROM login_audit_logs WHERE appid = $1 AND status <> 'success' AND created_at >= date_trunc('day', NOW())) AS login_failure_today`
	var appid, total, enabled, disabled, banners, notices, oauth, newToday, new7, new30, loginOk, loginFail int64
	err = conn.QueryRow(ctx, fullQuery, int64(10000)).Scan(&appid, &total, &enabled, &disabled, &banners, &notices, &oauth, &newToday, &new7, &new30, &loginOk, &loginFail)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
	} else {
		fmt.Printf("  appid=%d total=%d enabled=%d disabled=%d banners=%d notices=%d oauth=%d newToday=%d new7=%d new30=%d loginOk=%d loginFail=%d\n",
			appid, total, enabled, disabled, banners, notices, oauth, newToday, new7, new30, loginOk, loginFail)
	}
}
