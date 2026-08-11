package fontkit

import (
	"encoding/binary"
	"os"
	"runtime"
	"testing"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// 内嵌的 Go 字体必须能被 PDF 引擎直接加载 —— 这是「英文收据在任何环境下都出得来」的底线，
// 它一旦失效，没装任何字体的容器里连英文都渲染不出。
func TestEmbeddedGoFontLoadsIntoPDFEngine(t *testing.T) {
	for name, raw := range map[string][]byte{"go-regular": goregular.TTF, "go-bold": gobold.TTF} {
		face, err := Load("embedded:"+name, raw, 0)
		if err != nil {
			t.Fatalf("%s 归一化失败：%v", name, err)
		}
		if missing := face.Missing("Receipt Total 1,234.56 №"); len(missing) > 0 {
			t.Errorf("%s 缺少拉丁字符：%q", name, string(missing))
		}
		if err := loadIntoGoPDF(face); err != nil {
			t.Fatalf("%s 无法被 gopdf 加载：%v", name, err)
		}
	}
}

// 拉丁字体不该被误判成能画中文 —— 误判会让渲染器选错字体，产出一份缺字的凭证。
func TestLatinFaceIsNotTreatedAsCJK(t *testing.T) {
	face, err := Load("embedded:go-regular", goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	if face.SupportsHan() || face.SupportsCJK() {
		t.Fatal("Go 字体被误判为支持汉字")
	}
	if missing := face.Missing("电子收据"); len(missing) != 4 {
		t.Fatalf("期望 4 个缺失汉字，实得 %q", string(missing))
	}
}

func TestCFFOutlinesRejectedWithActionableError(t *testing.T) {
	raw := make([]byte, 64)
	copy(raw, "OTTO")
	if _, err := Load("fake.otf", raw, 0); err == nil {
		t.Fatal("CFF 字体应当被拒绝")
	} else if !contains(err.Error(), ".ttf") {
		t.Fatalf("错误信息应当指出改用 .ttf/.ttc，实得：%v", err)
	}
}

func TestAppleTrueTypeTagRewritten(t *testing.T) {
	raw := make([]byte, len(goregular.TTF))
	copy(raw, goregular.TTF)
	copy(raw[:4], tagAppleTrueType)
	face, err := Load("apple.ttf", raw, 0)
	if err != nil {
		t.Fatalf("Apple 'true' 版本号应被改写后正常加载：%v", err)
	}
	if got := binary.BigEndian.Uint32(face.Data[:4]); got != versionTrueType {
		t.Fatalf("版本号未改写：%#x", got)
	}
}

// TTC 拆分是本包存在的理由：系统上的中文字体几乎全是 .ttc，
// 拆不出来就等于「装了中文字体也渲染不出中文」。
func TestCollectionFaceExtractionRendersCJK(t *testing.T) {
	path, raw := findSystemCollection(t)
	if raw == nil {
		t.Skipf("当前平台（%s）未找到可用的 TTC 字体，跳过", runtime.GOOS)
	}
	if got := FaceCount(raw); got < 1 {
		t.Fatalf("%s 的字型数解析异常：%d", path, got)
	}
	faces, err := LoadAll(path, raw)
	if err != nil {
		t.Fatalf("%s 拆分失败：%v", path, err)
	}
	var han *Face
	for _, face := range faces {
		if face.SupportsHan() {
			han = face
			break
		}
	}
	if han == nil {
		t.Skipf("%s 内没有覆盖汉字的字型", path)
	}
	if err := loadIntoGoPDF(han); err != nil {
		t.Fatalf("从 %s 拆出的字型无法被 gopdf 加载：%v", path, err)
	}
	if missing := han.Missing("电子收据 合计 人民币"); len(missing) > 0 {
		t.Fatalf("拆出的中文字型仍缺字：%q", string(missing))
	}
	t.Logf("已从 %s 拆出可用中文字型：%s", path, han)
}

// loadIntoGoPDF 用真实的 PDF 引擎验证归一化结果，而不是只看字节头 ——
// 「字节头对了但 gopdf 仍拒绝」正是这个包要防的情况。
func loadIntoGoPDF(face *Face) error {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFontData("probe", face.Data); err != nil {
		return err
	}
	if err := pdf.SetFont("probe", "", 12); err != nil {
		return err
	}
	pdf.SetXY(40, 40)
	if err := pdf.Cell(nil, "probe"); err != nil {
		return err
	}
	_, err := pdf.GetBytesPdfReturnErr()
	return err
}

func findSystemCollection(t *testing.T) (string, []byte) {
	t.Helper()
	candidates := []string{
		`C:\Windows\Fonts\msyh.ttc`,
		`C:\Windows\Fonts\simsun.ttc`,
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		"/System/Library/Fonts/PingFang.ttc",
	}
	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err == nil && len(raw) > 12 && string(raw[:4]) == tagTrueTypeCollection {
			return path, raw
		}
	}
	return "", nil
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
