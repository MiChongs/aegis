package service

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"aegis/pkg/egress"
)

const storageOutboundTimeout = 60 * time.Second

func validateStorageEndpoint(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("存储 Endpoint 无效")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("存储 Endpoint 仅允许 http 或 https")
	}
	if parsed.User != nil {
		return fmt.Errorf("存储 Endpoint 不允许在 URL 中携带凭据")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && egress.IsBlockedOutboundIP(ip) {
		return fmt.Errorf("存储 Endpoint 不允许指向本机或私有网络")
	}
	return nil
}

// newStorageOutboundTransport 存储驱动的统一出站 transport。
//
// 它交给出海网关而不是自己拨号，于是境外对象存储（S3 / Azure Blob）能按域名后缀
// 走代理线路；BlockPrivateTargets 保留了原有的 SSRF 防线——**只在直连时生效**，
// 走代理时目标由对端解析，本地校验无从谈起，该场景靠端点白名单约束。
func newStorageOutboundTransport() http.RoundTripper {
	return egress.NewTransport(egress.Profile{
		Name:                  "storage.outbound",
		DialTimeout:           10 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		BlockPrivateTargets:   true,
	})
}
