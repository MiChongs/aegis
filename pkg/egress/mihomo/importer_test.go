package mihomo

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestImportYAML(t *testing.T) {
	result, err := Import([]byte(`
mixed-port: 7890
allow-lan: true
mode: rule
tun: {enable: true}
hosts: {"router.lan": 192.168.1.1}
proxies:
  - {name: "香港 01", type: ss, server: hk.example.com, port: 443, cipher: aes-256-gcm, password: p}
proxy-providers:
  机场:
    type: http
    url: https://sub.example.com/x?token=abc
    interval: 3600
proxy-groups:
  - {name: 节点选择, type: select, proxies: [香港 01, DIRECT], use: [机场]}
rules:
  - PROCESS-NAME,curl,DIRECT
  - GEOIP,CN,DIRECT
  - MATCH,节点选择
`))
	if err != nil {
		t.Fatal(err)
	}
	p := result.Profile
	if len(p.Proxies) != 1 || len(p.ProxyGroups) != 1 || len(p.ProxyProviders) != 1 {
		t.Fatalf("解析不全: %+v", p)
	}
	if len(p.Rules) != 2 {
		t.Fatalf("应去掉进程规则: %v", p.Rules)
	}
	if _, ok := p.Extra["hosts"]; !ok {
		t.Fatalf("hosts 应保留到 Extra")
	}
	if _, ok := p.Extra["tun"]; ok {
		t.Fatalf("tun 必须被忽略")
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("应提示忽略了托管键")
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("导入结果应能通过校验: %v", err)
	}
}

func TestImportURIs(t *testing.T) {
	vmess, _ := json.Marshal(map[string]any{"v": "2", "ps": "美国, 01", "add": "us.example.com", "port": "443", "id": "uuid-1", "aid": "0", "net": "ws", "path": "/ray", "host": "cdn.example.com", "tls": "tls"})
	lines := []string{
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pass")) + "@1.2.3.4:8388#SS%20节点",
		"vmess://" + base64.StdEncoding.EncodeToString(vmess),
		"vless://uuid-2@jp.example.com:443?security=reality&pbk=PUB&sid=ab&type=grpc&serviceName=svc&flow=xtls-rprx-vision&sni=www.apple.com&fp=chrome#日本",
		"trojan://secret@sg.example.com:443?sni=sg.example.com&type=ws&path=%2Fws#新加坡",
		"hy2://auth@hy.example.com:8443?sni=hy.example.com&obfs=salamander&obfs-password=x&insecure=1#HY2",
		"tuic://uuid-3:pw@tuic.example.com:443?congestion_control=bbr&alpn=h3#TUIC",
		"ss://bad",
	}
	payload := base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
	result, err := Import([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != "uri" || len(result.Profile.Proxies) != 6 {
		t.Fatalf("应识别 6 个节点: %d %v", len(result.Profile.Proxies), result.Warnings)
	}
	byType := map[string]map[string]any{}
	for _, proxy := range result.Profile.Proxies {
		byType[proxy["type"].(string)] = proxy
	}
	if byType["vmess"]["name"] != "美国  01" {
		t.Fatalf("名称里的逗号应被替换: %q", byType["vmess"]["name"])
	}
	if byType["vmess"]["ws-opts"].(map[string]any)["path"] != "/ray" {
		t.Fatalf("vmess 传输解析不对: %+v", byType["vmess"])
	}
	reality := byType["vless"]["reality-opts"].(map[string]any)
	if reality["public-key"] != "PUB" || byType["vless"]["network"] != "grpc" {
		t.Fatalf("vless reality 解析不对: %+v", byType["vless"])
	}
	if byType["hysteria2"]["obfs"] != "salamander" || byType["hysteria2"]["skip-cert-verify"] != true {
		t.Fatalf("hy2 解析不对: %+v", byType["hysteria2"])
	}
	if byType["ss"]["name"] != "SS 节点" || byType["ss"]["cipher"] != "aes-256-gcm" {
		t.Fatalf("ss 解析不对: %+v", byType["ss"])
	}
}

func TestRenderInjectsSystem(t *testing.T) {
	p := Profile{
		Proxies:     []map[string]any{{"name": "a", "type": "direct", "interval": float64(3600)}},
		ProxyGroups: []map[string]any{{"name": "g", "type": "select", "proxies": []any{"a"}}},
		Extra:       map[string]any{"tun": map[string]any{"enable": true}, "hosts": map[string]any{"x": "1.1.1.1"}},
		Rules:       []string{"GEOIP,CN,DIRECT"},
	}
	out, err := Render(p, System{ControllerAddr: "127.0.0.1:1", Secret: "s", InboundPort: 2, Password: "pw", TargetPorts: map[string]int{"g": 3}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, want := range []string{"proxy: g", "port: 3", "udp: true", "MATCH,DIRECT", "external-controller: 127.0.0.1:1", "interval: 3600", "hosts:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("缺少 %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "tun:") || strings.Contains(text, "3600.0") {
		t.Fatalf("不该出现 tun 或浮点整数:\n%s", text)
	}
	if _, err := Render(p, System{ControllerAddr: "127.0.0.1:1", InboundPort: 2, Password: "pw", TargetPorts: map[string]int{"missing": 3}}); err == nil {
		t.Fatalf("引用核心里不存在的目标应报错")
	}
	if err := (Profile{ProxyGroups: []map[string]any{{"name": "g", "type": "select", "proxies": []any{"missing"}}}}).Validate(); err == nil {
		t.Fatalf("引用不存在的成员应报错")
	}
}
