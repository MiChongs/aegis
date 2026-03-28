package httptransport

import (
	"net/http"

	admindomain "aegis/internal/domain/admin"
	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminGetSystemSettings(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可调整系统设置")
		return
	}
	item, err := h.system.GetSettings(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminUpdateSystemSettings(c *gin.Context) {
	_, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可调整系统设置")
		return
	}
	var req AdminSystemSettingsUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, _ := adminActor(c)
	var actorID *int64
	if adminID > 0 {
		actorID = &adminID
	}
	item, err := h.system.UpdateSettings(c.Request.Context(), actorID, systemdomain.SettingsUpdate{
		Firewall: systemdomain.FirewallSettingsPatch{
			Enabled:           req.Firewall.Enabled,
			GlobalRate:        req.Firewall.GlobalRate,
			AuthRate:          req.Firewall.AuthRate,
			AdminRate:         req.Firewall.AdminRate,
			CorazaEnabled:     req.Firewall.CorazaEnabled,
			CorazaParanoia:    req.Firewall.CorazaParanoia,
			RequestBodyLimit:  req.Firewall.RequestBodyLimit,
			RequestBodyMemory: req.Firewall.RequestBodyMemory,
			AllowedCIDRs:      req.Firewall.AllowedCIDRs,
			BlockedCIDRs:      req.Firewall.BlockedCIDRs,
			BlockedUserAgents: req.Firewall.BlockedUserAgents,
			BlockedPathPrefix: req.Firewall.BlockedPathPrefix,
			MaxPathLength:     req.Firewall.MaxPathLength,
			MaxQueryLength:    req.Firewall.MaxQueryLength,
		},
		Security: systemdomain.SecuritySettingsPatch{
			ChallengeTTLSeconds: req.Security.ChallengeTTLSeconds,
			Modules: systemdomain.SecurityModuleSettingsPatch{
				TOTPEnabled:          req.Security.Modules.TOTPEnabled,
				RecoveryCodesEnabled: req.Security.Modules.RecoveryCodesEnabled,
				PasskeyEnabled:       req.Security.Modules.PasskeyEnabled,
			},
			TOTP: systemdomain.SecurityTOTPSettingsPatch{
				Enabled:              req.Security.TOTP.Enabled,
				Issuer:               req.Security.TOTP.Issuer,
				EnrollmentTTLSeconds: req.Security.TOTP.EnrollmentTTLSeconds,
				Skew:                 req.Security.TOTP.Skew,
				Digits:               req.Security.TOTP.Digits,
			},
			RecoveryCodes: systemdomain.SecurityRecoveryCodeSettingsPatch{
				Enabled: req.Security.RecoveryCodes.Enabled,
				Count:   req.Security.RecoveryCodes.Count,
				Length:  req.Security.RecoveryCodes.Length,
			},
			Passkey: systemdomain.SecurityPasskeySettingsPatch{
				Enabled:             req.Security.Passkey.Enabled,
				RPDisplayName:       req.Security.Passkey.RPDisplayName,
				RPID:                req.Security.Passkey.RPID,
				RPOrigins:           req.Security.Passkey.RPOrigins,
				RPTopOrigins:        req.Security.Passkey.RPTopOrigins,
				ChallengeTTLSeconds: req.Security.Passkey.ChallengeTTLSeconds,
				UserVerification:    req.Security.Passkey.UserVerification,
			},
		},
		AdminCaptcha: systemdomain.AdminCaptchaSettingsPatch{
			Enabled:            req.AdminCaptcha.Enabled,
			Type:               req.AdminCaptcha.Type,
			RequireForLogin:    req.AdminCaptcha.RequireForLogin,
			RequireForRegister: req.AdminCaptcha.RequireForRegister,
			AudioLang:          req.AdminCaptcha.AudioLang,
		},
		LDAP:     mapLDAPPatch(req.LDAP),
		OIDC:     mapOIDCPatch(req.OIDC),
		Branding: mapBrandingPatch(req.Branding),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
	h.recordAudit(c, "settings.update", "settings", "", "修改平台设置")
}

func (h *Handler) AdminLDAPTest(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可测试 LDAP 连接")
		return
	}
	var req AdminLDAPTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result := h.ldapSvc.TestConnection(c.Request.Context(), systemdomain.LDAPTestRequest{
		Server: req.Server, Port: req.Port,
		UseTLS: req.UseTLS, UseStartTLS: req.UseStartTLS, SkipTLSVerify: req.SkipTLSVerify,
		BindDN: req.BindDN, BindPassword: req.BindPassword,
		BaseDN: req.BaseDN, UserFilter: req.UserFilter, TestAccount: req.TestAccount,
		ConnectionTimeoutSeconds: req.ConnectionTimeoutSeconds,
	})
	response.Success(c, 200, "LDAP 连接测试完成", result)
	h.recordAudit(c, "security.ldap_test", "security", "", "测试 LDAP 连接")
}

func mapLDAPPatch(req AdminLDAPSettingsUpdateRequest) systemdomain.LDAPSettingsPatch {
	patch := systemdomain.LDAPSettingsPatch{
		Enabled: req.Enabled, Server: req.Server, Port: req.Port,
		UseTLS: req.UseTLS, UseStartTLS: req.UseStartTLS, SkipTLSVerify: req.SkipTLSVerify,
		BindDN: req.BindDN, BindPassword: req.BindPassword,
		BaseDN: req.BaseDN, UserFilter: req.UserFilter, UserAttribute: req.UserAttribute,
		GroupBaseDN: req.GroupBaseDN, GroupFilter: req.GroupFilter,
		GroupAttribute: req.GroupAttribute, AdminGroupDN: req.AdminGroupDN,
		ConnectionTimeoutSeconds: req.ConnectionTimeoutSeconds,
		SearchTimeoutSeconds: req.SearchTimeoutSeconds, FallbackToLocal: req.FallbackToLocal,
	}
	if req.AttrMapping != nil {
		patch.AttrMapping = &systemdomain.LDAPAttributeMappingPatch{
			Account: req.AttrMapping.Account, DisplayName: req.AttrMapping.DisplayName,
			Email: req.AttrMapping.Email, Phone: req.AttrMapping.Phone,
		}
	}
	return patch
}

func mapBrandingPatch(req AdminBrandingSettingsUpdateRequest) systemdomain.BrandingSettingsPatch {
	return systemdomain.BrandingSettingsPatch{
		PlatformName: req.PlatformName, ConsoleName: req.ConsoleName,
		LogoURL: req.LogoURL, LogoDarkURL: req.LogoDarkURL, FaviconURL: req.FaviconURL,
		PrimaryColor: req.PrimaryColor, PrimaryColorDark: req.PrimaryColorDark, AccentColor: req.AccentColor,
		LoginBgURL: req.LoginBgURL, LoginBgColor: req.LoginBgColor,
		FooterText: req.FooterText, CustomCSS: req.CustomCSS,
	}
}

// AdminGetPublicBranding 公开品牌信息（无需登录）
func (h *Handler) AdminGetPublicBranding(c *gin.Context) {
	settings, err := h.system.GetSettings(c.Request.Context())
	if err != nil {
		response.Success(c, 200, "ok", systemdomain.BrandingConfig{PlatformName: "Aegis", ConsoleName: "控制台"})
		return
	}
	response.Success(c, 200, "ok", settings.Branding.BrandingConfig)
}

func requireSuperAdminSession(c *gin.Context) (*admindomain.AccessContext, bool) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		return nil, false
	}
	if !session.IsSuperAdmin {
		return nil, false
	}
	return session, true
}
