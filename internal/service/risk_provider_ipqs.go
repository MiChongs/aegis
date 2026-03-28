package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aegis/internal/config"
	securitydomain "aegis/internal/domain/security"

	"github.com/go-resty/resty/v2"
)

type IPQualityScoreProvider struct {
	baseURL string
	apiKey  string
	cfg     config.RiskIPQualityScoreConfig
	client  *resty.Client
}

type ipqsLookupResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	CountryCode  string `json:"country_code"`
	Region       string `json:"region"`
	ISP          string `json:"ISP"`
	Organization string `json:"organization"`
	FraudScore   int    `json:"fraud_score"`
	Proxy        bool   `json:"proxy"`
	VPN          bool   `json:"vpn"`
	Tor          bool   `json:"tor"`
	ActiveVPN    bool   `json:"active_vpn"`
	ActiveTor    bool   `json:"active_tor"`
	BotStatus    bool   `json:"bot_status"`
	IsCrawler    bool   `json:"is_crawler"`
	RecentAbuse  bool   `json:"recent_abuse"`
	Host         bool   `json:"host"`
}

func NewIPQualityScoreProvider(cfg config.RiskIPReputationConfig) *IPQualityScoreProvider {
	apiKey := strings.TrimSpace(cfg.IPQS.APIKey)
	if apiKey == "" {
		return nil
	}
	client := resty.New().
		SetTimeout(cfg.Timeout).
		SetRetryCount(1).
		SetHeader("Accept", "application/json")
	return &IPQualityScoreProvider{
		baseURL: strings.TrimRight(cfg.IPQS.BaseURL, "/"),
		apiKey:  apiKey,
		cfg:     cfg.IPQS,
		client:  client,
	}
}

func (p *IPQualityScoreProvider) Name() string {
	return "ipqualityscore"
}

func (p *IPQualityScoreProvider) Lookup(ctx context.Context, ip string) (*securitydomain.IPRiskRecord, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, nil
	}

	var resp ipqsLookupResponse
	httpResp, err := p.client.R().
		SetContext(ctx).
		SetResult(&resp).
		SetQueryParams(map[string]string{
			"strictness":        strconv.Itoa(p.cfg.Strictness),
			"fast":              strconv.FormatBool(p.cfg.Fast),
			"mobile":            strconv.FormatBool(p.cfg.Mobile),
			"lighter_penalties": strconv.FormatBool(p.cfg.LighterPenalties),
		}).
		Get(fmt.Sprintf("%s/%s/%s", p.baseURL, p.apiKey, ip))
	if err != nil {
		return nil, fmt.Errorf("query ipqualityscore: %w", err)
	}
	if httpResp.StatusCode() >= http.StatusBadRequest {
		return nil, fmt.Errorf("query ipqualityscore: status=%d", httpResp.StatusCode())
	}
	if !resp.Success && resp.Message != "" {
		return nil, fmt.Errorf("query ipqualityscore: %s", resp.Message)
	}

	record := &securitydomain.IPRiskRecord{
		IP:           ip,
		Country:      strings.TrimSpace(resp.CountryCode),
		Region:       strings.TrimSpace(resp.Region),
		ISP:          firstNonEmptyString(resp.ISP, resp.Organization),
		RiskScore:    resp.FraudScore,
		IsProxy:      resp.Proxy,
		IsVPN:        resp.VPN || resp.ActiveVPN,
		IsTor:        resp.Tor || resp.ActiveTor,
		IsDatacenter: resp.Host,
		LastSeenAt:   time.Now().UTC(),
	}
	if resp.BotStatus || resp.IsCrawler || resp.RecentAbuse {
		if record.RiskScore < 75 {
			record.RiskScore = 75
		}
	}
	return normalizeIPRiskRecord(ip, record), nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
