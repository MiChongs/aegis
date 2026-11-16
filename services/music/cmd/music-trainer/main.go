// music-trainer 音乐个性化微服务之一（docs/music-personalization.md），配置见 services/music/internal/config（环境变量 MUSIC_*）。
package main

import (
	"fmt"
	"os"

	"aegis/services/music/internal/app"
	"aegis/services/music/internal/config"
)

// version 构建时注入：-ldflags "-X main.version=..."
var version = ""

func main() {
	cfg, err := config.Load()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if version != "" {
		cfg.Version = version
	}
	app.New(cfg, app.RoleTrainer).Run()
}
