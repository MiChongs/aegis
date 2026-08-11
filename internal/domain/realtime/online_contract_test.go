package realtime

import (
	"encoding/json"
	"testing"
	"time"
)

// 管理端在线用户表直接读这些字段名，缺一个就是一列空白。
//
// 这条测试是照着一个真实故障写的：presence 存在 Redis 里，那里只有 userId，
// 而 AppOnlineUser 当时既没有 account/nickname（要回 Postgres 查），
// 也没有把 IP 与连接时间从 SampleConnection 提到顶层。前端按
// account / ip / connectedAt 渲染，于是三列里两列恒为空，只有时间列
// 因为回落到 lastSeenAt 才有值 —— 看起来像"连接上了但没传信息"。
//
// 控制台那边的 OnlineUserItem 同时也从 Record<string, unknown> 改成了具名类型，
// 两边一起才守得住：这里保证字段存在，那里保证读的名字没写错。
func TestAppOnlineUserExposesEveryColumnTheConsoleRenders(t *testing.T) {
	connectedAt := time.Date(2026, 8, 12, 5, 7, 6, 0, time.UTC)
	item := AppOnlineUser{
		AppID:       7,
		UserID:      4231,
		Account:     "alice",
		Nickname:    "Alice",
		IP:          "203.0.113.9",
		Connections: 2,
		ConnectedAt: connectedAt,
		LastSeenAt:  connectedAt.Add(time.Minute),
	}

	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}

	// 键名与 aegis-console 的 OnlineUserItem 一一对应。
	for _, field := range []string{"account", "nickname", "ip", "connectedAt", "lastSeenAt", "connections", "userId"} {
		if _, ok := decoded[field]; !ok {
			t.Errorf("在线用户缺少字段 %q —— 管理端对应的那一列会是空白", field)
		}
	}
}

// 没查到账号的行不该凭空出现空字符串键，让前端分不清"没这个人"和"字段没了"。
func TestAppOnlineUserOmitsUnresolvedIdentity(t *testing.T) {
	raw, err := json.Marshal(AppOnlineUser{AppID: 7, UserID: 4231, Connections: 1})
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	for _, field := range []string{"account", "nickname", "ip"} {
		if _, ok := decoded[field]; ok {
			t.Errorf("字段 %q 在没有值时不应出现（omitempty）", field)
		}
	}
	// connectedAt 是 time.Time，omitempty 对结构体无效，必须用 omitzero，
	// 否则零值会序列化成 "0001-01-01T00:00:00Z"，前端会把它当成一个真实时间显示出来。
	if value, ok := decoded["connectedAt"]; ok {
		t.Errorf("零值 connectedAt 不应出现，实际为 %v —— 检查 tag 是不是写成了 omitempty", value)
	}
}
