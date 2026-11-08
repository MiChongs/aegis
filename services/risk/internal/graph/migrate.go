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
		// graphd 首次启动只有 root / nebula：配置了别的密码就用默认密码登录改掉，再用新密码重连
		if cfg.NebulaUser != "root" || cfg.NebulaPassword == defaultRootPassword {
			return err
		}
		if cerr := changeDefaultPassword(pool, cfg.NebulaPassword); cerr != nil {
			return fmt.Errorf("%w（用默认密码改密也失败：%v）", err, cerr)
		}
		logf("nebula：root 默认密码已改为配置的密码")
		// graphd 从 metad 同步账号有几秒延迟（实测约 8 秒），期间新密码登录会报 Invalid password
		deadline := time.Now().Add(60 * time.Second)
		for {
			if session, err = pool.GetSession(cfg.NebulaUser, cfg.NebulaPassword); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return err
			}
			time.Sleep(2 * time.Second)
		}
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
	for _, stmt := range SchemaAdditions() {
		if _, err := exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "existed") {
			return err
		}
	}
	// schema 同样要等心跳才对写入生效：新点类型与新加的属性各试写一次确认
	deadline = time.Now().Add(60 * time.Second)
	for {
		_, err := exec(`UPSERT VERTEX ON subnet "sn:__probe__" SET cidr = "probe"; UPSERT VERTEX ON identity "oa:__probe__" SET provider = "probe"; UPSERT VERTEX ON user "u:__probe__" SET nickname = "probe"; UPSERT VERTEX ON ip "ip:__probe__" SET vpn = false, shared = false`)
		if err == nil {
			_, _ = exec(`DELETE VERTEX "sn:__probe__", "oa:__probe__", "u:__probe__", "ip:__probe__"`)
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

const defaultRootPassword = "nebula"

// maxPasswordLen NebulaGraph 密码最长 24 个字符（更长的 CHANGE PASSWORD 报 SemanticError）
const maxPasswordLen = 24

func changeDefaultPassword(pool *nebula.ConnectionPool, password string) error {
	if len(password) > maxPasswordLen {
		return fmt.Errorf("RISK_NEBULA_PASSWORD 有 %d 个字符，NebulaGraph 最长 %d 个", len(password), maxPasswordLen)
	}
	session, err := pool.GetSession("root", defaultRootPassword)
	if err != nil {
		return err
	}
	defer session.Release()
	rs, err := session.Execute(fmt.Sprintf("CHANGE PASSWORD root FROM %s TO %s", Quote(defaultRootPassword), Quote(password)))
	if err != nil {
		return err
	}
	if !rs.IsSucceed() {
		return fmt.Errorf("%s", rs.GetErrorMsg())
	}
	return nil
}
