package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// 签到留痕字段的截断长度必须与 daily_signins 的实际列宽一致。
//
// 两个方向都会出事：常量比列宽大，超长的 UA 又会让整个签到事务以 22001 失败并回滚
// 积分与连签天数；常量比列宽小，则是在没有必要的情况下把留痕截短。
// 迁移文件是列宽的唯一事实来源，所以这里直接读它，而不是把数字再抄一遍。
func TestSignInColumnLimitsMatchSchema(t *testing.T) {
	root := filepath.Join("..", "..", "..", "migrations", "postgres")
	create := readMigration(t, filepath.Join(root, "000003_add_signin_and_points.up.sql"))
	widen := readMigration(t, filepath.Join(root, "000071_widen_daily_signins_device_info.up.sql"))

	cases := []struct {
		column   string
		constant int
		schema   string
	}{
		{"sign_in_source", signInSourceMaxLen, create},
		{"ip_address", signInIPMaxLen, create},
		{"location", signInLocationMaxLen, create},
		{"bonus_type", signInBonusTypeMaxLen, create},
		{"bonus_description", signInBonusDescMaxLen, create},
		// device_info 建表时是 128，装不下任何一个现代客户端的 UA，由 000071 放宽。
		{"device_info", signInDeviceInfoMaxLen, widen},
	}

	for _, tc := range cases {
		width := columnWidth(t, tc.schema, tc.column)
		if width != tc.constant {
			t.Errorf("%s: 迁移里是 VARCHAR(%d)，代码里的截断上限是 %d", tc.column, width, tc.constant)
		}
	}
}

// truncateColumn 按码点截断：VARCHAR(n) 数的是字符而不是字节，
// 按字节切还会把一个多字节字符劈成两半，写进去就是乱码。
func TestTruncateColumnClipsByRune(t *testing.T) {
	const limit = 4
	if got := truncateColumn("中文测试内容", limit); got != "中文测试" {
		t.Fatalf("应当按字符截断，实际 %q", got)
	}
	if got := truncateColumn("abc", limit); got != "abc" {
		t.Fatalf("未超限时应当原样返回，实际 %q", got)
	}
	if got := truncateColumn("", limit); got != "" {
		t.Fatalf("空串应当原样返回，实际 %q", got)
	}
}

func readMigration(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取迁移文件失败：%v", err)
	}
	return string(content)
}

func columnWidth(t *testing.T, schema string, column string) int {
	t.Helper()
	pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(column) + `\s+(?:TYPE\s+)?VARCHAR\((\d+)\)`)
	match := pattern.FindStringSubmatch(schema)
	if match == nil {
		t.Fatalf("迁移里找不到 %s 的列宽声明", column)
	}
	width, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("%s 的列宽解析失败：%v", column, err)
	}
	return width
}
