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

	// 查看当前状态
	rows, _ := conn.Query(ctx, "SELECT id, account, display_name, status, is_super_admin FROM admin_accounts ORDER BY id")
	fmt.Println("当前管理员状态:")
	for rows.Next() {
		var id int64
		var account, displayName, status string
		var isSuperAdmin bool
		rows.Scan(&id, &account, &displayName, &status, &isSuperAdmin)
		fmt.Printf("  id=%d account=%s name=%s status=%s super=%v\n", id, account, displayName, status, isSuperAdmin)
	}
	rows.Close()

	// 修复：将所有超级管理员恢复为 active
	tag, err := conn.Exec(ctx, "UPDATE admin_accounts SET status = 'active', updated_at = NOW() WHERE is_super_admin = TRUE AND status = 'disabled'")
	if err != nil {
		fmt.Println("update error:", err)
		os.Exit(1)
	}
	fmt.Printf("\n已恢复 %d 个被禁用的超级管理员\n", tag.RowsAffected())

	// 验证
	rows, _ = conn.Query(ctx, "SELECT id, account, status, is_super_admin FROM admin_accounts ORDER BY id")
	fmt.Println("\n修复后状态:")
	for rows.Next() {
		var id int64
		var account, status string
		var isSuperAdmin bool
		rows.Scan(&id, &account, &status, &isSuperAdmin)
		fmt.Printf("  id=%d account=%s status=%s super=%v\n", id, account, status, isSuperAdmin)
	}
	rows.Close()
}
