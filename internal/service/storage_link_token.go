package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
)

// 存储代理地址的寻址令牌。
//
// ── 这里修的是什么 ────────────────────────────────────────────────
//
// 原来的 `/api/storage/proxy/{ticket}` 里，ticket 是 Redis 里的一个随机 key，
// 最长活 1 小时（不传时 10 分钟）。地址一旦交出去就会被存下来：
// 上传接口的返回值、控制台复制的链接、Service Worker 的 30 天缓存、
// 客户端本地库、别人贴进横幅/发布包的外链 —— 票据一过期，全部 404。
// 这就是「图片上传后过一段时间打不开」。票据只是一个随机数，
// 过期后连它指向哪个对象都查不回来，所以也无从补救。
//
// ── 现在的做法 ────────────────────────────────────────────────────
//
// 令牌自己携带「哪个配置的哪个对象」，用平台主密钥派生的专用密钥签名：
//
//	{base64url(payload)}.{base64url(sig)}
//
// 不落 Redis、不怕重启、不怕 Redis 被清。payload 里的 e 是到期时间：
//   - e = 0：永久地址。给横幅、发布包、工单附件、上传返回值这类「会被存下来」的内容用；
//     对象被删除后地址自然 404，不需要单独吊销。
//   - e > 0：限时地址。给云存储这类用户私有数据、以及调用方显式要求时效的链接用。
//
// 签名同时挡住两件事：伪造任意对象键读别人的文件、按序遍历存储桶。
// 换 SECURITY_MASTER_KEY 会让所有已交出的地址失效 —— 与其它派生密钥一致。
const (
	// storageLinkSigBytes 签名长度。16 字节 = 128 位：这里保护的是对象内容本身，
	// 比头像令牌（只防遍历）要求高。
	storageLinkSigBytes = 16
	// storageLinkMaxTTL 限时地址的上限。无状态以后时效不再占 Redis，
	// 上限只是为了不让「限时」退化成事实上的永久。
	storageLinkMaxTTL = 7 * 24 * time.Hour
)

// storageLinkClaims 令牌负载。字段名刻意压短：它会原样出现在 URL 里。
type storageLinkClaims struct {
	ConfigID  int64  `json:"c"`
	ObjectKey string `json:"k"`
	AppID     int64  `json:"a,omitempty"`
	Download  bool   `json:"d,omitempty"`
	FileName  string `json:"n,omitempty"`
	ExpiresAt int64  `json:"e,omitempty"`
}

type storageLinkSigner struct {
	key [32]byte
}

// newStorageLinkSigner 从平台主密钥派生专用密钥，用途盐与 aegis.avatar.link 等同构。
func newStorageLinkSigner(masterKey string) *storageLinkSigner {
	return &storageLinkSigner{key: sha256.Sum256([]byte("aegis.storage.link\x00" + masterKey))}
}

// newEphemeralStorageLinkSigner 未注入主密钥时的兜底（测试、未走 bootstrap 的工具）：
// 随机密钥，签出的地址只在本进程内有效。
func newEphemeralStorageLinkSigner() *storageLinkSigner {
	var seed [32]byte
	_, _ = rand.Read(seed[:])
	return &storageLinkSigner{key: seed}
}

func (s *storageLinkSigner) Encode(claims storageLinkClaims) (string, error) {
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := avatarTokenEncoding.EncodeToString(raw)
	return payload + "." + avatarTokenEncoding.EncodeToString(s.sign(payload)), nil
}

// Decode 解析并验签、校验时效。任何一步不过都只回答「不存在」。
func (s *storageLinkSigner) Decode(token string, now time.Time) (*storagedomain.ProxyTicket, bool) {
	token = strings.TrimSpace(token)
	idx := strings.LastIndex(token, ".")
	if idx <= 0 || idx == len(token)-1 {
		return nil, false
	}
	payload := token[:idx]
	sig, err := avatarTokenEncoding.DecodeString(token[idx+1:])
	if err != nil || !hmac.Equal(sig, s.sign(payload)) {
		return nil, false
	}
	raw, err := avatarTokenEncoding.DecodeString(payload)
	if err != nil {
		return nil, false
	}
	var claims storageLinkClaims
	if err := json.Unmarshal(raw, &claims); err != nil || claims.ConfigID <= 0 || strings.TrimSpace(claims.ObjectKey) == "" {
		return nil, false
	}
	ticket := &storagedomain.ProxyTicket{
		AppID:     claims.AppID,
		ConfigID:  claims.ConfigID,
		ObjectKey: claims.ObjectKey,
		Download:  claims.Download,
		FileName:  claims.FileName,
	}
	if claims.ExpiresAt > 0 {
		ticket.ExpiresAt = time.Unix(claims.ExpiresAt, 0)
		if now.After(ticket.ExpiresAt) {
			return nil, false
		}
	}
	return ticket, true
}

func (s *storageLinkSigner) sign(payload string) []byte {
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write([]byte(payload))
	return mac.Sum(nil)[:storageLinkSigBytes]
}

// isStorageLinkToken 区分新令牌与旧的 Redis 票据（32 位十六进制，不含点）。
func isStorageLinkToken(token string) bool {
	return strings.Contains(token, ".")
}
