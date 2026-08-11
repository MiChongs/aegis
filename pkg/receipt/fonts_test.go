package receipt

import (
	"os"
	"path/filepath"
	"testing"
)

// 各发行版把中日韩字体装在完全不同的位置与文件名下。
// 系统字体扫描按文件名特征过滤（不这样做就得逐个解析几百个字体），
// 因此这张特征表漏了哪个发行版，那个发行版上的中文凭证就会静默降级成英文。
func TestCJKHintsCoverCommonDistributions(t *testing.T) {
	shouldMatch := []string{
		// Alpine：font-noto-cjk（Docker 镜像装的就是它）
		"/usr/share/fonts/noto/NotoSansCJK-Regular.ttc",
		// Debian / Ubuntu：fonts-noto-cjk
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		// 其它常见发行版与手工安装
		"/usr/share/fonts/adobe-source-han-sans/SourceHanSansSC-Regular.ttf",
		"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
		"/usr/share/fonts/truetype/arphic/uming.ttc",
		"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
		"/usr/share/fonts/sarasa/Sarasa-Regular.ttc",
		"/usr/share/fonts/lxgw/LXGWWenKai-Regular.ttf",
		// Windows
		`C:\Windows\Fonts\msyh.ttc`,
		`C:\Windows\Fonts\msyhbd.ttc`,
		`C:\Windows\Fonts\msjh.ttc`,
		`C:\Windows\Fonts\simhei.ttf`,
		`C:\Windows\Fonts\simsun.ttc`,
		`C:\Windows\Fonts\Deng.ttf`,
		`C:\Windows\Fonts\meiryo.ttc`,
		`C:\Windows\Fonts\malgun.ttf`,
		// macOS
		"/System/Library/Fonts/PingFang.ttc",
		"/System/Library/Fonts/Hiragino Sans GB.ttc",
	}
	for _, path := range shouldMatch {
		if cjkHintRank(path) >= len(cjkFontHints) {
			t.Errorf("%s 未被识别为中日韩字体，该环境的中文凭证会降级成英文", path)
		}
	}

	// 纯拉丁字体不该被当成中日韩候选：白白解析几十兆只为发现它画不出汉字
	for _, path := range []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
		`C:\Windows\Fonts\arial.ttf`,
		`C:\Windows\Fonts\segoeui.ttf`,
	} {
		if cjkHintRank(path) < len(cjkFontHints) {
			t.Errorf("%s 被误判为中日韩字体", path)
		}
	}
}

// .otf 是 CFF 轮廓，PDF 引擎嵌不进去，因此扫描阶段就不收。
func TestFontFileExtensions(t *testing.T) {
	for _, name := range []string{"a.ttf", "a.TTF", "a.ttc", "a.TTC"} {
		if !isFontFile(name) {
			t.Errorf("%s 应当被接受", name)
		}
	}
	for _, name := range []string{"a.otf", "a.woff2", "a.pfb", "a.txt", "a"} {
		if isFontFile(name) {
			t.Errorf("%s 不该被接受", name)
		}
	}
}

// 目录扫描里常规体必须排在粗体前面：先撞上粗体会让整份凭证用粗体当正文。
func TestDirectoryScanPrefersRegularOverBold(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"NotoSansCJK-Bold.ttc", "NotoSansCJK-Regular.ttc", "DejaVuSans.ttf"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stub"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths := scanDir(dir, 1)
	if len(paths) != 3 {
		t.Fatalf("扫描结果数量异常：%v", paths)
	}
	if filepath.Base(paths[0]) != "NotoSansCJK-Regular.ttc" {
		t.Errorf("常规体应排在最前，实得 %v", paths)
	}
	if filepath.Base(paths[1]) != "NotoSansCJK-Bold.ttc" {
		t.Errorf("粗体应排第二，实得 %v", paths)
	}
}

// 配置的目录不存在是常态（多数部署没有 assets/fonts），不该报错也不该中断扫描。
func TestScanDirToleratesMissingDirectory(t *testing.T) {
	if got := scanDir(filepath.Join(t.TempDir(), "nope"), 1); len(got) != 0 {
		t.Errorf("不存在的目录应返回空，实得 %v", got)
	}
	if got := scanDir("", 1); len(got) != 0 {
		t.Errorf("空目录名应返回空，实得 %v", got)
	}
}

// 显式配置的字体优先于系统扫描 —— 它是「系统里字体名字很怪」时唯一的出路。
func TestExplicitFontPathTakesPrecedence(t *testing.T) {
	paths := cjkCandidatePaths(FontConfig{
		RegularPath:       "/opt/fonts/custom-regular.ttf",
		BoldPath:          "/opt/fonts/custom-bold.ttf",
		DisableSystemScan: true,
	})
	if len(paths) < 2 || paths[0] != "/opt/fonts/custom-regular.ttf" || paths[1] != "/opt/fonts/custom-bold.ttf" {
		t.Fatalf("显式配置未排在最前：%v", paths)
	}
}
