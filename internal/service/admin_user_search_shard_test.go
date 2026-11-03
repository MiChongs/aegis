package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	userdomain "aegis/internal/domain/user"
	"github.com/blevesearch/bleve/v2"
	"go.uber.org/zap"
)

// 分片并行检索与单一索引给出同样的命中集合与总数，分页拼起来不重不漏。
func TestAdminUserSearchShardsMatchSingleIndex(t *testing.T) {
	single, err := bleve.NewMemOnly(newAdminUserSearchMapping())
	if err != nil {
		t.Fatal(err)
	}
	defer single.Close()
	shards := make([]bleve.Index, 4)
	for i := range shards {
		if shards[i], err = bleve.NewMemOnly(newAdminUserSearchMapping()); err != nil {
			t.Fatal(err)
		}
	}
	svc := &AdminUserSearchService{log: zap.NewNop(), shards: shards, index: bleve.NewIndexAlias(shards...), buildWorkers: 3, batchSize: 50}
	defer svc.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := make([]userdomain.AdminUserSearchSource, 0, 120)
	for i := 1; i <= 120; i++ {
		nickname := "路人"
		if i%3 == 0 {
			nickname = "张三"
		}
		items = append(items, userdomain.AdminUserSearchSource{
			UserID: int64(i), AppID: 7, Account: fmt.Sprintf("user%03d", i), Nickname: nickname, Enabled: true,
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	if err := svc.indexPage(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if err := single.Index(adminUserSearchDocID(item.AppID, item.UserID), buildAdminUserSearchDocument(item)); err != nil {
			t.Fatal(err)
		}
	}

	for _, keyword := range []string{"zhangsan", "zs", "user1", "路人"} {
		query := userdomain.AdminUserQuery{Keyword: keyword, Page: 1, Limit: 500}
		want := searchAdminUserIDs(t, single, 7, query)
		got := searchAdminUserIDs(t, svc.index, 7, query)
		if len(want) == 0 || !sameIDSet(want, got) {
			t.Fatalf("keyword %q: sharded %d hits, single %d hits", keyword, len(got), len(want))
		}
		// 分页拼回完整集合
		var paged []int64
		for page := 1; ; page++ {
			ids := searchAdminUserIDs(t, svc.index, 7, userdomain.AdminUserQuery{Keyword: keyword, Page: page, Limit: 7})
			if len(ids) == 0 {
				break
			}
			paged = append(paged, ids...)
		}
		if len(paged) != len(want) || !sameIDSet(paged, want) {
			t.Fatalf("keyword %q: paging produced %d ids, want %d", keyword, len(paged), len(want))
		}
	}
}

func TestAdminUserSearchCacheInvalidatesOnWrite(t *testing.T) {
	svc := &AdminUserSearchService{log: zap.NewNop()}
	key := adminUserSearchCacheKey(7, userdomain.AdminUserQuery{Keyword: "a", Page: 1, Limit: 20})
	gen := svc.generation(7).Load()
	svc.remember(key, gen, []int64{1, 2}, 2)
	if ids, total, ok := svc.cached(key, gen); !ok || total != 2 || len(ids) != 2 {
		t.Fatal("expected cache hit")
	}
	svc.invalidate(7)
	if _, _, ok := svc.cached(key, svc.generation(7).Load()); ok {
		t.Fatal("cache should be invalid after a write")
	}
}

func sameIDSet(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[int64]int, len(a))
	for _, id := range a {
		seen[id]++
	}
	for _, id := range b {
		if seen[id] == 0 {
			return false
		}
		seen[id]--
	}
	return true
}
