package egress

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// UDP 探测与自测：经线路向 DNS 服务器发一次 A 查询。
//
// 选 DNS 是因为它是最普遍、最便宜、几乎所有出口都放行的 UDP 往返；
// 查询 ID 随机，回包 ID 对不上就继续等，避免把迟到的旧回包当成功。

// dnsQuery 经已连接的数据报连接查询 name，返回 A / AAAA 记录。
func dnsQuery(ctx context.Context, conn net.Conn, name string) ([]string, error) {
	var idBytes [2]byte
	_, _ = rand.Read(idBytes[:])
	id := binary.BigEndian.Uint16(idBytes[:])
	fqdn, err := dnsmessage.NewName(strings.TrimSuffix(name, ".") + ".")
	if err != nil {
		return nil, fmt.Errorf("查询名无效: %w", err)
	}
	message := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: fqdn, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
	}
	packet, err := message.Pack()
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(DefaultHealthTimeout))
	}
	if _, err := conn.Write(packet); err != nil {
		return nil, fmt.Errorf("发送 DNS 查询失败: %w", err)
	}
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return nil, fmt.Errorf("UDP 无回应：超时前没有收到 DNS 回包")
			}
			return nil, err
		}
		var reply dnsmessage.Message
		if err := reply.Unpack(buf[:n]); err != nil || reply.Header.ID != id || !reply.Header.Response {
			continue
		}
		if reply.Header.RCode != dnsmessage.RCodeSuccess {
			return nil, fmt.Errorf("DNS 返回 %s", reply.Header.RCode)
		}
		answers := []string{}
		for _, answer := range reply.Answers {
			switch body := answer.Body.(type) {
			case *dnsmessage.AResource:
				answers = append(answers, net.IP(body.A[:]).String())
			case *dnsmessage.AAAAResource:
				answers = append(answers, net.IP(body.AAAA[:]).String())
			case *dnsmessage.CNAMEResource:
				answers = append(answers, "CNAME "+body.CNAME.String())
			}
		}
		return answers, nil
	}
}

// probeUDP 经端点做一次 UDP 探测。
func probeUDP(ctx context.Context, endpoint *Endpoint, server, name string) ([]string, error) {
	conn, err := endpoint.dial(ctx, "udp", server, false, 0, 0)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return dnsQuery(ctx, conn, name)
}
