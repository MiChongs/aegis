package receipt

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"aegis/pkg/fontkit"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// FontConfig 凭证字体来源。
//
// 拉丁字形永远来自内嵌的 Go 字体，不依赖任何外部文件 ——
// 这是「英文凭证在空容器里也出得来」的保证。这里配置的只是**中日韩字形**从哪来。
type FontConfig struct {
	// RegularPath / BoldPath 显式指定字体文件（.ttf 或 .ttc；.otf 不支持，见 fontkit）
	RegularPath string
	BoldPath    string
	// Dirs 额外的字体搜索目录，按顺序扫描
	Dirs []string
	// DisableSystemScan 关闭系统字体目录扫描。容器里字体是显式装进来的，
	// 关掉可以省去一轮无谓的目录遍历。
	DisableSystemScan bool
}

// fontSet 一次解析出的可用字型集合。
type fontSet struct {
	latin     *fontkit.Face // 恒不为 nil
	latinBold *fontkit.Face // 恒不为 nil
	cjk       *fontkit.Face // 可能为 nil：环境里没有中日韩字体
	cjkBold   *fontkit.Face // 可能为 nil：多数中文字体没有粗体伴侣

	// notes 解析过程中的诊断信息，启动时打一次日志，排查「中文没出来」时最有用
	notes []string
}

// hasCJK 是否具备中日韩字形能力。
func (fs *fontSet) hasCJK() bool { return fs.cjk != nil }

// describe 供日志与接口自检使用的一行描述。
func (fs *fontSet) describe() string {
	if fs.cjk == nil {
		return fmt.Sprintf("拉丁：%s（内嵌）；中日韩：不可用", fs.latin.Family)
	}
	bold := "无粗体伴侣（以字号与颜色区分层级）"
	if fs.cjkBold != nil {
		bold = "粗体：" + fs.cjkBold.Family
	}
	return fmt.Sprintf("拉丁：%s（内嵌）；中日韩：%s，%s", fs.latin.Family, fs.cjk, bold)
}

// loadFonts 解析可用字型。
//
// 拉丁字体解析失败才返回 error —— 那意味着内嵌资源本身坏了，属于构建事故。
// 找不到中日韩字体不是错误：英文凭证照常输出，中文凭证会在渲染时降级并如实上报。
func loadFonts(cfg FontConfig) (*fontSet, error) {
	set := &fontSet{}
	var err error
	if set.latin, err = fontkit.Load("embedded:go-regular", goregular.TTF, 0); err != nil {
		return nil, fmt.Errorf("receipt: 内嵌拉丁字体不可用：%w", err)
	}
	if set.latinBold, err = fontkit.Load("embedded:go-bold", gobold.TTF, 0); err != nil {
		return nil, fmt.Errorf("receipt: 内嵌拉丁粗体不可用：%w", err)
	}

	// 找到常规体之后最多再多解析几个文件去碰粗体伴侣。中日韩字体动辄十几兆，
	// 为了一个纯装饰性的粗体把系统里的字体全解析一遍不划算；
	// 没有粗体也不影响可读性 —— 层级改由字号与颜色表达。
	const boldSearchBudget = 4
	extraLoads := 0
	for _, path := range cjkCandidatePaths(cfg) {
		if note := eachFace(path, func(face *fontkit.Face) bool {
			if !face.SupportsHan() {
				return true
			}
			if face.IsBold() {
				if set.cjkBold == nil {
					set.cjkBold = face
				}
			} else if set.cjk == nil {
				set.cjk = face
			}
			return set.cjk == nil || set.cjkBold == nil
		}); note != "" {
			set.notes = append(set.notes, note)
		}
		if set.cjk != nil && set.cjkBold != nil {
			break
		}
		if set.cjk != nil {
			if extraLoads++; extraLoads > boldSearchBudget {
				break
			}
		}
	}
	if set.cjk == nil {
		set.notes = append(set.notes, "未找到可用的中日韩字体：中文 / 日文 / 韩文凭证将降级为英文。"+
			"请配置 PAYMENT_RECEIPT_FONT_PATH 指向一个 .ttf 或 .ttc 字体，或在镜像中安装 font-noto-cjk。")
	}
	return set, nil
}

// eachFace 逐个归一化字体文件里的字型，交给 visit 挑选；visit 返回 false 即停止。
//
// **逐个**而不是一次全展开，是因为归一化会为每个字型重建一份独立 sfnt：
// Noto Sans CJK 的 .ttc 里有 5 个字型（JP/KR/SC/TC/HK），各约二十兆，
// 全展开等于为了留下一个而先占掉上百兆内存。这里没被留下的字型当即成为垃圾。
//
// 失败不致命，但要留下**可执行的**诊断：.otf 不支持这件事必须说清楚，
// 否则运维会反复换不同的 .otf 重试。
func eachFace(path string, visit func(*fontkit.Face) bool) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ""
		}
		return fmt.Sprintf("读取字体 %s 失败：%v", path, err)
	}
	count := fontkit.FaceCount(raw)
	if count <= 0 {
		return fmt.Sprintf("字体 %s 不可用：无法识别的字体文件", path)
	}
	note := ""
	for i := range count {
		face, err := fontkit.Load(path, raw, i)
		if err != nil {
			if note == "" { // 同一个文件里的多个字型往往同因失败，报一次就够
				note = fmt.Sprintf("字体 %s 不可用：%v", path, err)
			}
			continue
		}
		if !visit(face) {
			break
		}
	}
	return note
}

// cjkCandidatePaths 按优先级列出中日韩字体的候选路径。
func cjkCandidatePaths(cfg FontConfig) []string {
	var paths []string
	add := func(candidates ...string) {
		for _, path := range candidates {
			path = strings.TrimSpace(path)
			if path != "" && !slices.Contains(paths, path) {
				paths = append(paths, path)
			}
		}
	}

	// 1. 显式配置：优先级最高，且是「系统里字体名字很怪」时唯一的出路
	add(cfg.RegularPath, cfg.BoldPath)
	// 2. 配置的目录 + 随镜像分发的目录：运维特意放进来的，不做文件名过滤，全部纳入候选
	for _, dir := range append(slices.Clone(cfg.Dirs), "assets/fonts", "data/fonts") {
		add(scanDir(dir, 2)...)
	}
	// 3. 系统字体目录：只收文件名像中日韩字体的。
	//    系统里动辄几百个字体，逐个解析（每个都要读全文件、建 cmap）会让启动明显变慢，
	//    而这些字体里绝大多数是纯拉丁的。名字不常规的字体请用显式配置指定。
	if !cfg.DisableSystemScan {
		for _, path := range systemFontPaths() {
			if cjkHintRank(path) < len(cjkFontHints) {
				add(path)
			}
		}
	}
	return paths
}

// scanDir 列出目录下的候选字体文件（含至多 depth 层子目录），按「像不像中日韩字体」排序。
func scanDir(dir string, depth int) []string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	var found []string
	root := filepath.Clean(dir)
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // 目录不存在是常态（多数部署没有 assets/fonts），不该中断扫描
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if strings.Count(strings.TrimPrefix(path, root), string(filepath.Separator)) > depth {
				return filepath.SkipDir
			}
			return nil
		}
		if isFontFile(entry.Name()) {
			found = append(found, path)
		}
		return nil
	})
	sortByCJKLikelihood(found)
	return found
}

func isFontFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ttf", ".ttc":
		return true
	default:
		// .otf 是 CFF 轮廓，PDF 引擎读不了；这里不收，交由 fontkit 在显式配置时明确报错
		return false
	}
}

// cjkFontHints 常见中日韩字体的文件名特征。命中的文件排在前面，
// 避免为了找一个中文字体把系统里几百个字体全解析一遍。
var cjkFontHints = []string{
	"notosanscjk", "notoserifcjk", "notosanssc", "notosanstc", "notosansjp", "notosanskr",
	"sourcehansans", "sourcehanserif", "sarasa", "lxgw", "msyh", "msjh",
	"simhei", "simsun", "simkai", "simfang", "deng", "fangsong", "kaiti", "youyuan",
	"pingfang", "hiragino", "stheiti", "songti",
	"wqy", "zenhei", "microhei", "droidsansfallback", "arphic", "ukai", "uming",
	"malgun", "gulim", "batang", "nanumgothic", "meiryo", "yugoth", "msgothic", "msmincho",
	"unifont", "hanazono", "cjk",
}

// sortByCJKLikelihood 越像中日韩字体越靠前；同等条件下常规体优先于粗体 ——
// 先撞上粗体会让它被当成正文字体，整份凭证排成粗的。
func sortByCJKLikelihood(paths []string) {
	sort.SliceStable(paths, func(i, j int) bool {
		ri, rj := cjkHintRank(paths[i]), cjkHintRank(paths[j])
		if ri != rj {
			return ri < rj
		}
		return !looksBold(paths[i]) && looksBold(paths[j])
	})
}

func looksBold(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return strings.Contains(name, "bold") || strings.Contains(name, "bd.") || strings.Contains(name, "-bd")
}

func cjkHintRank(path string) int {
	name := strings.ToLower(filepath.Base(path))
	name = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(name)
	for rank, hint := range cjkFontHints {
		if strings.Contains(name, hint) {
			return rank
		}
	}
	return len(cjkFontHints)
}

// systemFontPaths 各平台的系统字体位置。
//
// 返回的是**候选**，调用方会按文件名特征过滤后才真正解析（见 cjkCandidatePaths）。
// Linux 上各发行版的字体布局差异很大（Alpine 的 font-noto-cjk 落在
// /usr/share/fonts/noto/，Debian 在 /usr/share/fonts/opentype/noto/），
// 因此这里递归整个字体根目录，而不是维护一张永远追不全的具名文件表。
func systemFontPaths() []string {
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		dir := filepath.Join(root, "Fonts")
		// 常规体在前、粗体在后：解析到常规体就基本可以开工了，
		// 顺序反了会让整份凭证用粗体当正文。
		return []string{
			filepath.Join(dir, "msyh.ttc"), // 微软雅黑
			filepath.Join(dir, "msyh.ttf"),
			filepath.Join(dir, "simhei.ttf"), // 黑体
			filepath.Join(dir, "simsun.ttc"), // 宋体
			filepath.Join(dir, "Deng.ttf"),   // 等线
			filepath.Join(dir, "msjh.ttc"),   // 微軟正黑體
			filepath.Join(dir, "meiryo.ttc"), // メイリオ
			filepath.Join(dir, "malgun.ttf"), // 맑은 고딕
			filepath.Join(dir, "msyhbd.ttc"), // 微软雅黑 粗体（作为粗体伴侣）
			filepath.Join(dir, "msjhbd.ttc"), // 微軟正黑體 粗體
			filepath.Join(dir, "malgunbd.ttf"),
		}
	case "darwin":
		return []string{
			"/System/Library/Fonts/PingFang.ttc",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
			"/System/Library/Fonts/STHeiti Light.ttc",
			"/Library/Fonts/Arial Unicode.ttf",
		}
	default: // linux 及其它类 unix
		var paths []string
		for _, dir := range []string{"/usr/share/fonts", "/usr/local/share/fonts", "/usr/share/fonts/truetype"} {
			paths = append(paths, scanDir(dir, 3)...)
		}
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, scanDir(filepath.Join(home, ".fonts"), 2)...)
			paths = append(paths, scanDir(filepath.Join(home, ".local", "share", "fonts"), 2)...)
		}
		return paths
	}
}
