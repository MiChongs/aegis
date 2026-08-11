package service

import (
	"context"
	"strings"

	securitydomain "aegis/internal/domain/security"
	"aegis/pkg/circuitbreaker"
	"aegis/pkg/resilience"
	"aegis/pkg/timeutil"
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

type circuitBreakeredIPReputationProvider struct {
	name     string
	upstream IPReputationProvider
}

func wrapIPReputationProvider(provider IPReputationProvider) IPReputationProvider {
	if provider == nil {
		return nil
	}
	return &circuitBreakeredIPReputationProvider{
		name:     circuitbreaker.Name("risk", provider.Name()),
		upstream: provider,
	}
}

func (p *circuitBreakeredIPReputationProvider) Name() string {
	return p.upstream.Name()
}

func (p *circuitBreakeredIPReputationProvider) Lookup(ctx context.Context, ip string) (*securitydomain.IPRiskRecord, error) {
	return resilience.Execute(ctx, p.name, resilience.Options{
		Timeout:     timeutil.Seconds(6),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(150),
		MaxBackoff:  timeutil.Milliseconds(1200),
		RatePerSec:  8,
		Burst:       16,
	}, func(callCtx context.Context) (*securitydomain.IPRiskRecord, error) {
		return p.upstream.Lookup(callCtx, ip)
	})
}
