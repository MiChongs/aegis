// Package egressh3 让 HTTP/3（QUIC）出站也按出海网关的路由表走。
//
// QUIC 跑在 UDP 上，标准库的 http.Transport 管不到它；这里给 quic-go 的 http3.Transport
// 换一个拨号函数：每条 QUIC 连接用一个 egress.ListenPacket 套接字，目标地址保留域名
// （egress.Addr），于是 UDP 报文经规则选中的线路（直连、SOCKS5、Shadowsocks、Trojan、mihomo）发出。
// 线路不能承载 UDP 时拨号失败，调用方按惯例回落 HTTP/2 即可。
//
// 单独成包是为了不让 pkg/egress 本身依赖 quic-go。
package egressh3

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strconv"

	"aegis/pkg/egress"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// NewTransport 返回经全局默认网关出站的 HTTP/3 Transport。
func NewTransport(profile egress.Profile) *http3.Transport {
	return &http3.Transport{
		TLSClientConfig: profile.TLSClientConfig,
		Dial: func(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
			return Dial(ctx, profile, addr, tlsCfg, cfg)
		},
	}
}

// NewClient 返回只走 HTTP/3 的客户端。
func NewClient(profile egress.Profile) *http.Client {
	return &http.Client{Transport: NewTransport(profile), Timeout: profile.Timeout, CheckRedirect: profile.CheckRedirect}
}

// Dial 经网关建立一条 QUIC 连接（0-RTT 可用）。连接关闭时一并回收底层套接字。
func Dial(ctx context.Context, profile egress.Profile, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, err
	}
	pc, err := egress.ListenPacket(ctx, profile)
	if err != nil {
		return nil, err
	}
	conn, err := quic.DialEarly(ctx, pc, egress.Addr{Net: "udp", Host: host, Port: port}, tlsCfg, cfg)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	go func() {
		<-conn.Context().Done()
		_ = pc.Close()
	}()
	return conn, nil
}
