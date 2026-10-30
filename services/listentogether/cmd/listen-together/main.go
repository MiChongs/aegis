// listen-together 一起听微服务：独立进程、独立部署，与 Aegis 主服务只经票据、NATS 与内部 RPC 交互。
// 配置见 services/listentogether/internal/config（环境变量 LT_*），设计见 docs/listen-together.md。
package main

import (
	"fmt"
	"os"

	"aegis/services/listentogether/internal/app"
	"aegis/services/listentogether/internal/config"
)

// version 构建时注入：-ldflags "-X main.version=..."
var version = ""

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if version != "" {
		cfg.Version = version
	}
	app.New(cfg).Run()
}
