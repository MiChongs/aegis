package events

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// Digest 不宜原样外传的标识（第三方账号的 openid / unionid、通行密钥的凭据 ID）的定长摘要。
// 主服务上报事件时只带摘要；关系图用同一个算法建点（graph.IdentityVID 等），
// 回灌（从主库原值算）与实时事件（带摘要）落到同一个点上。
func Digest(v string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(v)))
	return hex.EncodeToString(sum[:12])
}

// 事件 Props 里约定的键
const (
	PropIdentityProvider = "identity_provider" // qq / google / ……
	PropIdentityKey      = "identity_key"      // Digest(unionid 或 openid)
	PropIdentityNickname = "identity_nickname"
	PropClientID         = "client_id" // OAuth2 客户端
	PropClientName       = "client_name"
	PropPasskeyKey       = "passkey_key" // Digest(凭据 ID 的十六进制)
	PropPasskeyName      = "passkey_name"
)
