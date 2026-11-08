package graph

import (
	"fmt"
	"strings"
	"time"

	"aegis/services/risk/internal/config"

	nebula "github.com/vesoft-inc/nebula-go/v3"
)

// Migrate 注册 storaged、建空间与 schema。可重复执行。
func Migrate(cfg config.Config, logf func(string, ...any)) error {
	hosts, err := HostAddrs(cfg.NebulaAddrs)
	if err != nil {
		return err
	}
	pool, err := nebula.NewConnectionPool(hosts, nebula.GetDefaultConf(), quietLogger{})
	if err != nil {
		return err
	}
	defer pool.Close()
	session, err := pool.GetSession(cfg.NebulaUser, cfg.NebulaPassword)
	if err != nil {
		return err
	}
	defer session.Release()
	exec := func(stmt string) (*nebula.ResultSet, error) {
		rs, err := session.Execute(stmt)
		if err != nil {
			return nil, err
		}
		if !rs.IsSucceed() {
			return rs, fmt.Errorf("%s: %s", stmt, rs.GetErrorMsg())
		}
		return rs, nil
	}

	// 3.x 起 storaged 要显式加进集群
	if len(cfg.NebulaStoraged) > 0 {
		parts := make([]string, len(cfg.NebulaStoraged))
		for i, h := range cfg.NebulaStoraged {
			host, port, _ := strings.Cut(h, ":")
			parts[i] = fmt.Sprintf("%s:%s", Quote(host), port)
		}
		if _, err := exec("ADD HOSTS " + strings.Join(parts, ", ")); err != nil && !strings.Contains(err.Error(), "xisted") {
			return err
		}
	}
	// 等 storaged 上线
	deadline := time.Now().Add(90 * time.Second)
	for {
		rs, err := exec("SHOW HOSTS")
		online := 0
		if err == nil {
			for _, row := range rs.AsStringTable() {
				for _, cell := range row {
					if strings.Contains(cell, "ONLINE") {
						online++
						break
					}
				}
			}
		}
		if online > 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("storaged 90 秒内没有上线")
		}
		time.Sleep(2 * time.Second)
	}

	if _, err := exec(fmt.Sprintf("CREATE SPACE IF NOT EXISTS %s (partition_num = 15, replica_factor = 1, vid_type = FIXED_STRING(96))", cfg.NebulaSpace)); err != nil {
		return err
	}
	// 新空间要等一个心跳周期（默认 10 秒）才能 USE
	deadline = time.Now().Add(60 * time.Second)
	for {
		if _, err := exec("USE " + cfg.NebulaSpace); err == nil {
			break
		} else if time.Now().After(deadline) {
			return err
		}
		time.Sleep(2 * time.Second)
	}
	for _, stmt := range Schema() {
		if _, err := exec(stmt); err != nil {
			return err
		}
	}
	// schema 同样要等心跳才对写入生效：试写一次确认
	deadline = time.Now().Add(60 * time.Second)
	for {
		_, err := exec(`UPSERT VERTEX ON subnet "sn:__probe__" SET cidr = "probe"`)
		if err == nil {
			_, _ = exec(`DELETE VERTEX "sn:__probe__"`)
			break
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(2 * time.Second)
	}
	if logf != nil {
		logf("nebula: 空间 %s 就绪", cfg.NebulaSpace)
	}
	return nil
}
