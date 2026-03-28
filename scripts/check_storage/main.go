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

	var count int
	conn.QueryRow(ctx, "SELECT COUNT(*) FROM storage_configs").Scan(&count)
	fmt.Println("total configs:", count)

	rows, _ := conn.Query(ctx, "SELECT id, scope, appid, provider, config_name, enabled, is_default FROM storage_configs ORDER BY id")
	defer rows.Close()
	for rows.Next() {
		var id int64
		var scope, provider, configName string
		var appid *int64
		var enabled, isDefault bool
		rows.Scan(&id, &scope, &appid, &provider, &configName, &enabled, &isDefault)
		fmt.Printf("  id=%d scope=%s provider=%s name=%s enabled=%v default=%v\n", id, scope, provider, configName, enabled, isDefault)
	}
}
