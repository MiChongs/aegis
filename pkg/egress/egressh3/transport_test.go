package egressh3

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	stdlog "log"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"aegis/pkg/egress"
	"github.com/quic-go/quic-go/http3"
	socks5server "github.com/things-go/go-socks5"
)

// TestHTTP3ThroughSocks5 QUIC 报文经 SOCKS5 的 UDP 中继到达本机 HTTP/3 服务端。
func TestHTTP3ThroughSocks5(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http3.Server{
		TLSConfig: http3.ConfigureTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}}),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "h3:"+r.Proto)
		}),
	}
	go func() { _ = server.Serve(udp) }()
	defer server.Close()

	socks := socks5server.NewServer(socks5server.WithLogger(socks5server.NewLogger(stdlog.New(io.Discard, "", 0))))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { _ = socks.Serve(listener) }()

	gw, err := egress.New(egress.Config{
		Enabled:   true,
		Endpoints: []egress.EndpointConfig{{Name: "socks", Protocol: egress.ProtocolSOCKS5H, Address: listener.Addr().String()}},
		Rules:     []egress.RuleConfig{{Name: "quic", Endpoints: []string{"socks"}, Match: egress.MatchConfig{Networks: []string{"udp"}}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	egress.SetDefault(gw)
	defer egress.SetDefault(nil)

	pool := x509.NewCertPool()
	parsed, _ := x509.ParseCertificate(der)
	pool.AddCert(parsed)
	client := NewClient(egress.Profile{Name: "h3.test", Timeout: 10 * time.Second, TLSClientConfig: &tls.Config{RootCAs: pool}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://127.0.0.1:"+portOf(udp.LocalAddr())+"/", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("HTTP/3 请求失败: %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "h3:HTTP/3.0" {
		t.Fatalf("响应不对: %q", body)
	}
	stat := gw.Stats().Endpoints[0]
	if stat.UDPSessions == 0 || stat.PacketsOut == 0 {
		t.Fatalf("QUIC 报文没有经过 SOCKS5 线路: %+v", stat)
	}
}

func portOf(addr net.Addr) string {
	_, port, _ := net.SplitHostPort(addr.String())
	return port
}
