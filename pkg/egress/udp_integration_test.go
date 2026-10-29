package egress

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shadowsocks/go-shadowsocks2/core"
	"github.com/shadowsocks/go-shadowsocks2/socks"
	"golang.org/x/net/dns/dnsmessage"
)

// UDP 的互操作测试：SOCKS5 用 things-go/go-socks5 的 UDP ASSOCIATE 服务端，
// Shadowsocks 用官方 core 包的 PacketConn 写服务端，Trojan 按协议规范写一个最小服务端。

func startUDPEcho(t *testing.T, prefix string) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(append([]byte(prefix), buf[:n]...), addr)
		}
	}()
	return pc.LocalAddr().String()
}

// startDNSServer 对任何 A 查询回答 203.0.113.7。
func startDNSServer(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buf[:n]) != nil || len(query.Questions) == 0 {
				continue
			}
			reply := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: query.Header.ID, Response: true, RecursionAvailable: true},
				Questions: query.Questions,
			}
			if query.Questions[0].Type == dnsmessage.TypeA {
				reply.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{Name: query.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   &dnsmessage.AResource{A: [4]byte{203, 0, 113, 7}},
				}}
			}
			packet, _ := reply.Pack()
			_, _ = pc.WriteTo(packet, addr)
		}
	}()
	return pc.LocalAddr().String()
}

// relayUDP 把一个报文发往 target 并等回包（测试服务端共用）。
func relayUDP(target string, payload []byte) ([]byte, error) {
	conn, err := net.Dial("udp", target)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(payload); err != nil {
		return nil, err
	}
	buf := make([]byte, 65535)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func startShadowsocksUDPServer(t *testing.T, method, password string) string {
	t.Helper()
	cipher, err := core.PickCipher(method, nil, password)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	pc := cipher.PacketConn(raw)
	go func() {
		buf := make([]byte, 65535)
		for {
			n, client, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			target := socks.SplitAddr(buf[:n])
			if target == nil {
				continue
			}
			payload := append([]byte(nil), buf[len(target):n]...)
			targetAddr := append(socks.Addr(nil), target...)
			go func() {
				reply, err := relayUDP(targetAddr.String(), payload)
				if err != nil {
					return
				}
				_, _ = pc.WriteTo(append(targetAddr, reply...), client)
			}()
		}
	}()
	return raw.LocalAddr().String()
}

func selfSignedTLS(t *testing.T) *tls.Config {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "trojan.test"}, DNSNames: []string{"trojan.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

// startTrojanServer 最小的 Trojan 服务端：TCP CONNECT 与 UDP ASSOCIATE 都支持。
func startTrojanServer(t *testing.T, password string) string {
	t.Helper()
	sum := sha256.Sum224([]byte(password))
	auth := hex.EncodeToString(sum[:])
	listener, err := tls.Listen("tcp", "127.0.0.1:0", selfSignedTLS(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				head := make([]byte, 56+2+1)
				if _, err := io.ReadFull(reader, head); err != nil || string(head[:56]) != auth {
					return
				}
				target, err := socks.ReadAddr(reader)
				if err != nil {
					return
				}
				if _, err := reader.Discard(2); err != nil {
					return
				}
				if head[58] == trojanCmdConnect {
					upstream, err := net.Dial("tcp", target.String())
					if err != nil {
						return
					}
					defer upstream.Close()
					go func() { _, _ = io.Copy(upstream, reader) }()
					_, _ = io.Copy(conn, upstream)
					return
				}
				for {
					addr, err := socks.ReadAddr(reader)
					if err != nil {
						return
					}
					var meta [4]byte
					if _, err := io.ReadFull(reader, meta[:]); err != nil {
						return
					}
					payload := make([]byte, int(meta[0])<<8|int(meta[1]))
					if _, err := io.ReadFull(reader, payload); err != nil {
						return
					}
					reply, err := relayUDP(addr.String(), payload)
					if err != nil {
						continue
					}
					frame := append(append([]byte(nil), addr...), 0, 0, '\r', '\n')
					putUint16(frame[len(addr):], len(reply))
					if _, err := conn.Write(append(frame, reply...)); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return listener.Addr().String()
}

func udpRoundTrip(t *testing.T, gw *Gateway, profile Profile, target, payload string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := gw.DialProfile(ctx, profile, "udp", target)
	if err != nil {
		t.Fatalf("UDP 拨号失败: %v", err)
	}
	defer conn.Close()
	if _, ok := conn.(net.PacketConn); !ok {
		t.Fatalf("UDP 连接应同时实现 net.PacketConn（net.Resolver 依赖它）")
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	for i := 0; i < 3; i++ {
		if _, err := conn.Write([]byte(payload)); err != nil {
			t.Fatalf("发送失败: %v", err)
		}
		buf := make([]byte, 2048)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("接收失败: %v", err)
		}
		if got := string(buf[:n]); !strings.HasSuffix(got, payload) {
			t.Fatalf("回包不对: %q", got)
		}
		if i == 2 {
			return string(buf[:n])
		}
	}
	return ""
}

func udpGateway(t *testing.T, endpoints ...EndpointConfig) *Gateway {
	t.Helper()
	names := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		names = append(names, endpoint.Name)
	}
	gw, err := New(Config{
		Enabled:   true,
		Health:    HealthConfig{Enabled: false},
		Endpoints: endpoints,
		Rules:     []RuleConfig{{Name: "all", Endpoints: names[:1], Match: MatchConfig{MatchAll: true}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	return gw
}

func TestUDPDirect(t *testing.T) {
	echo := startUDPEcho(t, "direct:")
	gw := udpGateway(t, EndpointConfig{Name: "d", Protocol: ProtocolDirect})
	if got := udpRoundTrip(t, gw, Profile{}, echo, "ping"); got != "direct:ping" {
		t.Fatalf("got %q", got)
	}
}

func TestUDPThroughSocks5(t *testing.T) {
	echo := startUDPEcho(t, "socks:")
	for _, protocol := range []Protocol{ProtocolSOCKS5, ProtocolSOCKS5H} {
		plain := startSocks5Server(t, "", "")
		gw := udpGateway(t, EndpointConfig{Name: "s", Protocol: protocol, Address: plain})
		if got := udpRoundTrip(t, gw, Profile{}, echo, "hello"); got != "socks:hello" {
			t.Fatalf("%s got %q", protocol, got)
		}
	}
	authed := startSocks5Server(t, "u", "p")
	gw := udpGateway(t, EndpointConfig{Name: "s", Protocol: ProtocolSOCKS5H, Address: authed, Username: "u", Password: "p"})
	udpRoundTrip(t, gw, Profile{}, echo, "with-auth")

	wrong := udpGateway(t, EndpointConfig{Name: "s", Protocol: ProtocolSOCKS5H, Address: authed, Username: "u", Password: "x"})
	if _, err := wrong.DialProfile(context.Background(), Profile{}, "udp", echo); err == nil {
		t.Fatalf("密码错误应在 ASSOCIATE 阶段失败")
	}
}

func TestUDPThroughShadowsocks(t *testing.T) {
	echo := startUDPEcho(t, "ss:")
	for _, method := range []string{"aes-128-gcm", "chacha20-ietf-poly1305"} {
		server := startShadowsocksUDPServer(t, method, "secret")
		gw := udpGateway(t, EndpointConfig{Name: "ss", Protocol: ProtocolShadowsocks, Address: server, Shadowsocks: ShadowsocksConfig{Method: method, Password: "secret"}})
		if got := udpRoundTrip(t, gw, Profile{}, echo, "x"); got != "ss:x" {
			t.Fatalf("%s got %q", method, got)
		}
	}
}

func TestUDPThroughTrojan(t *testing.T) {
	echo := startUDPEcho(t, "trojan:")
	server := startTrojanServer(t, "pw")
	gw := udpGateway(t, EndpointConfig{
		Name: "tj", Protocol: ProtocolTrojan, Address: server, Password: "pw",
		TLS: TLSConfig{Enabled: true, ServerName: "trojan.test", InsecureSkipVerify: true},
	})
	if got := udpRoundTrip(t, gw, Profile{}, echo, "over-tls"); got != "trojan:over-tls" {
		t.Fatalf("got %q", got)
	}
}

// TestUDPChains Trojan 的 UDP 在 TLS 流里，可以接在只能走 TCP 的跳板后面；
// Shadowsocks 的 UDP 要以 UDP 发给服务端，上一跳是 HTTP CONNECT 时不能承载 UDP。
func TestUDPChains(t *testing.T) {
	echo := startUDPEcho(t, "chain:")
	connect := startConnectProxy(t, "")
	trojan := startTrojanServer(t, "pw")
	ss := startShadowsocksUDPServer(t, "aes-256-gcm", "k")
	socksHop := startSocks5Server(t, "", "")

	gw := udpGateway(t,
		EndpointConfig{Name: "tj", Protocol: ProtocolTrojan, Address: trojan, Password: "pw", Via: "hop", TLS: TLSConfig{Enabled: true, InsecureSkipVerify: true}},
		EndpointConfig{Name: "hop", Protocol: ProtocolHTTP, Address: connect},
	)
	if got := udpRoundTrip(t, gw, Profile{}, echo, "via-connect"); got != "chain:via-connect" {
		t.Fatalf("got %q", got)
	}

	blocked := udpGateway(t,
		EndpointConfig{Name: "ss", Protocol: ProtocolShadowsocks, Address: ss, Shadowsocks: ShadowsocksConfig{Method: "aes-256-gcm", Password: "k"}, Via: "hop"},
		EndpointConfig{Name: "hop", Protocol: ProtocolHTTP, Address: connect},
	)
	if endpoint, _ := blocked.Endpoint("ss"); endpoint.SupportsUDP() {
		t.Fatalf("经 HTTP 跳板的 Shadowsocks 不能承载 UDP")
	}
	if _, err := blocked.DialProfile(context.Background(), Profile{}, "udp", echo); err == nil {
		t.Fatalf("应拒绝")
	}

	viaSocks := udpGateway(t,
		EndpointConfig{Name: "ss", Protocol: ProtocolShadowsocks, Address: ss, Shadowsocks: ShadowsocksConfig{Method: "aes-256-gcm", Password: "k"}, Via: "hop"},
		EndpointConfig{Name: "hop", Protocol: ProtocolSOCKS5H, Address: socksHop},
	)
	if got := udpRoundTrip(t, viaSocks, Profile{}, echo, "ss-via-socks"); got != "chain:ss-via-socks" {
		t.Fatalf("got %q", got)
	}
}

// TestUDPRouting UDP 只在能承载它的线路里选；networks 维度只匹配对应的传输层。
func TestUDPRouting(t *testing.T) {
	echo := startUDPEcho(t, "routed:")
	connect := startConnectProxy(t, "")
	socksAddr := startSocks5Server(t, "", "")
	gw, err := New(Config{
		Enabled: true,
		Health:  HealthConfig{Enabled: false},
		Endpoints: []EndpointConfig{
			{Name: "http", Protocol: ProtocolHTTP, Address: connect},
			{Name: "socks", Protocol: ProtocolSOCKS5H, Address: socksAddr},
			{Name: "socks-tcp-only", Protocol: ProtocolSOCKS5H, Address: socksAddr, UDP: boolPtr(false)},
		},
		Rules: []RuleConfig{
			{Name: "no-quic", Action: ActionReject, Match: MatchConfig{Networks: []string{"udp"}, Ports: []int{443}}},
			{Name: "all", Endpoints: []string{"http", "socks-tcp-only", "socks"}, Match: MatchConfig{MatchAll: true}},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()

	if tcp := gw.Route(Target{Host: "example.com", Port: 80}); tcp.Endpoint == nil || tcp.Endpoint.Name() != "http" {
		t.Fatalf("TCP 应选第一个线路: %+v", tcp)
	}
	host, portText, _ := net.SplitHostPort(echo)
	port, _ := strconv.Atoi(portText)
	if udp := gw.Route(Target{Host: host, Port: port, Network: "udp"}); udp.Endpoint == nil || udp.Endpoint.Name() != "socks" {
		t.Fatalf("UDP 应跳过 http 与关掉 UDP 的线路: %+v", udp)
	}
	udpRoundTrip(t, gw, Profile{}, echo, "skip")
	if udp443 := gw.Route(Target{Host: "example.com", Port: 443, Network: "udp"}); udp443.Action != ActionReject {
		t.Fatalf("UDP 443 应被 networks 规则拒绝: %+v", udp443)
	}
	if tcp443 := gw.Route(Target{Host: "example.com", Port: 443}); tcp443.Action != ActionProxy {
		t.Fatalf("TCP 443 不受 UDP 规则影响: %+v", tcp443)
	}
	explanation := gw.Explain(Target{Host: "example.com", Port: 443, Network: "udp"})
	if explanation.Network != "udp" || explanation.Rule != "no-quic" {
		t.Fatalf("解释不对: %+v", explanation)
	}

	if _, err := New(Config{Enabled: true, Endpoints: []EndpointConfig{{Name: "h", Protocol: ProtocolHTTP, Address: connect, UDP: boolPtr(true)}}}, nil); err == nil {
		t.Fatalf("HTTP 线路显式开 UDP 应校验失败")
	}
}

// TestListenPacketRoutesPerDestination 一个套接字发往两个目标，分别走直连与 SOCKS5。
func TestListenPacketRoutesPerDestination(t *testing.T) {
	direct := startUDPEcho(t, "A:")
	proxied := startUDPEcho(t, "B:")
	socksAddr := startSocks5Server(t, "", "")
	_, proxiedPort, _ := net.SplitHostPort(proxied)
	port, _ := strconv.Atoi(proxiedPort)
	gw, err := New(Config{
		Enabled:   true,
		Health:    HealthConfig{Enabled: false},
		Endpoints: []EndpointConfig{{Name: "socks", Protocol: ProtocolSOCKS5H, Address: socksAddr}},
		Rules:     []RuleConfig{{Name: "b", Endpoints: []string{"socks"}, Match: MatchConfig{Ports: []int{port}}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()

	pc, err := gw.ListenPacket(context.Background(), Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	addrA, _ := ParseAddr("udp", direct)
	addrB, _ := ParseAddr("udp", proxied)
	if _, err := pc.WriteTo([]byte("1"), addrA); err != nil {
		t.Fatal(err)
	}
	if _, err := pc.WriteTo([]byte("2"), addrB); err != nil {
		t.Fatal(err)
	}
	_ = pc.SetReadDeadline(time.Now().Add(3 * time.Second))
	got := map[string]string{}
	buf := make([]byte, 64)
	for len(got) < 2 {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			t.Fatalf("ReadFrom: %v (%v)", err, got)
		}
		got[from.String()] = string(buf[:n])
	}
	if got[direct] != "A:1" || got[proxied] != "B:2" {
		t.Fatalf("回包对不上来源: %v", got)
	}
	stats := gw.Stats()
	if stats.Endpoints[0].UDPSessions != 1 || stats.Endpoints[0].PacketsIn == 0 {
		t.Fatalf("UDP 统计没有记到线路上: %+v", stats.Endpoints[0])
	}
	_ = pc.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, _, err := pc.ReadFrom(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("读超时应返回 deadline 错误: %v", err)
	}
}

func TestResolverAndUDPProbe(t *testing.T) {
	dns := startDNSServer(t)
	socksAddr := startSocks5Server(t, "", "")
	connect := startConnectProxy(t, "")
	gw, err := New(Config{
		Enabled: true,
		Health:  HealthConfig{Enabled: true, ProbeURL: "", UDPProbeServer: dns, UDPProbeName: "probe.test"},
		Endpoints: []EndpointConfig{
			{Name: "socks", Protocol: ProtocolSOCKS5H, Address: socksAddr},
			{Name: "http", Protocol: ProtocolHTTP, Address: connect},
			{Name: "dead-udp", Protocol: ProtocolShadowsocks, Address: "127.0.0.1:9", Shadowsocks: ShadowsocksConfig{Method: "aes-128-gcm", Password: "x"}},
		},
		Rules: []RuleConfig{{Name: "all", Endpoints: []string{"socks"}, Match: MatchConfig{MatchAll: true}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()

	addrs, err := gw.Resolver(Profile{}, dns).LookupHost(context.Background(), "anything.example")
	if err != nil || len(addrs) == 0 || addrs[0] != "203.0.113.7" {
		t.Fatalf("经网关的解析器: %v %v", addrs, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	results := map[string]ProbeResult{}
	for _, result := range gw.ProbeAll(ctx) {
		results[result.Endpoint] = result
	}
	if !results["socks"].UDPChecked || !results["socks"].UDPOK {
		t.Fatalf("SOCKS5 线路的 UDP 探测应成功: %+v", results["socks"])
	}
	if results["http"].UDPChecked {
		t.Fatalf("HTTP 线路不该做 UDP 探测")
	}
	if !results["dead-udp"].UDPChecked || results["dead-udp"].UDPOK {
		t.Fatalf("没有服务端的线路 UDP 探测应失败: %+v", results["dead-udp"])
	}
	for _, stat := range gw.Stats().Endpoints {
		if stat.Name == "socks" && (!stat.UDP || !stat.UDPHealthy || stat.UDPLatencyMS < 0) {
			t.Fatalf("UDP 运行态不对: %+v", stat)
		}
		if stat.Name == "http" && stat.UDP {
			t.Fatalf("HTTP 线路不该标成能承载 UDP")
		}
	}

	test := gw.Test(ctx, TestRequest{Network: "udp", URL: dns, Name: "self.test"})
	if !test.OK || len(test.Answers) != 1 || test.Endpoint != "socks" {
		t.Fatalf("UDP 自测: %+v", test)
	}
}
