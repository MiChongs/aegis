package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	avatardomain "aegis/internal/domain/avatar"
	apperrors "aegis/pkg/errors"
)

// 头像的寻址与落库形态。
//
// ── 这里修的是什么 ────────────────────────────────────────────────
//
// 原来的链路是：上传时把 `storage://{configID}/{objectKey}` 写进
// user_profiles.avatar，每次读资料时现签一个 **30 分钟** 的代理票据
// （Redis 里的一次性 key），把 `/api/storage/proxy/{ticket}` 交给客户端。
//
// 交出去的是一个会过期的地址，而客户端理所当然会把它存下来：
// 控制台存进 localStorage（zustand persist）、移动端存进本地库、
// 邮件正文里嵌成 <img>、中间还可能有 CDN。半小时后这些副本全部变成死链 ——
// 表现就是「头像过一阵子就没了」。
//
// 更严重的是第二跳：有客户端会把读回来的整份资料原样 PUT 回来
// （读-改-写是最常见的表单写法）。于是那个临时地址被写回数据库，
// 覆盖掉唯一那份 storage:// 引用。这之后头像不是过期，是**永久丢失**：
// 库里再没有任何东西指向那个对象。这条链路只在自定义上传时存在，
// 因为只有它才产生 storage:// 引用 —— 第三方头像和 Gravatar 都是永久外链。
//
// ── 现在的做法 ────────────────────────────────────────────────────
//
//	落库：仍是 storage:// 引用（存量数据零迁移）
//	出网：/api/avatars/{ownerToken}?v={version} —— **永久地址**
//
// ownerToken 编码的是「谁」而不是「哪个对象」。因此换了头像地址不变
// （v 变，用于破缓存），旧地址永远解析到这个人当前的头像；没有头像时
// 解析到服务端生成的默认头像。**这个地址不会失效，也就没有什么可写坏的**。
//
// 签名不是为了保密（头像本来就是半公开的），是为了挡住按 ID 遍历
// 把全站用户头像刷走。
const (
	// avatarLinkPath 永久头像地址的路由前缀。改它等于让所有已经流出去的
	// 地址失效，包括邮件里那些 —— 除非同时保留旧前缀的兼容。
	avatarLinkPath = "/api/avatars/"
	// avatarProxyPathMarker 旧的临时票据地址特征。识别它是为了**拒绝**它
	// 被写进数据库，而不是为了使用它。
	avatarProxyPathMarker = "/api/storage/proxy/"
	// avatarOwnerSigBytes 主体令牌的签名长度。9 字节 = 72 位，
	// 伪造一条要试 2^71 次，而它保护的只是"别被遍历"，没必要更长。
	avatarOwnerSigBytes = 9
)

var avatarTokenEncoding = base64.RawURLEncoding

// avatarLinkSigner 主体令牌的签名器。
type avatarLinkSigner struct {
	key [32]byte
}

// newAvatarLinkSigner 从平台主密钥派生一把专用密钥。
//
// 用途盐与其它派生（aegis.email.master / aegis.notify.master …）同构：
// 直接拿主密钥当 HMAC key 会让不同用途的签名互相可伪造。
func newAvatarLinkSigner(masterKey string) *avatarLinkSigner {
	return &avatarLinkSigner{key: sha256.Sum256([]byte("aegis.avatar.link\x00" + masterKey))}
}

// EncodeOwner 把主体编码成一段可放进 URL 的短令牌。
func (s *avatarLinkSigner) EncodeOwner(owner avatardomain.Owner) string {
	if s == nil || !owner.Valid() {
		return ""
	}
	payload := avatarOwnerPayload(owner)
	return avatarTokenEncoding.EncodeToString([]byte(payload)) + "." + avatarTokenEncoding.EncodeToString(s.sign(payload))
}

// DecodeOwner 解析并验签。签名不过一律当作"这个令牌不存在"，
// 不区分"格式不对"与"签名不对" —— 区分开等于告诉刷库的人还差哪一步。
func (s *avatarLinkSigner) DecodeOwner(token string) (avatardomain.Owner, bool) {
	if s == nil {
		return avatardomain.Owner{}, false
	}
	token = strings.TrimSpace(token)
	idx := strings.LastIndex(token, ".")
	if idx <= 0 || idx == len(token)-1 {
		return avatardomain.Owner{}, false
	}
	payloadRaw, err := avatarTokenEncoding.DecodeString(token[:idx])
	if err != nil {
		return avatardomain.Owner{}, false
	}
	sig, err := avatarTokenEncoding.DecodeString(token[idx+1:])
	if err != nil {
		return avatardomain.Owner{}, false
	}
	payload := string(payloadRaw)
	if !hmac.Equal(sig, s.sign(payload)) {
		return avatardomain.Owner{}, false
	}
	owner, ok := parseAvatarOwnerPayload(payload)
	if !ok || !owner.Valid() {
		return avatardomain.Owner{}, false
	}
	return owner, true
}

func (s *avatarLinkSigner) sign(payload string) []byte {
	mac := hmac.New(sha256.New, s.key[:])
	mac.Write([]byte(payload))
	return mac.Sum(nil)[:avatarOwnerSigBytes]
}

// avatarOwnerPayload 主体的紧凑表示。
//
// 明文编码而不是加密：这里没有秘密（用户 ID 在任何一个列表接口里都能拿到），
// 而可读的载荷让排障时一眼能看出「这个头像地址是谁的」。
func avatarOwnerPayload(owner avatardomain.Owner) string {
	if owner.Type == avatardomain.OwnerAdmin {
		return "a" + strconv.FormatInt(owner.ID, 10)
	}
	return "u" + strconv.FormatInt(owner.AppID, 10) + "." + strconv.FormatInt(owner.ID, 10)
}

func parseAvatarOwnerPayload(payload string) (avatardomain.Owner, bool) {
	if len(payload) < 2 {
		return avatardomain.Owner{}, false
	}
	switch payload[0] {
	case 'a':
		id, err := strconv.ParseInt(payload[1:], 10, 64)
		if err != nil {
			return avatardomain.Owner{}, false
		}
		return avatardomain.Owner{Type: avatardomain.OwnerAdmin, ID: id}, true
	case 'u':
		parts := strings.SplitN(payload[1:], ".", 2)
		if len(parts) != 2 {
			return avatardomain.Owner{}, false
		}
		appID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return avatardomain.Owner{}, false
		}
		userID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return avatardomain.Owner{}, false
		}
		return avatardomain.Owner{Type: avatardomain.OwnerUser, AppID: appID, ID: userID}, true
	default:
		return avatardomain.Owner{}, false
	}
}

// buildAvatarLink 拼出永久头像地址。
//
// version 只用来破缓存：服务端解析时**不看**它，永远返回该主体当前的头像。
// 这一条正是"地址不会失效"的落点 —— 客户端手上那份两年前的副本，
// 今天点开拿到的仍然是这个人今天的头像。
func buildAvatarLink(baseURL string, token string, version string) string {
	if strings.TrimSpace(token) == "" {
		return ""
	}
	link := joinURL(baseURL, avatarLinkPath+url.PathEscape(token))
	if version = strings.TrimSpace(version); version != "" {
		link += "?v=" + url.QueryEscape(version)
	}
	return link
}

// isAegisAvatarLink 这个地址是不是我们自己发出去的永久头像地址。
func isAegisAvatarLink(raw string) bool {
	return strings.Contains(raw, avatarLinkPath)
}

// isEphemeralStorageLink 这个地址是不是**会过期**的存储代理票据。
//
// 识别它唯一的用处是拒绝把它写进数据库。它可能来自两个地方：
// 老版本服务端交出去的头像地址，以及客户端读-改-写时原样回传的资料。
func isEphemeralStorageLink(raw string) bool {
	return strings.Contains(raw, avatarProxyPathMarker)
}

// NormalizeAvatarInput 决定客户端提交的头像值该不该落库、以什么形态落库。
//
// 这是「头像消失」的第二道、也是决定性的一道闸门。调用方（更新资料的三条链路）
// 把客户端传来的值和库里当前的值交给它，拿回该写进去的值：
//
//	临时代理地址      → 保持原值不动（写进去等于亲手销毁唯一的引用）
//	自家永久头像地址  → 保持原值不动（那是我们发出去的展示形态，不是引用）
//	storage:// 引用   → **拒绝**，除非与当前值完全一致
//	http(s) 外链      → 放行（第三方头像）
//	其它协议          → 拒绝
//
// storage:// 那一条不只是为了完整性：不挡的话任何登录用户都可以把自己的
// 头像设成 `storage://3/别人的私有文件.pdf`，然后从头像地址上把它读出来。
// 这个引用只应该由服务端在上传成功后自己产生。
func NormalizeAvatarInput(raw string, current string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if isEphemeralStorageLink(trimmed) || isAegisAvatarLink(trimmed) {
		return strings.TrimSpace(current), nil
	}
	if _, _, ok := parseStorageReference(trimmed); ok {
		if trimmed == strings.TrimSpace(current) {
			return trimmed, nil
		}
		return "", apperrors.New(40092, http.StatusBadRequest,
			"头像引用只能由上传接口生成，请改用 POST /me/avatar 上传头像")
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return trimmed, nil
	}
	return "", apperrors.New(40093, http.StatusBadRequest,
		"头像地址只支持 http(s) 链接，或通过上传接口上传图片")
}

// SanitizeExternalAvatar 收下一个**外部来源**的头像地址，不合规就当没有。
//
// 与 NormalizeAvatarInput 的分工：那个用在「改资料」上，有当前值可以退回；
// 这个用在**建号**上（第三方登录自动注册），那时候没有当前值，
// 也没有任何理由报错打断一次登录 —— 头像不合规就是没头像，服务端会画一个默认的。
//
// 挡的是同一件事：原生 exchange 那条链路上，`avatar` 是**客户端请求体里的字段**
// （不是服务端从 userinfo 拉的），因此可以被填成 `storage://3/别人的私有文件.pdf`，
// 注册完再从自己的头像地址上把它读出来。
func SanitizeExternalAvatar(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return ""
	}
	// 自家的两种内部形态也不收：那是展示地址，不是第三方头像来源。
	if isAegisAvatarLink(trimmed) || isEphemeralStorageLink(trimmed) {
		return ""
	}
	return trimmed
}

// avatarVersionOf 由内容标识派生短版本串。
//
// 取内容摘要而不是时间戳：同一张图重复上传应该得到同一个版本，
// 否则每次"重新上传同一张头像"都会让全网缓存失效一次。
func avatarVersionOf(seed string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("%x", sum[:6])
}
