// Package graph 风控关系图（NebulaGraph）。
//
// 点：user（应用用户）、account（登录名）、device、ip、subnet、phone、email、
// identity（第三方登录身份，如 QQ / Google）、payer（付款账号）、passkey（通行密钥）、client（用户授权过的第三方应用）。
// 边：uses_device、uses_ip、in_subnet、has_phone、has_email、account_of、invited、targets、
// registered_from（注册 IP）、registered_on（注册设备）、bound_identity、paid_with、has_passkey、authorized。
// 边上记 first_seen / last_seen（秒）与 count。点 ID 带类型前缀，用户按应用隔离：u:<应用>:<用户>。
package graph

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"aegis/services/risk/internal/config"

	nebula "github.com/vesoft-inc/nebula-go/v3"
)

// 点类型
const (
	TagUser     = "user"
	TagAccount  = "account"
	TagDevice   = "device"
	TagIP       = "ip"
	TagSubnet   = "subnet"
	TagPhone    = "phone"
	TagEmail    = "email"
	TagIdentity = "identity"
	TagPayer    = "payer"
	TagPasskey  = "passkey"
	TagClient   = "client"
)

// 边类型
const (
	EdgeUsesDevice     = "uses_device"
	EdgeUsesIP         = "uses_ip"
	EdgeInSubnet       = "in_subnet"
	EdgeHasPhone       = "has_phone"
	EdgeHasEmail       = "has_email"
	EdgeAccountOf      = "account_of"
	EdgeInvited        = "invited"
	EdgeTargets        = "targets"
	EdgeRegisteredFrom = "registered_from"
	EdgeRegisteredOn   = "registered_on"
	EdgeBoundIdentity  = "bound_identity"
	EdgePaidWith       = "paid_with"
	EdgeHasPasskey     = "has_passkey"
	EdgeAuthorized     = "authorized"
)

// Edges 全部边类型
func Edges() []string {
	return []string{EdgeUsesDevice, EdgeUsesIP, EdgeInSubnet, EdgeHasPhone, EdgeHasEmail, EdgeAccountOf, EdgeInvited, EdgeTargets,
		EdgeRegisteredFrom, EdgeRegisteredOn, EdgeBoundIdentity, EdgePaidWith, EdgeHasPasskey, EdgeAuthorized}
}

// Schema 建空间之后执行的 DDL（IF NOT EXISTS，可重复执行）
func Schema() []string {
	edge := "first_seen int DEFAULT 0, last_seen int DEFAULT 0, count int DEFAULT 0"
	stmts := []string{
		"CREATE TAG IF NOT EXISTS user(app_id int DEFAULT 0, uid int DEFAULT 0, name string DEFAULT \"\", status string DEFAULT \"\", risk_level string DEFAULT \"\", banned bool DEFAULT false, last_action string DEFAULT \"\", first_seen int DEFAULT 0, last_seen int DEFAULT 0)",
		"CREATE TAG IF NOT EXISTS account(app_id int DEFAULT 0, name string DEFAULT \"\", first_seen int DEFAULT 0, last_seen int DEFAULT 0)",
		"CREATE TAG IF NOT EXISTS device(device_id string DEFAULT \"\", platform string DEFAULT \"\", brand string DEFAULT \"\", model string DEFAULT \"\", emulator bool DEFAULT false, rooted bool DEFAULT false, banned bool DEFAULT false, first_seen int DEFAULT 0, last_seen int DEFAULT 0)",
		"CREATE TAG IF NOT EXISTS ip(addr string DEFAULT \"\", country string DEFAULT \"\", province string DEFAULT \"\", city string DEFAULT \"\", isp string DEFAULT \"\", asn int DEFAULT 0, proxy bool DEFAULT false, datacenter bool DEFAULT false, banned bool DEFAULT false, first_seen int DEFAULT 0, last_seen int DEFAULT 0)",
		"CREATE TAG IF NOT EXISTS subnet(cidr string DEFAULT \"\", first_seen int DEFAULT 0, last_seen int DEFAULT 0)",
		"CREATE TAG IF NOT EXISTS phone(masked string DEFAULT \"\", prefix string DEFAULT \"\", first_seen int DEFAULT 0, last_seen int DEFAULT 0)",
		"CREATE TAG IF NOT EXISTS email(masked string DEFAULT \"\", domain string DEFAULT \"\", first_seen int DEFAULT 0, last_seen int DEFAULT 0)",
	}
	stmts = append(stmts,
		"CREATE TAG IF NOT EXISTS identity(provider string DEFAULT \"\", nickname string DEFAULT \"\", region string DEFAULT \"\", first_seen int DEFAULT 0, last_seen int DEFAULT 0)",
		"CREATE TAG IF NOT EXISTS payer(channel string DEFAULT \"\", masked string DEFAULT \"\", first_seen int DEFAULT 0, last_seen int DEFAULT 0)",
		"CREATE TAG IF NOT EXISTS passkey(name string DEFAULT \"\", first_seen int DEFAULT 0, last_seen int DEFAULT 0)",
		"CREATE TAG IF NOT EXISTS client(client_id string DEFAULT \"\", name string DEFAULT \"\", first_seen int DEFAULT 0, last_seen int DEFAULT 0)",
	)
	for _, e := range Edges() {
		stmts = append(stmts, fmt.Sprintf("CREATE EDGE IF NOT EXISTS %s(%s)", e, edge))
	}
	return stmts
}

// SchemaAdditions 已有点类型后加的属性（ALTER TAG ADD，已存在时 Nebula 报 Existed，迁移里忽略）
func SchemaAdditions() []string {
	return []string{
		"ALTER TAG user ADD (nickname string DEFAULT \"\", registered_at int DEFAULT 0)",
		"ALTER TAG device ADD (os_version string DEFAULT \"\", app_version string DEFAULT \"\", name string DEFAULT \"\")",
		"ALTER TAG ip ADD (vpn bool DEFAULT false, tor bool DEFAULT false)",
		// users：用过这个 IP 的不同用户数；hosting：机房 / 云主机 / 代理出口；shared：共享出口（用户数达到 proxyip.SharedUsers）。
		// 后两者为真的 IP 是"弱关系"：关系图上不穿过它们展开，图特征里不按它们统计同 IP 账号。
		"ALTER TAG ip ADD (users int DEFAULT 0, hosting bool DEFAULT false, shared bool DEFAULT false)",
	}
}

// Client 图数据库会话池
type Client struct {
	pool  *nebula.SessionPool
	space string
}

type quietLogger struct{ nebula.DefaultLogger }

func (quietLogger) Info(string) {}

// HostAddrs 解析 host:port 列表
func HostAddrs(addrs []string) ([]nebula.HostAddress, error) {
	var out []nebula.HostAddress
	for _, a := range addrs {
		host, port, ok := strings.Cut(a, ":")
		if !ok {
			return nil, fmt.Errorf("地址 %q 缺端口", a)
		}
		p, err := strconv.Atoi(port)
		if err != nil {
			return nil, err
		}
		out = append(out, nebula.HostAddress{Host: host, Port: p})
	}
	return out, nil
}

// Open 连图空间（空间须已由 migrate 建好）
func Open(cfg config.Config) (*Client, error) {
	hosts, err := HostAddrs(cfg.NebulaAddrs)
	if err != nil {
		return nil, err
	}
	conf, err := nebula.NewSessionPoolConf(cfg.NebulaUser, cfg.NebulaPassword, hosts, cfg.NebulaSpace,
		nebula.WithMaxSize(64), nebula.WithMinSize(2), nebula.WithTimeOut(3*time.Second), nebula.WithIdleTime(10*time.Minute))
	if err != nil {
		return nil, err
	}
	pool, err := nebula.NewSessionPool(*conf, quietLogger{})
	if err != nil {
		return nil, err
	}
	return &Client{pool: pool, space: cfg.NebulaSpace}, nil
}

// Close 关闭
func (c *Client) Close() {
	if c != nil && c.pool != nil {
		c.pool.Close()
	}
}

// Exec 执行并检查错误
func (c *Client) Exec(stmt string) (*nebula.ResultSet, error) {
	rs, err := c.pool.Execute(stmt)
	if err != nil {
		return nil, err
	}
	if !rs.IsSucceed() {
		return rs, fmt.Errorf("nebula: %s", rs.GetErrorMsg())
	}
	return rs, nil
}

// 点 ID

func hashed(prefix, v string) string {
	if len(prefix)+len(v) <= 90 {
		return prefix + v
	}
	sum := sha1.Sum([]byte(v))
	return prefix + "#" + hex.EncodeToString(sum[:])
}

// UserVID 用户点
func UserVID(appID, userID int64) string { return "u:" + itoa(appID) + ":" + itoa(userID) }

// AccountVID 登录名点
func AccountVID(appID int64, account string) string {
	return hashed("a:"+itoa(appID)+":", strings.ToLower(account))
}

// DeviceVID 设备点
func DeviceVID(id string) string { return hashed("d:", id) }

// IPVID IP 点
func IPVID(ip string) string { return "ip:" + ip }

// SubnetVID 网段点
func SubnetVID(cidr string) string { return "sn:" + cidr }

// PhoneVID 手机号点（只存摘要，展示用打码值）
func PhoneVID(phone string) string {
	sum := sha1.Sum([]byte(phone))
	return "ph:" + hex.EncodeToString(sum[:12])
}

// EmailVID 邮箱点
func EmailVID(email string) string {
	sum := sha1.Sum([]byte(strings.ToLower(email)))
	return "em:" + hex.EncodeToString(sum[:12])
}

// digest 定长摘要（身份标识、付款账号这类不宜原样放进点 ID 的值）
func digest(prefix, v string) string {
	sum := sha1.Sum([]byte(v))
	return prefix + hex.EncodeToString(sum[:12])
}

// IdentityVID 第三方登录身份：同一个 QQ unionid / Google sub 是同一个点（跨应用也是同一个人）
func IdentityVID(provider, subject string) string {
	return digest("oa:"+strings.ToLower(provider)+":", subject)
}

// PayerVID 付款账号（如爱发电用户）
func PayerVID(channel, account string) string {
	return digest("py:"+strings.ToLower(channel)+":", account)
}

// PasskeyVID 通行密钥（按凭据 ID）
func PasskeyVID(credentialID string) string { return digest("pk:", credentialID) }

// ClientVID 用户授权过的第三方应用（OAuth2 客户端）
func ClientVID(clientID string) string { return hashed("c:", clientID) }

// Kind 从点 ID 取类型
func Kind(vid string) string {
	switch {
	case strings.HasPrefix(vid, "oa:"):
		return TagIdentity
	case strings.HasPrefix(vid, "py:"):
		return TagPayer
	case strings.HasPrefix(vid, "pk:"):
		return TagPasskey
	case strings.HasPrefix(vid, "c:"):
		return TagClient
	case strings.HasPrefix(vid, "u:"):
		return TagUser
	case strings.HasPrefix(vid, "a:"):
		return TagAccount
	case strings.HasPrefix(vid, "d:"):
		return TagDevice
	case strings.HasPrefix(vid, "ip:"):
		return TagIP
	case strings.HasPrefix(vid, "sn:"):
		return TagSubnet
	case strings.HasPrefix(vid, "ph:"):
		return TagPhone
	case strings.HasPrefix(vid, "em:"):
		return TagEmail
	}
	return ""
}

// MaskPhone 138****8000
func MaskPhone(p string) string {
	if len(p) < 7 {
		return "****"
	}
	return p[:3] + "****" + p[len(p)-4:]
}

// MaskEmail a***@qq.com
func MaskEmail(e string) string {
	at := strings.LastIndexByte(e, '@')
	if at <= 0 {
		return "***"
	}
	return e[:1] + "***" + e[at:]
}

// Quote nGQL 字符串字面量
func Quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
	return `"` + r.Replace(s) + `"`
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// ErrNotReady 图服务没连上
var ErrNotReady = errors.New("关系图未就绪")
