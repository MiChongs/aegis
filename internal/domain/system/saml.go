package system

import (
	"strings"
	"time"
)

const DefaultSAMLNameIDFormat = "urn:oasis:names:tc:SAML:1.1:nameid-format:unspecified"

// SAMLConfig 管理侧 SAML 2.0 配置。
type SAMLConfig struct {
	Enabled             bool                 `json:"enabled"`
	IDPMetadataURL      string               `json:"idpMetadataURL"`
	IDPMetadataXML      string               `json:"idpMetadataXML"`
	EntityID            string               `json:"entityID"`
	MetadataURL         string               `json:"metadataURL"`
	ACSURL              string               `json:"acsURL"`
	SPCertificate       string               `json:"spCertificate"`
	SPPrivateKey        string               `json:"spPrivateKey"` // AES-GCM 加密存储
	NameIDFormat        string               `json:"nameIDFormat"`
	SignAuthnRequests   bool                 `json:"signAuthnRequests"`
	ForceAuthn          bool                 `json:"forceAuthn"`
	AllowIDPInitiated   bool                 `json:"allowIdpInitiated"`
	AllowedDomains      []string             `json:"allowedDomains,omitempty"`
	AdminGroupAttribute string               `json:"adminGroupAttribute,omitempty"`
	AdminGroupValue     string               `json:"adminGroupValue,omitempty"`
	AttrMapping         SAMLAttributeMapping `json:"attrMapping"`
	FallbackToLocal     bool                 `json:"fallbackToLocal"`
	FrontendCallbackURL string               `json:"frontendCallbackURL"`
}

type SAMLAttributeMapping struct {
	Account     string `json:"account"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	Groups      string `json:"groups"`
}

type SAMLSettingsView struct {
	Enabled             bool                 `json:"enabled"`
	IDPMetadataURL      string               `json:"idpMetadataURL"`
	HasIDPMetadataXML   bool                 `json:"hasIdpMetadataXML"`
	EntityID            string               `json:"entityID"`
	MetadataURL         string               `json:"metadataURL"`
	ACSURL              string               `json:"acsURL"`
	SPCertificate       string               `json:"spCertificate"`
	HasSPPrivateKey     bool                 `json:"hasSpPrivateKey"`
	NameIDFormat        string               `json:"nameIDFormat"`
	SignAuthnRequests   bool                 `json:"signAuthnRequests"`
	ForceAuthn          bool                 `json:"forceAuthn"`
	AllowIDPInitiated   bool                 `json:"allowIdpInitiated"`
	AllowedDomains      []string             `json:"allowedDomains,omitempty"`
	AdminGroupAttribute string               `json:"adminGroupAttribute,omitempty"`
	AdminGroupValue     string               `json:"adminGroupValue,omitempty"`
	AttrMapping         SAMLAttributeMapping `json:"attrMapping"`
	FallbackToLocal     bool                 `json:"fallbackToLocal"`
	FrontendCallbackURL string               `json:"frontendCallbackURL"`
	Source              string               `json:"source"`
	UpdatedBy           *int64               `json:"updatedBy,omitempty"`
	UpdatedAt           *time.Time           `json:"updatedAt,omitempty"`
}

type SAMLSettingsPatch struct {
	Enabled             *bool                      `json:"enabled,omitempty"`
	IDPMetadataURL      *string                    `json:"idpMetadataURL,omitempty"`
	IDPMetadataXML      *string                    `json:"idpMetadataXML,omitempty"`
	EntityID            *string                    `json:"entityID,omitempty"`
	MetadataURL         *string                    `json:"metadataURL,omitempty"`
	ACSURL              *string                    `json:"acsURL,omitempty"`
	SPCertificate       *string                    `json:"spCertificate,omitempty"`
	SPPrivateKey        *string                    `json:"spPrivateKey,omitempty"`
	NameIDFormat        *string                    `json:"nameIDFormat,omitempty"`
	SignAuthnRequests   *bool                      `json:"signAuthnRequests,omitempty"`
	ForceAuthn          *bool                      `json:"forceAuthn,omitempty"`
	AllowIDPInitiated   *bool                      `json:"allowIdpInitiated,omitempty"`
	AllowedDomains      *[]string                  `json:"allowedDomains,omitempty"`
	AdminGroupAttribute *string                    `json:"adminGroupAttribute,omitempty"`
	AdminGroupValue     *string                    `json:"adminGroupValue,omitempty"`
	AttrMapping         *SAMLAttributeMappingPatch `json:"attrMapping,omitempty"`
	FallbackToLocal     *bool                      `json:"fallbackToLocal,omitempty"`
	FrontendCallbackURL *string                    `json:"frontendCallbackURL,omitempty"`
}

type SAMLAttributeMappingPatch struct {
	Account     *string `json:"account,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	Email       *string `json:"email,omitempty"`
	Phone       *string `json:"phone,omitempty"`
	Groups      *string `json:"groups,omitempty"`
}

type SAMLTestRequest struct {
	IDPMetadataURL string `json:"idpMetadataURL"`
	IDPMetadataXML string `json:"idpMetadataXML"`
}

type SAMLTestResult struct {
	MetadataOK       bool     `json:"metadataOK"`
	EntityID         string   `json:"entityID,omitempty"`
	SSORedirectURL   string   `json:"ssoRedirectURL,omitempty"`
	SSOPostURL       string   `json:"ssoPostURL,omitempty"`
	SingleLogoutURL  string   `json:"singleLogoutURL,omitempty"`
	CertificateCount int      `json:"certificateCount"`
	SupportedNameID  []string `json:"supportedNameIdFormats,omitempty"`
	Error            string   `json:"error,omitempty"`
	LatencyMs        int64    `json:"latencyMs"`
}

type SAMLUser struct {
	NameID      string            `json:"nameId"`
	Account     string            `json:"account"`
	DisplayName string            `json:"displayName"`
	Email       string            `json:"email"`
	Phone       string            `json:"phone"`
	Groups      []string          `json:"groups,omitempty"`
	Attributes  map[string]string `json:"attributes,omitempty"`
}

func NormalizeSAMLConfig(cfg SAMLConfig) SAMLConfig {
	cfg.IDPMetadataURL = strings.TrimSpace(cfg.IDPMetadataURL)
	cfg.IDPMetadataXML = strings.TrimSpace(cfg.IDPMetadataXML)
	cfg.EntityID = strings.TrimSpace(cfg.EntityID)
	cfg.MetadataURL = strings.TrimSpace(cfg.MetadataURL)
	cfg.ACSURL = strings.TrimSpace(cfg.ACSURL)
	cfg.SPCertificate = strings.TrimSpace(cfg.SPCertificate)
	cfg.SPPrivateKey = strings.TrimSpace(cfg.SPPrivateKey)
	cfg.NameIDFormat = strings.TrimSpace(cfg.NameIDFormat)
	cfg.AdminGroupAttribute = strings.TrimSpace(cfg.AdminGroupAttribute)
	cfg.AdminGroupValue = strings.TrimSpace(cfg.AdminGroupValue)
	cfg.FrontendCallbackURL = strings.TrimSpace(cfg.FrontendCallbackURL)
	if cfg.MetadataURL == "" && strings.HasSuffix(cfg.ACSURL, "/callback") {
		cfg.MetadataURL = strings.TrimSuffix(cfg.ACSURL, "/callback") + "/metadata"
	}
	if cfg.ACSURL == "" && strings.HasSuffix(cfg.MetadataURL, "/metadata") {
		cfg.ACSURL = strings.TrimSuffix(cfg.MetadataURL, "/metadata") + "/callback"
	}
	if cfg.EntityID == "" {
		cfg.EntityID = cfg.MetadataURL
	}
	if cfg.NameIDFormat == "" {
		cfg.NameIDFormat = DefaultSAMLNameIDFormat
	}
	if cfg.AttrMapping.Account == "" {
		cfg.AttrMapping.Account = "uid"
	}
	if cfg.AttrMapping.DisplayName == "" {
		cfg.AttrMapping.DisplayName = "displayName"
	}
	if cfg.AttrMapping.Email == "" {
		cfg.AttrMapping.Email = "email"
	}
	if cfg.AttrMapping.Phone == "" {
		cfg.AttrMapping.Phone = "phone"
	}
	if cfg.AttrMapping.Groups == "" {
		cfg.AttrMapping.Groups = "groups"
	}
	return cfg
}
