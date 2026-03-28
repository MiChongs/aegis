package service

import (
	"context"
	"strings"

	securitydomain "aegis/internal/domain/security"
)

// IPReputationProvider 提供外部 IP 风险情报查询能力。
type IPReputationProvider interface {
	Name() string
	Lookup(ctx context.Context, ip string) (*securitydomain.IPRiskRecord, error)
}

func normalizeIPRiskRecord(ip string, rec *securitydomain.IPRiskRecord) *securitydomain.IPRiskRecord {
	if rec == nil {
		return nil
	}
	rec.IP = strings.TrimSpace(ip)
	rec.Country = strings.TrimSpace(rec.Country)
	rec.Region = strings.TrimSpace(rec.Region)
	rec.ISP = strings.TrimSpace(rec.ISP)
	rec.RiskTag = classifyIPRiskTag(rec)
	if rec.RiskScore < 0 {
		rec.RiskScore = 0
	}
	if rec.RiskScore > 100 {
		rec.RiskScore = 100
	}
	return rec
}

func classifyIPRiskTag(rec *securitydomain.IPRiskRecord) string {
	switch {
	case rec == nil:
		return "normal"
	case rec.IsTor:
		return "tor"
	case rec.IsVPN:
		return "vpn"
	case rec.IsProxy:
		return "proxy"
	case rec.IsDatacenter:
		return "datacenter"
	case rec.RiskScore >= 75:
		return "bot"
	default:
		return "normal"
	}
}
