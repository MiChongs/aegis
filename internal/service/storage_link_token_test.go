package service

import (
	"strings"
	"testing"
	"time"
)

func TestStorageLinkTokenPermanentRoundTrip(t *testing.T) {
	signer := newStorageLinkSigner("master")
	token, err := signer.Encode(storageLinkClaims{ConfigID: 7, ObjectKey: "tickets/202610/ab.png", AppID: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !isStorageLinkToken(token) {
		t.Fatalf("新令牌应能与旧票据区分：%q", token)
	}
	// 永久地址：多年以后仍然有效
	ticket, ok := signer.Decode(token, time.Now().AddDate(10, 0, 0))
	if !ok {
		t.Fatal("永久令牌不应过期")
	}
	if ticket.ConfigID != 7 || ticket.ObjectKey != "tickets/202610/ab.png" || ticket.AppID != 3 || !ticket.ExpiresAt.IsZero() {
		t.Fatalf("解析结果不符：%+v", ticket)
	}
	// 新的签名器（模拟重启）同一主密钥仍能解析 —— 不依赖 Redis 或进程状态
	if _, ok := newStorageLinkSigner("master").Decode(token, time.Now()); !ok {
		t.Fatal("同一主密钥派生的签名器应能解析")
	}
}

func TestStorageLinkTokenExpiry(t *testing.T) {
	signer := newStorageLinkSigner("master")
	now := time.Now()
	token, err := signer.Encode(storageLinkClaims{ConfigID: 1, ObjectKey: "a.txt", Download: true, FileName: "a.txt", ExpiresAt: now.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	ticket, ok := signer.Decode(token, now)
	if !ok || !ticket.Download || ticket.FileName != "a.txt" || ticket.ExpiresAt.IsZero() {
		t.Fatalf("限时令牌在有效期内应能解析：%+v %v", ticket, ok)
	}
	if _, ok := signer.Decode(token, now.Add(2*time.Minute)); ok {
		t.Fatal("过期令牌不应被接受")
	}
}

func TestStorageLinkTokenRejectsForgery(t *testing.T) {
	signer := newStorageLinkSigner("master")
	token, err := signer.Encode(storageLinkClaims{ConfigID: 1, ObjectKey: "mine.png"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := newStorageLinkSigner("other").Decode(token, time.Now()); ok {
		t.Fatal("别的主密钥签的令牌不应被接受")
	}
	// 换掉负载、保留签名：等同于想读别人的对象
	forged, _ := signer.Encode(storageLinkClaims{ConfigID: 1, ObjectKey: "theirs.png"})
	mixed := forged[:strings.LastIndex(forged, ".")] + token[strings.LastIndex(token, "."):]
	if _, ok := signer.Decode(mixed, time.Now()); ok {
		t.Fatal("负载与签名不匹配时不应被接受")
	}
	for _, bad := range []string{"", ".", "abc.", ".abc", "0123456789abcdef0123456789abcdef", "a.b.c"} {
		if _, ok := signer.Decode(bad, time.Now()); ok {
			t.Fatalf("畸形令牌不应被接受：%q", bad)
		}
	}
}
