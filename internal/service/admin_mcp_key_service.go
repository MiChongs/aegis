package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// MCP 密钥：管理员给 MCP 服务端（AI 助手）签发的长期凭证，见 docs/mcp.md。
//
// 判定是两层交集，且**两层都在每次请求时现查**：
//   - 管理员本人此刻的权限 —— 密钥不缓存角色，撤了角色、停了账号，下一次调用就收窄或失效；
//   - 密钥自己的范围 —— 权限点（支持 `ticket:*`）、应用、只读、来源 IP、每分钟次数。
//
// 交集的落点是 Authorize：路由级判定与 service 层的细粒度判定（工单、组织）
// 都经过它，因此密钥范围不会只在中间件那一层生效。

const (
	mcpKeyMaxPerAdmin     = 20
	mcpKeyMaxPermissions  = 400
	mcpKeyMaxApps         = 200
	mcpKeyMaxAllowIPs     = 50
	mcpKeyDefaultRate     = 120
	mcpKeyMaxRate         = 6000
	mcpKeyTouchInterval   = 30 * time.Second
	mcpKeySecretBytes     = 32
	mcpKeyDisplayedPrefix = 16
)

// mcpKeyUsage 使用记录的节流器：每把密钥最多每 30 秒落一次库，次数累计后一并写入。
// 每个请求都 UPDATE 一行会让一个高频轮询的助手把那一行变成热点。
type mcpKeyUsage struct {
	mu      sync.Mutex
	pending map[int64]*mcpKeyUsageState
}

type mcpKeyUsageState struct {
	uses    int64
	ip      string
	flushed time.Time
}

// ── 签发与管理（只接受控制台会话，见 ensureMCPKeyManager） ──

// ListMCPKeys 列出自己的密钥；all 为真且调用者是超管时列全部。
func (s *AdminService) ListMCPKeys(ctx context.Context, access *admindomain.AccessContext, all bool) ([]admindomain.MCPKey, error) {
	if err := ensureMCPKeyManager(access); err != nil {
		return nil, err
	}
	var owner *int64
	if !(all && access.IsSuperAdmin) {
		id := access.AdminID
		owner = &id
	}
	items, err := s.pg.ListAdminMCPKeys(ctx, owner)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range items {
		items[i].Status = items[i].EffectiveStatus(now)
	}
	return items, nil
}

// GetMCPKey 读一把密钥（本人或超管）。
func (s *AdminService) GetMCPKey(ctx context.Context, access *admindomain.AccessContext, id int64) (*admindomain.MCPKey, error) {
	if err := ensureMCPKeyManager(access); err != nil {
		return nil, err
	}
	item, err := s.ownedMCPKey(ctx, access, id)
	if err != nil {
		return nil, err
	}
	item.Status = item.EffectiveStatus(time.Now())
	return item, nil
}

// CreateMCPKey 签发一把新密钥，明文只在返回值里出现这一次。
func (s *AdminService) CreateMCPKey(ctx context.Context, access *admindomain.AccessContext, input admindomain.MCPKeyInput) (*admindomain.CreatedMCPKey, error) {
	if err := ensureMCPKeyManager(access); err != nil {
		return nil, err
	}
	normalized, err := s.normalizeMCPKeyInput(ctx, access, input)
	if err != nil {
		return nil, err
	}
	count, err := s.pg.CountActiveAdminMCPKeys(ctx, access.AdminID)
	if err != nil {
		return nil, err
	}
	if count >= mcpKeyMaxPerAdmin {
		return nil, apperrors.New(40169, http.StatusBadRequest,
			fmt.Sprintf("每个管理员最多持有 %d 把未撤销的 MCP 密钥，请先撤销不再使用的密钥", mcpKeyMaxPerAdmin))
	}
	secret, prefix, hash, err := newMCPKeySecret()
	if err != nil {
		return nil, err
	}
	item, err := s.pg.CreateAdminMCPKey(ctx, access.AdminID, normalized, prefix, hash)
	if err != nil {
		return nil, err
	}
	item.Status = item.EffectiveStatus(time.Now())
	return &admindomain.CreatedMCPKey{MCPKey: *item, Secret: secret}, nil
}

// UpdateMCPKey 修改名称、范围与有效期。明文不变，正在使用它的助手不必重配。
func (s *AdminService) UpdateMCPKey(ctx context.Context, access *admindomain.AccessContext, id int64, input admindomain.MCPKeyInput) (*admindomain.MCPKey, error) {
	if err := ensureMCPKeyManager(access); err != nil {
		return nil, err
	}
	current, err := s.ownedMCPKey(ctx, access, id)
	if err != nil {
		return nil, err
	}
	if current.Status == admindomain.MCPKeyStatusRevoked {
		return nil, mcpKeyNotFound()
	}
	// 超管改别人的密钥时，范围按密钥主人校验：应用清单要落在主人能管的范围里。
	owner := access
	if current.AdminID != access.AdminID {
		owner, err = s.mcpKeyOwnerAccess(ctx, current.AdminID)
		if err != nil {
			return nil, err
		}
	}
	normalized, err := s.normalizeMCPKeyInput(ctx, owner, input)
	if err != nil {
		return nil, err
	}
	if err := s.pg.UpdateAdminMCPKey(ctx, id, normalized); err != nil {
		return nil, err
	}
	return s.GetMCPKey(ctx, access, id)
}

// SetMCPKeyEnabled 停用 / 启用。停用可逆，撤销不可逆。
func (s *AdminService) SetMCPKeyEnabled(ctx context.Context, access *admindomain.AccessContext, id int64, enabled bool) (*admindomain.MCPKey, error) {
	if err := ensureMCPKeyManager(access); err != nil {
		return nil, err
	}
	if _, err := s.ownedMCPKey(ctx, access, id); err != nil {
		return nil, err
	}
	status := admindomain.MCPKeyStatusDisabled
	if enabled {
		status = admindomain.MCPKeyStatusActive
	}
	affected, err := s.pg.SetAdminMCPKeyStatus(ctx, id, status)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, mcpKeyNotFound()
	}
	return s.GetMCPKey(ctx, access, id)
}

// RotateMCPKey 换一个新明文，旧明文立刻失效；范围与使用记录保留。
func (s *AdminService) RotateMCPKey(ctx context.Context, access *admindomain.AccessContext, id int64) (*admindomain.CreatedMCPKey, error) {
	if err := ensureMCPKeyManager(access); err != nil {
		return nil, err
	}
	if _, err := s.ownedMCPKey(ctx, access, id); err != nil {
		return nil, err
	}
	secret, prefix, hash, err := newMCPKeySecret()
	if err != nil {
		return nil, err
	}
	affected, err := s.pg.RotateAdminMCPKey(ctx, id, prefix, hash)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, mcpKeyNotFound()
	}
	item, err := s.GetMCPKey(ctx, access, id)
	if err != nil {
		return nil, err
	}
	return &admindomain.CreatedMCPKey{MCPKey: *item, Secret: secret}, nil
}

// RevokeMCPKey 永久撤销（本人或超管）。
func (s *AdminService) RevokeMCPKey(ctx context.Context, access *admindomain.AccessContext, id int64) error {
	if err := ensureMCPKeyManager(access); err != nil {
		return err
	}
	if _, err := s.ownedMCPKey(ctx, access, id); err != nil {
		return err
	}
	affected, err := s.pg.RevokeAdminMCPKey(ctx, id, access.AdminID)
	if err != nil {
		return err
	}
	if affected == 0 {
		return mcpKeyNotFound()
	}
	return nil
}

// MCPPermissionCatalog 签发表单用的权限目录：分组即权限目录本身，另附「全部」这一档的说明。
func (s *AdminService) MCPPermissionCatalog(ctx context.Context, access *admindomain.AccessContext) map[string]any {
	return map[string]any{
		"groups":      authz.PermissionCatalog(),
		"effective":   s.EffectivePermissions(ctx, access),
		"maxPerAdmin": mcpKeyMaxPerAdmin,
		"defaultRate": mcpKeyDefaultRate,
		"keyPrefix":   admindomain.MCPKeyPrefix,
	}
}

// ── 认证 ──

// ValidateMCPKey 校验一把明文密钥并组装本次请求的访问上下文。
//
// 返回的 AccessContext 与会话登录的同构，差别只在 MCPKey 非空：
// 下游的 Authorize、审计、处理函数都不需要知道请求来自 MCP。
func (s *AdminService) ValidateMCPKey(ctx context.Context, secret, clientIP string) (*admindomain.AccessContext, error) {
	secret = strings.TrimSpace(secret)
	if !strings.HasPrefix(secret, admindomain.MCPKeyPrefix) || len(secret) < len(admindomain.MCPKeyPrefix)+32 {
		return nil, apperrors.New(40160, http.StatusUnauthorized, "MCP 密钥无效")
	}
	digest := sha256.Sum256([]byte(secret))
	key, err := s.pg.GetAdminMCPKeyByHash(ctx, digest[:])
	if err != nil {
		return nil, err
	}
	if key == nil || subtle.ConstantTimeCompare(key.KeyHash, digest[:]) != 1 {
		return nil, apperrors.New(40160, http.StatusUnauthorized, "MCP 密钥无效")
	}
	now := time.Now()
	switch key.Status {
	case admindomain.MCPKeyStatusRevoked:
		return nil, apperrors.New(40162, http.StatusUnauthorized, "MCP 密钥已撤销")
	case admindomain.MCPKeyStatusDisabled:
		return nil, apperrors.New(40162, http.StatusUnauthorized, "MCP 密钥已停用，请在控制台「MCP 密钥」中重新启用")
	}
	if key.Expired(now) {
		return nil, apperrors.New(40161, http.StatusUnauthorized, "MCP 密钥已过期，请在控制台延长有效期或签发新密钥")
	}
	if !mcpIPAllowed(key.AllowIPs, clientIP) {
		return nil, apperrors.New(40163, http.StatusForbidden,
			fmt.Sprintf("来源 IP %s 不在该 MCP 密钥的允许列表中", firstNonEmptyString(clientIP, "未知")))
	}
	if key.RateLimitPerMinute > 0 && s.sessions != nil {
		count, rateErr := s.sessions.HitAdminMCPKeyRate(ctx, key.ID, now)
		if rateErr != nil {
			// 计数器不可用时放行：限流是保护手段，不是认证的一部分，
			// 不能让一次 Redis 抖动把所有 MCP 调用一并拒掉。
			s.log.Warn("MCP 密钥限流计数失败，本次放行", zap.Int64("keyId", key.ID), zap.Error(rateErr))
		} else if count > int64(key.RateLimitPerMinute) {
			return nil, apperrors.New(40168, http.StatusTooManyRequests,
				fmt.Sprintf("MCP 密钥调用过于频繁：每分钟最多 %d 次", key.RateLimitPerMinute))
		}
	}
	profile, err := s.pg.GetAdminAccessByID(ctx, key.AdminID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, apperrors.New(40160, http.StatusUnauthorized, "MCP 密钥无效")
	}
	if profile.Account.Status != "active" {
		return nil, apperrors.New(40310, http.StatusForbidden, "管理员账户不可用")
	}
	s.recordMCPKeyUse(key.ID, clientIP, now)

	var expiresAt time.Time
	if key.ExpiresAt != nil {
		expiresAt = *key.ExpiresAt
	}
	access := s.accessFromProfile(ctx, profile)
	// 审计里的会话列写成 mcp:<前缀>，一眼能看出这条操作是哪把密钥做的
	access.TokenID = "mcp:" + key.KeyPrefix
	access.IssuedAt = key.CreatedAt
	access.ExpiresAt = expiresAt
	access.MCPKey = &admindomain.MCPKeyScope{
		KeyID:       key.ID,
		Name:        key.Name,
		KeyPrefix:   key.KeyPrefix,
		Permissions: append([]string(nil), key.Permissions...),
		AppIDs:      append([]int64(nil), key.AppIDs...),
		ReadOnly:    key.ReadOnly,
	}
	return access, nil
}

// authorizeMCPScope Authorize 的第一道：权限点与应用是否落在密钥范围里。
func authorizeMCPScope(access *admindomain.AccessContext, permission string, appID *int64) error {
	key := access.MCPKey
	if key == nil {
		return nil
	}
	if appID != nil && *appID > 0 && !key.AllowsApp(*appID) {
		return apperrors.New(40165, http.StatusForbidden,
			fmt.Sprintf("MCP 密钥「%s」未授权应用 #%d", key.Name, *appID))
	}
	if permission != "" && !key.AllowsPermission(permission) {
		return apperrors.New(40164, http.StatusForbidden,
			fmt.Sprintf("MCP 密钥「%s」的权限范围不包含「%s」（%s），请在控制台「MCP 密钥」中调整范围",
				key.Name, permissionDisplayName(permission), permission))
	}
	return nil
}

// restrictSnapshotToMCPKey 生效权限快照按密钥范围收窄，让 whoami 与 /auth/me 说的就是实际能做的。
func restrictSnapshotToMCPKey(snapshot admindomain.PermissionSnapshot, key *admindomain.MCPKeyScope) admindomain.PermissionSnapshot {
	if key == nil || key.AllowsAll() {
		return snapshot
	}
	filter := func(items []string) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			if key.AllowsPermission(item) {
				out = append(out, item)
			}
		}
		return out
	}
	restricted := admindomain.PermissionSnapshot{Global: filter(snapshot.Global), Apps: map[string][]string{}}
	if !key.RestrictsApps() {
		for appID, items := range snapshot.Apps {
			restricted.Apps[appID] = filter(items)
		}
		return restricted
	}
	for _, appID := range key.AppIDs {
		id := strconv.FormatInt(appID, 10)
		items, ok := snapshot.Apps[id]
		if !ok {
			// 全局授权在该应用下同样生效：快照里没有单独一项时按全局集算
			items = snapshot.Global
		}
		restricted.Apps[id] = filter(items)
	}
	return restricted
}

// ── 内部 ──

// ensureMCPKeyManager 密钥只能由控制台会话管理：拿一把密钥去签发新密钥、
// 放宽自己的范围，等于让被限权的一方自行解除限制。
func ensureMCPKeyManager(access *admindomain.AccessContext) error {
	if access == nil {
		return apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	if access.MCPKey != nil {
		return apperrors.New(40167, http.StatusForbidden, "MCP 密钥只能在控制台登录后管理，不能用 MCP 密钥自身调用")
	}
	return nil
}

func (s *AdminService) ownedMCPKey(ctx context.Context, access *admindomain.AccessContext, id int64) (*admindomain.MCPKey, error) {
	if id <= 0 {
		return nil, mcpKeyNotFound()
	}
	item, err := s.pg.GetAdminMCPKey(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil || (item.AdminID != access.AdminID && !access.IsSuperAdmin) {
		return nil, mcpKeyNotFound()
	}
	return item, nil
}

func (s *AdminService) mcpKeyOwnerAccess(ctx context.Context, adminID int64) (*admindomain.AccessContext, error) {
	profile, err := s.pg.GetAdminAccessByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, mcpKeyNotFound()
	}
	return s.accessFromProfile(ctx, profile), nil
}

func mcpKeyNotFound() error {
	return apperrors.New(40498, http.StatusNotFound, "MCP 密钥不存在或已撤销")
}

func mcpKeyInvalid(message string) error {
	return apperrors.New(40169, http.StatusBadRequest, message)
}

func (s *AdminService) normalizeMCPKeyInput(ctx context.Context, owner *admindomain.AccessContext, input admindomain.MCPKeyInput) (admindomain.MCPKeyInput, error) {
	out := admindomain.MCPKeyInput{
		Name:               strings.TrimSpace(input.Name),
		Description:        strings.TrimSpace(input.Description),
		ReadOnly:           input.ReadOnly,
		RateLimitPerMinute: input.RateLimitPerMinute,
		ExpiresAt:          input.ExpiresAt,
	}
	if out.Name == "" || len([]rune(out.Name)) > 96 {
		return out, mcpKeyInvalid("密钥名称长度必须在 1 到 96 个字符之间")
	}
	if len([]rune(out.Description)) > 500 {
		return out, mcpKeyInvalid("说明最多 500 个字符")
	}

	permissions, err := normalizeMCPPermissions(input.Permissions)
	if err != nil {
		return out, err
	}
	out.Permissions = permissions

	apps, err := s.normalizeMCPApps(ctx, owner, input.AppIDs)
	if err != nil {
		return out, err
	}
	out.AppIDs = apps

	ips, err := normalizeMCPAllowIPs(input.AllowIPs)
	if err != nil {
		return out, err
	}
	out.AllowIPs = ips

	if out.RateLimitPerMinute < 0 || out.RateLimitPerMinute > mcpKeyMaxRate {
		return out, mcpKeyInvalid(fmt.Sprintf("每分钟调用上限必须在 0 到 %d 之间（0 为不限）", mcpKeyMaxRate))
	}
	if out.ExpiresAt != nil {
		expires := out.ExpiresAt.UTC()
		if !expires.After(time.Now()) {
			return out, mcpKeyInvalid("过期时间必须晚于当前时间")
		}
		out.ExpiresAt = &expires
	}
	return out, nil
}

func normalizeMCPPermissions(items []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" || seen[item] {
			continue
		}
		if !validMCPPermissionPattern(item) {
			return nil, mcpKeyInvalid(fmt.Sprintf("权限点 %q 不存在：请从权限目录中选择，或使用 `分组:*` / `*` 通配", item))
		}
		seen[item] = true
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil, mcpKeyInvalid("至少选择一个权限点")
	}
	if len(out) > mcpKeyMaxPermissions {
		return nil, mcpKeyInvalid(fmt.Sprintf("权限点最多 %d 项", mcpKeyMaxPermissions))
	}
	if seen[admindomain.MCPKeyAllPermissions] {
		return []string{admindomain.MCPKeyAllPermissions}, nil
	}
	sort.Strings(out)
	return out, nil
}

// validMCPPermissionPattern 模式必须能匹配到目录里至少一个权限点，
// 否则一个拼错的通配会成为一把「看起来有权限、实际什么都做不了」的密钥。
func validMCPPermissionPattern(pattern string) bool {
	if pattern == admindomain.MCPKeyAllPermissions {
		return true
	}
	if strings.HasSuffix(pattern, ":*") {
		for _, code := range authz.AllPermissionCodes() {
			if admindomain.MCPPermissionMatches(pattern, code) {
				return true
			}
		}
		return false
	}
	// 旧的合并权限点（app:read 之类）仍然合法：权限拆分之前签发的密钥要能原样编辑保存
	return authz.PermissionExists(pattern) || authz.IsLegacyPermission(pattern)
}

func (s *AdminService) normalizeMCPApps(ctx context.Context, owner *admindomain.AccessContext, ids []int64) ([]int64, error) {
	if len(ids) > mcpKeyMaxApps {
		return nil, mcpKeyInvalid(fmt.Sprintf("应用最多 %d 个", mcpKeyMaxApps))
	}
	global := owner.IsSuperAdmin
	allowed := map[int64]bool{}
	for _, assignment := range owner.Assignments {
		if assignment.AppID == nil {
			global = true
			continue
		}
		allowed[*assignment.AppID] = true
	}
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		app, err := s.pg.GetAppByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if app == nil {
			return nil, mcpKeyInvalid(fmt.Sprintf("应用 #%d 不存在", id))
		}
		if !global && !allowed[id] {
			return nil, mcpKeyInvalid(fmt.Sprintf("你没有应用 #%d 的任何角色，不能把它加入密钥范围", id))
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func normalizeMCPAllowIPs(items []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		var canonical string
		if strings.Contains(item, "/") {
			prefix, err := netip.ParsePrefix(item)
			if err != nil {
				return nil, mcpKeyInvalid(fmt.Sprintf("%q 不是有效的 IP 或 CIDR", item))
			}
			canonical = prefix.Masked().String()
		} else {
			addr, err := netip.ParseAddr(item)
			if err != nil {
				return nil, mcpKeyInvalid(fmt.Sprintf("%q 不是有效的 IP 或 CIDR", item))
			}
			canonical = addr.Unmap().String()
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		out = append(out, canonical)
	}
	if len(out) > mcpKeyMaxAllowIPs {
		return nil, mcpKeyInvalid(fmt.Sprintf("允许的来源最多 %d 条", mcpKeyMaxAllowIPs))
	}
	return out, nil
}

func mcpIPAllowed(allow []string, clientIP string) bool {
	if len(allow) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(clientIP))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, item := range allow {
		if strings.Contains(item, "/") {
			if prefix, err := netip.ParsePrefix(item); err == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		if other, err := netip.ParseAddr(item); err == nil && other.Unmap() == addr {
			return true
		}
	}
	return false
}

func newMCPKeySecret() (secret, prefix string, hash []byte, err error) {
	random := make([]byte, mcpKeySecretBytes)
	if _, err = rand.Read(random); err != nil {
		return "", "", nil, err
	}
	secret = admindomain.MCPKeyPrefix + base64.RawURLEncoding.EncodeToString(random)
	digest := sha256.Sum256([]byte(secret))
	return secret, secret[:mcpKeyDisplayedPrefix], digest[:], nil
}

func (s *AdminService) recordMCPKeyUse(keyID int64, ip string, now time.Time) {
	s.mcpUsage.mu.Lock()
	if s.mcpUsage.pending == nil {
		s.mcpUsage.pending = map[int64]*mcpKeyUsageState{}
	}
	state := s.mcpUsage.pending[keyID]
	if state == nil {
		state = &mcpKeyUsageState{}
		s.mcpUsage.pending[keyID] = state
	}
	state.uses++
	state.ip = ip
	if now.Sub(state.flushed) < mcpKeyTouchInterval {
		s.mcpUsage.mu.Unlock()
		return
	}
	uses := state.uses
	state.uses = 0
	state.flushed = now
	s.mcpUsage.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.pg.TouchAdminMCPKey(ctx, keyID, ip, uses, now)
	}()
}
