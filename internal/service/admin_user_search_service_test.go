package service

import (
	"sort"
	"testing"

	userdomain "aegis/internal/domain/user"
	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

func TestAdminUserSearchSupportsPinyinKeywordQueries(t *testing.T) {
	index, err := bleve.NewMemOnly(newAdminUserSearchMapping())
	if err != nil {
		t.Fatalf("create in-memory index: %v", err)
	}
	defer index.Close()

	documents := []userdomain.AdminUserSearchSource{
		{UserID: 1, AppID: 1001, Account: "alice", Nickname: "张三", Email: "alice@example.com", Phone: "13800000001"},
		{UserID: 2, AppID: 1001, Account: "bob", Nickname: "重庆", Email: "cq@example.com", Phone: "13800000002"},
	}
	for _, document := range documents {
		if err := index.Index(adminUserSearchDocID(document.AppID, document.UserID), buildAdminUserSearchDocument(document)); err != nil {
			t.Fatalf("index document %d: %v", document.UserID, err)
		}
	}

	testCases := []struct {
		name       string
		adminQuery userdomain.AdminUserQuery
		wantIDs    []int64
	}{
		{
			name:       "joined pinyin keyword",
			adminQuery: userdomain.AdminUserQuery{Keyword: "zhangsan", Page: 1, Limit: 10},
			wantIDs:    []int64{1},
		},
		{
			name:       "spaced pinyin keyword",
			adminQuery: userdomain.AdminUserQuery{Keyword: "zhang san", Page: 1, Limit: 10},
			wantIDs:    []int64{1},
		},
		{
			name:       "initials keyword",
			adminQuery: userdomain.AdminUserQuery{Keyword: "zs", Page: 1, Limit: 10},
			wantIDs:    []int64{1},
		},
		{
			name:       "polyphonic pinyin keyword",
			adminQuery: userdomain.AdminUserQuery{Keyword: "chongqing", Page: 1, Limit: 10},
			wantIDs:    []int64{2},
		},
		{
			name:       "field-specific nickname pinyin",
			adminQuery: userdomain.AdminUserQuery{Nickname: "zhang san", Page: 1, Limit: 10},
			wantIDs:    []int64{1},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			gotIDs := searchAdminUserIDs(t, index, 1001, testCase.adminQuery)
			if !equalInt64Slices(gotIDs, testCase.wantIDs) {
				t.Fatalf("search ids mismatch, got %v want %v", gotIDs, testCase.wantIDs)
			}
		})
	}
}

func TestBuildKeywordSearchQueryAvoidsWildcardQueries(t *testing.T) {
	query := buildKeywordSearchQuery("zhangsan")
	if containsWildcardQuery(query) {
		t.Fatalf("keyword query should not contain wildcard query: %#v", query)
	}
}

func searchAdminUserIDs(t *testing.T, index bleve.Index, appID int64, adminQuery userdomain.AdminUserQuery) []int64 {
	t.Helper()

	rootQuery := bleve.NewConjunctionQuery(buildAdminUserSearchQueries(appID, adminQuery)...)
	request := bleve.NewSearchRequestOptions(rootQuery, adminQuery.Limit, (adminQuery.Page-1)*adminQuery.Limit, false)
	request.SortBy([]string{"-_score", "-created_at", "-user_id"})

	result, err := index.Search(request)
	if err != nil {
		t.Fatalf("search index: %v", err)
	}

	ids := make([]int64, 0, len(result.Hits))
	for _, hit := range result.Hits {
		userID, ok := parseAdminUserSearchDocID(hit.ID)
		if ok {
			ids = append(ids, userID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func containsWildcardQuery(q blevequery.Query) bool {
	switch typed := q.(type) {
	case *blevequery.WildcardQuery:
		return true
	case *blevequery.DisjunctionQuery:
		for _, child := range typed.Disjuncts {
			if containsWildcardQuery(child) {
				return true
			}
		}
	case *blevequery.ConjunctionQuery:
		for _, child := range typed.Conjuncts {
			if containsWildcardQuery(child) {
				return true
			}
		}
	}
	return false
}

func equalInt64Slices(got []int64, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
