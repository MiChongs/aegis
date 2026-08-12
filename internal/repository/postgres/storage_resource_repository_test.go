package postgres

import (
	"strconv"
	"strings"
	"testing"
	"time"

	storagedomain "aegis/internal/domain/storage"
)

// 占位符编号是这段代码里最容易错、又最难在运行期发现的地方：
// 编号错位不会报语法错，只会拿错参数去比对，静默返回错误的结果集。
// 这里逐条钉住「条件文本 ↔ 参数序」的对应关系。

func TestBuildObjectWhereNumbersPlaceholdersInOrder(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	minSize, maxSize := int64(1024), int64(1<<20)
	configID, appID, uploadedBy := int64(7), int64(3), int64(42)

	var args []any
	where := buildObjectWhere(storagedomain.ObjectListQuery{
		ConfigID: &configID, AppID: &appID,
		Prefix: "avatars", Folder: "2026/08", FolderView: true,
		Keyword: "头像", ContentType: "image/", Status: "active",
		UploaderType: "user", UploadedBy: &uploadedBy,
		MinSize: &minSize, MaxSize: &maxSize,
		CreatedFrom: &from, CreatedTo: &to,
	}, &args, objectWhereOptions{IncludeStatus: true, DirectChildrenOnly: true})

	// 每个 $N 都必须落在 args 的范围内，且逐个递增出现
	for i := 1; i <= len(args); i++ {
		if !strings.Contains(where, "$"+strconv.Itoa(i)) {
			t.Fatalf("占位符 $%d 未出现在 WHERE 中：%s", i, where)
		}
	}
	if strings.Contains(where, "$"+strconv.Itoa(len(args)+1)) {
		t.Fatalf("WHERE 里引用了越界的占位符 $%d：%s", len(args)+1, where)
	}

	// keyword 用同一个占位符匹配两列，因此参数个数比条件个数少一个
	if got := strings.Count(where, "ILIKE"); got != 2 {
		t.Fatalf("关键字应同时匹配 file_name 与 object_key，得到 %d 处 ILIKE", got)
	}
	if !strings.Contains(where, "strpos(substr(object_key, length($") {
		t.Fatalf("目录浏览模式应限制为直接子文件：%s", where)
	}
}

// 重构前的实现只有 `content_type = $n` 精确匹配，而控制台的类型下拉发的是
// `image/` 这样的大类前缀 —— 于是「筛选图片」永远是空列表。
func TestBuildObjectWhereContentTypeFamilyUsesPrefix(t *testing.T) {
	var args []any
	where := buildObjectWhere(storagedomain.ObjectListQuery{ContentType: "image/"}, &args, objectWhereOptions{IncludeStatus: true})
	if !strings.Contains(where, "content_type LIKE $1") {
		t.Fatalf("大类应按前缀匹配：%s", where)
	}
	if len(args) != 1 || args[0] != "image/%" {
		t.Fatalf("前缀参数应为 image/%%，得到 %#v", args)
	}

	args = nil
	where = buildObjectWhere(storagedomain.ObjectListQuery{ContentType: "application/pdf"}, &args, objectWhereOptions{IncludeStatus: true})
	// 实际落库的类型可能带参数（text/plain; charset=utf-8），精确匹配要放过参数部分
	if !strings.Contains(where, "split_part(content_type, ';', 1) = $1") {
		t.Fatalf("完整类型应精确匹配且忽略参数部分：%s", where)
	}
}

// 目录名里一个 `%` 就能让前缀匹配退化成全表匹配 —— 点进 `2026%` 会列出所有文件
func TestEscapeLikePatternNeutralisesWildcards(t *testing.T) {
	if got := escapeLikePattern(`50%_off\x`); got != `50\%\_off\\x` {
		t.Fatalf("通配符转义结果不符：%s", got)
	}

	var args []any
	buildObjectWhere(storagedomain.ObjectListQuery{Folder: "2026%"}, &args, objectWhereOptions{IncludeStatus: true, DirectChildrenOnly: true})
	if len(args) < 1 || args[0] != `2026\%/%` {
		t.Fatalf("目录前缀未被转义：%#v", args)
	}
	// 第二个参数是给 length() 用的原始目录名，不能带转义
	if len(args) < 2 || args[1] != "2026%" {
		t.Fatalf("length() 用的目录名应保持原样：%#v", args)
	}
}

// 根目录下的直接子文件 = 对象键里根本没有斜杠
func TestBuildObjectWhereRootFolderView(t *testing.T) {
	var args []any
	where := buildObjectWhere(storagedomain.ObjectListQuery{}, &args, objectWhereOptions{IncludeStatus: true, DirectChildrenOnly: true})
	if !strings.Contains(where, "strpos(object_key, '/') = 0") {
		t.Fatalf("根目录应排除所有带斜杠的对象键：%s", where)
	}
}

// 汇总要同时说出「活跃多少 / 回收站多少」，带上 status 会让其中一档恒为 0
func TestBuildObjectWhereCanDropStatus(t *testing.T) {
	var args []any
	where := buildObjectWhere(storagedomain.ObjectListQuery{Status: "active"}, &args, objectWhereOptions{IncludeStatus: false})
	if strings.Contains(where, "status") {
		t.Fatalf("IncludeStatus=false 时不应出现状态条件：%s", where)
	}
	if len(args) != 0 {
		t.Fatalf("被忽略的条件不该占用参数位：%#v", args)
	}
}

func TestObjectOrderByWhitelistsColumns(t *testing.T) {
	cases := map[string]string{
		storagedomain.ObjectSortSize:      "size",
		storagedomain.ObjectSortFileName:  "file_name",
		storagedomain.ObjectSortObjectKey: "object_key",
		storagedomain.ObjectSortDeletedAt: "deleted_at",
		storagedomain.ObjectSortCreatedAt: "created_at",
		"id; DROP TABLE storage_objects":  "created_at", // 白名单外一律回落
	}
	for sort, column := range cases {
		got := objectOrderBy(sort, "asc")
		if !strings.HasPrefix(got, column+" ASC") {
			t.Fatalf("sort=%q 应排序在 %s 上，得到 %q", sort, column, got)
		}
		// 排序列有重复值时（size / created_at 都会），没有 tie-breaker 的分页
		// 会在翻页时重复或漏掉记录
		if !strings.HasSuffix(got, "id ASC") {
			t.Fatalf("排序必须以 id 兜底，得到 %q", got)
		}
	}
	if got := objectOrderBy("", "whatever"); !strings.Contains(got, "DESC") {
		t.Fatalf("非 asc 一律按降序，得到 %q", got)
	}
}

func TestNormalizeFolderPath(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"/":            "",
		"/2026/08/":    "2026/08",
		`2026\08`:      "2026/08",
		"  avatars/  ": "avatars",
		"a/b/c":        "a/b/c",
	}
	for input, want := range cases {
		if got := normalizeFolderPath(input); got != want {
			t.Fatalf("normalizeFolderPath(%q) = %q，期望 %q", input, got, want)
		}
	}
}
