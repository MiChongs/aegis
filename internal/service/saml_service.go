package service

import (
	"aegis/pkg/egress"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	systemdomain "aegis/internal/domain/system"
	apperrors "aegis/pkg/errors"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	dsig "github.com/russellhaering/goxmldsig"
	"go.uber.org/zap"
)

type SAMLService struct {
	log           *zap.Logger
	mu            sync.RWMutex
	config        systemdomain.SAMLConfig
	encryptionKey []byte
	serviceKey    string
	service       *saml.ServiceProvider
}

func NewSAMLService(log *zap.Logger, masterKey string) *SAMLService {
	if log == nil {
		log = zap.NewNop()
	}
	return &SAMLService{
		log:           log,
		encryptionKey: securityKeyMaterial(masterKey),
	}
}

func (s *SAMLService) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Enabled
}

func (s *SAMLService) CurrentConfig() systemdomain.SAMLConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *SAMLService) Reload(cfg systemdomain.SAMLConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = systemdomain.NormalizeSAMLConfig(cfg)
	s.service = nil
	s.serviceKey = ""
	s.log.Info("SAML 配置已重载",
		zap.Bool("enabled", s.config.Enabled),
		zap.String("metadata_url", s.config.MetadataURL),
		zap.String("acs_url", s.config.ACSURL),
	)
	return nil
}

func (s *SAMLService) EncryptPrivateKey(plaintext string) (string, error) {
	return encryptSecret(s.encryptionKey, plaintext)
}

func (s *SAMLService) DecryptPrivateKey(ciphertext string) (string, error) {
	return decryptSecret(s.encryptionKey, ciphertext)
}

func (s *SAMLService) EnsureCredentials(cfg systemdomain.SAMLConfig) (systemdomain.SAMLConfig, error) {
	cfg = systemdomain.NormalizeSAMLConfig(cfg)
	if cfg.SPCertificate != "" && cfg.SPPrivateKey != "" {
		return cfg, nil
	}
	certPEM, keyPEM, err := generateSelfSignedSAMLCredentials(cfg.EntityID)
	if err != nil {
		return cfg, err
	}
	encryptedKey, err := s.EncryptPrivateKey(keyPEM)
	if err != nil {
		return cfg, err
	}
	cfg.SPCertificate = certPEM
	cfg.SPPrivateKey = encryptedKey
	return cfg, nil
}

func (s *SAMLService) BuildAuthRedirect(ctx context.Context, relayState string) (string, string, error) {
	sp, err := s.ensureProvider(ctx)
	if err != nil {
		return "", "", err
	}
	idpURL := strings.TrimSpace(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding))
	if idpURL == "" {
		return "", "", apperrors.New(40198, http.StatusBadRequest, "SAML IdP 未提供 HTTP-Redirect 单点登录地址")
	}
	req, err := sp.MakeAuthenticationRequest(idpURL, saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return "", "", fmt.Errorf("build saml authn request: %w", err)
	}
	redirectURL, err := req.Redirect(relayState, sp)
	if err != nil {
		return "", "", fmt.Errorf("build saml redirect url: %w", err)
	}
	return redirectURL.String(), req.ID, nil
}

func (s *SAMLService) ParseAndVerifyResponse(ctx context.Context, req *http.Request, possibleRequestIDs []string) (*systemdomain.SAMLUser, error) {
	sp, cfg, err := s.ensureProviderWithConfig(ctx)
	if err != nil {
		return nil, err
	}
	assertion, err := sp.ParseResponse(req, possibleRequestIDs)
	if err != nil {
		return nil, fmt.Errorf("parse saml response: %w", err)
	}
	user := s.mapAssertionToUser(cfg, assertion)
	if len(cfg.AllowedDomains) > 0 && user.Email != "" {
		parts := strings.SplitN(user.Email, "@", 2)
		if len(parts) == 2 {
			domain := strings.ToLower(parts[1])
			allowed := false
			for _, item := range cfg.AllowedDomains {
				if strings.EqualFold(item, domain) {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, apperrors.New(40395, http.StatusForbidden, "SAML 邮箱域名不在允许列表中")
			}
		}
	}
	if cfg.AdminGroupAttribute != "" && cfg.AdminGroupValue != "" {
		if !containsFold(user.Groups, cfg.AdminGroupValue) && !strings.EqualFold(user.Attributes[strings.ToLower(cfg.AdminGroupAttribute)], cfg.AdminGroupValue) {
			return nil, apperrors.New(40396, http.StatusForbidden, "SAML 用户不在管理员组中")
		}
	}
	if strings.TrimSpace(user.Account) == "" {
		return nil, apperrors.New(40199, http.StatusUnauthorized, "SAML 响应中缺少可用账号标识")
	}
	return user, nil
}

func (s *SAMLService) MetadataXML(ctx context.Context) ([]byte, error) {
	sp, err := s.ensureProvider(ctx)
	if err != nil {
		return nil, err
	}
	data, err := xml.MarshalIndent(sp.Metadata(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), data...), nil
}

func (s *SAMLService) TestMetadata(ctx context.Context, req systemdomain.SAMLTestRequest) *systemdomain.SAMLTestResult {
	start := time.Now()
	result := &systemdomain.SAMLTestResult{}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	metadata, err := s.loadMetadata(ctx, systemdomain.SAMLConfig{
		IDPMetadataURL: strings.TrimSpace(req.IDPMetadataURL),
		IDPMetadataXML: strings.TrimSpace(req.IDPMetadataXML),
	})
	if err != nil {
		result.Error = err.Error()
		result.LatencyMs = time.Since(start).Milliseconds()
		return result
	}

	result.MetadataOK = true
	result.EntityID = metadata.EntityID
	if len(metadata.IDPSSODescriptors) > 0 {
		idp := metadata.IDPSSODescriptors[0]
		result.SSORedirectURL = endpointLocation(idp.SingleSignOnServices, saml.HTTPRedirectBinding)
		result.SSOPostURL = endpointLocation(idp.SingleSignOnServices, saml.HTTPPostBinding)
		result.SingleLogoutURL = endpointLocation(idp.SingleLogoutServices, saml.HTTPRedirectBinding)
		if result.SingleLogoutURL == "" {
			result.SingleLogoutURL = endpointLocation(idp.SingleLogoutServices, saml.HTTPPostBinding)
		}
		for _, item := range idp.NameIDFormats {
			result.SupportedNameID = append(result.SupportedNameID, string(item))
		}
		for _, key := range idp.KeyDescriptors {
			result.CertificateCount += len(key.KeyInfo.X509Data.X509Certificates)
		}
	}
	result.LatencyMs = time.Since(start).Milliseconds()
	return result
}

func (s *SAMLService) ensureProvider(ctx context.Context) (*saml.ServiceProvider, error) {
	sp, _, err := s.ensureProviderWithConfig(ctx)
	return sp, err
}

func (s *SAMLService) ensureProviderWithConfig(ctx context.Context) (*saml.ServiceProvider, systemdomain.SAMLConfig, error) {
	s.mu.RLock()
	cfg := s.config
	cacheKey := s.cacheKey(cfg)
	if s.service != nil && s.serviceKey == cacheKey {
		sp := s.service
		s.mu.RUnlock()
		return sp, cfg, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	cfg = s.config
	cacheKey = s.cacheKey(cfg)
	if s.service != nil && s.serviceKey == cacheKey {
		return s.service, cfg, nil
	}
	if !cfg.Enabled {
		return nil, cfg, apperrors.New(40197, http.StatusBadRequest, "SAML 认证未启用")
	}

	metadata, err := s.loadMetadata(ctx, cfg)
	if err != nil {
		return nil, cfg, err
	}
	keyPEM, err := s.DecryptPrivateKey(cfg.SPPrivateKey)
	if err != nil {
		return nil, cfg, fmt.Errorf("decrypt saml private key: %w", err)
	}
	signer, err := parsePEMPrivateKey(keyPEM)
	if err != nil {
		return nil, cfg, err
	}
	cert, err := parsePEMCertificate(cfg.SPCertificate)
	if err != nil {
		return nil, cfg, err
	}
	metadataURL, err := url.Parse(cfg.MetadataURL)
	if err != nil || metadataURL.Scheme == "" || metadataURL.Host == "" {
		return nil, cfg, apperrors.New(40095, http.StatusBadRequest, "SAML metadataURL 无效")
	}
	acsURL, err := url.Parse(cfg.ACSURL)
	if err != nil || acsURL.Scheme == "" || acsURL.Host == "" {
		return nil, cfg, apperrors.New(40096, http.StatusBadRequest, "SAML acsURL 无效")
	}

	sp := &saml.ServiceProvider{
		EntityID:              cfg.EntityID,
		Key:                   signer,
		Certificate:           cert,
		MetadataURL:           *metadataURL,
		AcsURL:                *acsURL,
		IDPMetadata:           metadata,
		AuthnNameIDFormat:     saml.NameIDFormat(cfg.NameIDFormat),
		MetadataValidDuration: 24 * time.Hour,
		AllowIDPInitiated:     cfg.AllowIDPInitiated,
		DefaultRedirectURI:    cfg.FrontendCallbackURL,
	}
	if cfg.SignAuthnRequests {
		sp.SignatureMethod = dsig.RSASHA256SignatureMethod
	}
	if cfg.ForceAuthn {
		value := true
		sp.ForceAuthn = &value
	}

	s.service = sp
	s.serviceKey = cacheKey
	return sp, cfg, nil
}

func (s *SAMLService) loadMetadata(ctx context.Context, cfg systemdomain.SAMLConfig) (*saml.EntityDescriptor, error) {
	if strings.TrimSpace(cfg.IDPMetadataXML) != "" {
		metadata, err := samlsp.ParseMetadata([]byte(cfg.IDPMetadataXML))
		if err != nil {
			return nil, fmt.Errorf("parse saml metadata xml: %w", err)
		}
		return metadata, nil
	}
	metadataURL := strings.TrimSpace(cfg.IDPMetadataURL)
	if metadataURL == "" {
		return nil, apperrors.New(40094, http.StatusBadRequest, "SAML IdP metadata 未配置")
	}
	u, err := url.Parse(metadataURL)
	if err != nil {
		return nil, fmt.Errorf("parse saml metadata url: %w", err)
	}
	httpClient := egress.NewClient(egress.Profile{Name: "auth.saml_metadata", Timeout: 10 * time.Second})
	meta, err := samlsp.FetchMetadata(ctx, httpClient, *u)
	if err != nil {
		return nil, fmt.Errorf("fetch saml metadata: %w", err)
	}
	return meta, nil
}

func (s *SAMLService) cacheKey(cfg systemdomain.SAMLConfig) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		cfg.IDPMetadataURL,
		cfg.IDPMetadataXML,
		cfg.EntityID,
		cfg.MetadataURL,
		cfg.ACSURL,
		cfg.SPCertificate,
		cfg.SPPrivateKey,
		cfg.NameIDFormat,
		fmt.Sprintf("%t", cfg.SignAuthnRequests),
		fmt.Sprintf("%t", cfg.ForceAuthn),
		fmt.Sprintf("%t", cfg.AllowIDPInitiated),
	}, "|")))
	return fmt.Sprintf("%x", sum[:])
}

func (s *SAMLService) mapAssertionToUser(cfg systemdomain.SAMLConfig, assertion *saml.Assertion) *systemdomain.SAMLUser {
	attributes := map[string]string{}
	groups := []string{}
	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			values := make([]string, 0, len(attr.Values))
			for _, item := range attr.Values {
				value := strings.TrimSpace(item.Value)
				if value == "" && item.NameID != nil {
					value = strings.TrimSpace(item.NameID.Value)
				}
				if value == "" {
					continue
				}
				values = append(values, value)
			}
			if len(values) == 0 {
				continue
			}
			first := values[0]
			if name := strings.ToLower(strings.TrimSpace(attr.Name)); name != "" {
				attributes[name] = first
				if strings.EqualFold(name, cfg.AttrMapping.Groups) {
					groups = append(groups, values...)
				}
			}
			if friendly := strings.ToLower(strings.TrimSpace(attr.FriendlyName)); friendly != "" {
				attributes[friendly] = first
				if strings.EqualFold(friendly, cfg.AttrMapping.Groups) {
					groups = append(groups, values...)
				}
			}
		}
	}
	nameID := ""
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		nameID = strings.TrimSpace(assertion.Subject.NameID.Value)
	}
	user := &systemdomain.SAMLUser{
		NameID:      nameID,
		Account:     firstNonBlank(attributes[strings.ToLower(cfg.AttrMapping.Account)], attributes["uid"], attributes["account"], attributes["name"], nameID),
		DisplayName: firstNonBlank(attributes[strings.ToLower(cfg.AttrMapping.DisplayName)], attributes["displayname"], attributes["name"], attributes["cn"]),
		Email:       firstNonBlank(attributes[strings.ToLower(cfg.AttrMapping.Email)], attributes["email"], attributes["mail"]),
		Phone:       firstNonBlank(attributes[strings.ToLower(cfg.AttrMapping.Phone)], attributes["phone"], attributes["mobile"], attributes["telephone"]),
		Groups:      uniqueStrings(groups),
		Attributes:  attributes,
	}
	if user.Account == "" {
		user.Account = firstNonBlank(user.Email, nameID)
	}
	if user.DisplayName == "" {
		user.DisplayName = firstNonBlank(user.Account, user.Email, nameID)
	}
	return user
}

func parsePEMPrivateKey(value string) (crypto.Signer, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(value)))
	if block == nil {
		return nil, fmt.Errorf("invalid saml private key pem")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("unsupported saml private key format")
}

func parsePEMCertificate(value string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(value)))
	if block == nil {
		return nil, fmt.Errorf("invalid saml certificate pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse saml certificate: %w", err)
	}
	return cert, nil
}

func generateSelfSignedSAMLCredentials(entityID string) (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return "", "", err
	}
	subject := entityID
	if subject == "" {
		subject = "aegis-saml-sp"
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   subject,
			Organization: []string{"Aegis"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour).UTC(),
		NotAfter:              time.Now().AddDate(10, 0, 0).UTC(),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	return string(certPEM), string(keyPEM), nil
}

func endpointLocation(endpoints []saml.Endpoint, binding string) string {
	for _, item := range endpoints {
		if item.Binding == binding && strings.TrimSpace(item.Location) != "" {
			return item.Location
		}
	}
	return ""
}

func containsFold(items []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}

func firstNonBlank(values ...string) string {
	for _, item := range values {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, item := range values {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
