package service

import (
	captchadomain "aegis/internal/domain/captcha"
	apperrors "aegis/pkg/errors"
	"net/http"
	"strings"
)

func BuildSMSProviderConfig(appID int64, purpose captchadomain.Purpose, smsCfg captchadomain.CaptchaSMSConfig) (*captchadomain.SMSProviderConfig, error) {
	provider := captchadomain.SMSProviderType(strings.ToLower(strings.TrimSpace(smsCfg.Provider)))
	if provider == "" {
		return nil, apperrors.New(40461, http.StatusNotFound, "未配置短信服务商")
	}

	template := selectSMSTemplateForPurpose(smsCfg.Templates, purpose)
	signName := strings.TrimSpace(smsCfg.SignName)
	templateID := strings.TrimSpace(smsCfg.TemplateID)
	codeParamKey := strings.TrimSpace(smsCfg.CodeParamKey)

	if template != nil {
		if value := strings.TrimSpace(template.SignName); value != "" {
			signName = value
		}
		if value := strings.TrimSpace(template.TemplateID); value != "" {
			templateID = value
		}
		if value := strings.TrimSpace(template.CodeParamKey); value != "" {
			codeParamKey = value
		}
	}

	if signName == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "短信签名不能为空")
	}
	if templateID == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "短信模板不能为空")
	}
	if codeParamKey == "" {
		codeParamKey = "code"
	}
	if provider == captchadomain.SMSProviderTencent && strings.TrimSpace(smsCfg.SDKAppID) == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "腾讯云短信配置缺少 SDKAppID")
	}

	return &captchadomain.SMSProviderConfig{
		AppID:        appID,
		Provider:     provider,
		Enabled:      true,
		IsDefault:    true,
		AccessKey:    strings.TrimSpace(smsCfg.AccessKey),
		SecretKey:    strings.TrimSpace(smsCfg.SecretKey),
		Region:       strings.TrimSpace(smsCfg.Region),
		SignName:     signName,
		TemplateID:   templateID,
		CodeParamKey: codeParamKey,
		SDKAppID:     strings.TrimSpace(smsCfg.SDKAppID),
	}, nil
}

func selectSMSTemplateForPurpose(templates []captchadomain.CaptchaSMSTemplateConfig, purpose captchadomain.Purpose) *captchadomain.CaptchaSMSTemplateConfig {
	target := normalizeSMSPurpose(string(purpose))
	if target == "" {
		return nil
	}
	for i := range templates {
		item := &templates[i]
		if !item.Enabled {
			continue
		}
		if normalizeSMSPurpose(item.Purpose) == target {
			return item
		}
	}
	return nil
}

func normalizeSMSPurpose(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	return value
}
