package httptransport

import (
	"net/http"

	admindomain "aegis/internal/domain/admin"
	systemdomain "aegis/internal/domain/system"
	auditmiddleware "aegis/internal/middleware"
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
	// 更新前读取原配置，用于审计日志 diff（重点关注防火墙段）
	beforeSettings, _ := h.system.GetSettings(c.Request.Context())
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
			Dynamic:            req.AdminCaptcha.Dynamic.ToDomain(),
		},
		LDAP:     mapLDAPPatch(req.LDAP),
		OIDC:     mapOIDCPatch(req.OIDC),
		SAML:     mapSAMLPatch(req.SAML),
		Branding: mapBrandingPatch(req.Branding),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	// 将修改前后的平台设置作为 diff 记录进审计日志（防火墙、安全、验证码、品牌、LDAP/OIDC/SAML 均在其中）
	if beforeSettings != nil && item != nil {
		auditmiddleware.SetAuditDiff(c, beforeSettings, item)
	}
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "更新成功", item)
	h.recordAudit(c, "settings.update", "settings", "", "修改平台设置（含防火墙/安全/品牌/SSO）")
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

func (h *Handler) AdminSAMLTest(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可测试 SAML 连接")
		return
	}
	var req AdminSAMLTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if h.samlSvc == nil {
		response.Error(c, http.StatusBadRequest, 40098, "SAML 服务未初始化")
		return
	}
	result := h.samlSvc.TestMetadata(c.Request.Context(), systemdomain.SAMLTestRequest{
		IDPMetadataURL: req.IDPMetadataURL,
		IDPMetadataXML: req.IDPMetadataXML,
	})
	response.Success(c, 200, "SAML metadata 测试完成", result)
	h.recordAudit(c, "security.saml_test", "security", "", "测试 SAML metadata")
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
		SearchTimeoutSeconds:     req.SearchTimeoutSeconds, FallbackToLocal: req.FallbackToLocal,
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

func mapSAMLPatch(req AdminSAMLSettingsUpdateRequest) systemdomain.SAMLSettingsPatch {
	patch := systemdomain.SAMLSettingsPatch{
		Enabled:             req.Enabled,
		IDPMetadataURL:      req.IDPMetadataURL,
		IDPMetadataXML:      req.IDPMetadataXML,
		EntityID:            req.EntityID,
		MetadataURL:         req.MetadataURL,
		ACSURL:              req.ACSURL,
		SPCertificate:       req.SPCertificate,
		SPPrivateKey:        req.SPPrivateKey,
		NameIDFormat:        req.NameIDFormat,
		SignAuthnRequests:   req.SignAuthnRequests,
		ForceAuthn:          req.ForceAuthn,
		AllowIDPInitiated:   req.AllowIDPInitiated,
		AllowedDomains:      req.AllowedDomains,
		AdminGroupAttribute: req.AdminGroupAttribute,
		AdminGroupValue:     req.AdminGroupValue,
		FallbackToLocal:     req.FallbackToLocal,
		FrontendCallbackURL: req.FrontendCallbackURL,
	}
	if req.AttrMapping != nil {
		patch.AttrMapping = &systemdomain.SAMLAttributeMappingPatch{
			Account:     req.AttrMapping.Account,
			DisplayName: req.AttrMapping.DisplayName,
			Email:       req.AttrMapping.Email,
			Phone:       req.AttrMapping.Phone,
			Groups:      req.AttrMapping.Groups,
		}
	}
	return patch
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
