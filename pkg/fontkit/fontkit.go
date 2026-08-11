// Package fontkit 把真实世界的字体文件归一化成 PDF 引擎能直接加载的独立 sfnt，
// 并回答「这个字体能不能画出这段文字」。
//
// 存在的理由很具体：gopdf 的 TTF 解析器只接受 sfnt 版本 0x00010000 的**独立**字体，
// 而系统上的中日韩字体几乎清一色是 TrueType Collection（Windows 的 msyh.ttc /
// simsun.ttc、Linux 的 NotoSansCJK-Regular.ttc、macOS 的 PingFang.ttc）或 CFF 轮廓的
// OpenType（NotoSansCJKsc-Regular.otf）。把这些文件原样喂给 gopdf 一律返回
// 「Incorrect magic number」并被静默跳过 —— 这正是「明明装了中文字体，导出的 PDF
// 里中文还是没有」的根因。
//
// 因此本包做两件事：
//
//  1. 归一化（Load）：ttcf 拆出指定 face 重建为独立 sfnt；Apple 的 'true' 版本号改写；
//     CFF/OTTO 明确报错而不是让调用方以为「这个字体不能用」。
//  2. 覆盖度（Missing）：用 x/image/font/sfnt 查 cmap，让调用方能在渲染**之前**
//     知道哪些字符会变成豆腐块，从而换字体或降级语言，而不是产出一份缺字的凭证。
package fontkit

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"

	"golang.org/x/image/font/sfnt"
)

// sfnt 版本标记
const (
	tagTrueTypeCollection = "ttcf"      // TrueType Collection
	tagOpenTypeCFF        = "OTTO"      // CFF（PostScript）轮廓，glyf 不存在
	tagAppleTrueType      = "true"      // Apple 旧式 TrueType，结构与 0x00010000 相同
	versionTrueType       = 0x0001_0000 // 标准 TrueType
	checkSumMagic         = 0xB1B0_AFBA // head.checkSumAdjustment 的规定常量
	headCheckSumAdjOffset = 8           // head 表内 checkSumAdjustment 的偏移
)

var (
	// ErrNotFont 数据不是可识别的 sfnt 字体
	ErrNotFont = errors.New("fontkit: 不是可识别的字体文件")
	// ErrCFFOutlines CFF/PostScript 轮廓字体。这类字体无法转成 TrueType glyf 轮廓，
	// 只能换一个 .ttf/.ttc 字体文件 —— 报错时要把这句话带给运维，否则对方会反复重试同一个 .otf。
	ErrCFFOutlines = errors.New("fontkit: 该字体是 CFF/OpenType 轮廓（.otf），PDF 引擎只支持 TrueType 轮廓，请改用 .ttf 或 .ttc")
	// ErrFaceIndexRange TTC 中不存在该序号的 face
	ErrFaceIndexRange = errors.New("fontkit: 字体集合中不存在该序号的字型")
)

// Face 一个已归一化、可直接交给 PDF 引擎的字型。
type Face struct {
	// Data 独立 sfnt 字节流，version 恒为 0x00010000
	Data []byte
	// Family 字体族名（取自 name 表），用于日志与诊断
	Family string
	// Subfamily 字型名（Regular / Bold / Light …），用于挑选粗体伴侣字型
	Subfamily string
	// Source 来源描述（文件路径或 "embedded:xxx"），出现在诊断信息里
	Source string
	// Index 在 TTC 中的序号；独立字体恒为 0
	Index int

	font *sfnt.Font
	bold bool
	mu   sync.Mutex
	buf  sfnt.Buffer
	// cover 记忆化的字符覆盖结果。收据里同一个字符会反复出现（金额里的数字、
	// 中文里的「元」），每次都走 cmap 二分查找是白费。
	cover map[rune]bool
}

// FaceCount 返回字体文件里包含几个字型：独立字体为 1，TTC 为其 face 数。
func FaceCount(raw []byte) int {
	if len(raw) < 12 {
		return 0
	}
	if string(raw[:4]) == tagTrueTypeCollection {
		return int(binary.BigEndian.Uint32(raw[8:12]))
	}
	return 1
}

// Load 归一化字体数据。index 为 TTC 内的字型序号，独立字体传 0。
//
// source 只用于诊断信息，可以是文件路径也可以是 "embedded:go-regular" 这样的标识。
func Load(source string, raw []byte, index int) (*Face, error) {
	if len(raw) < 12 {
		return nil, fmt.Errorf("%w：%s", ErrNotFont, source)
	}
	data, err := normalize(raw, index)
	if err != nil {
		return nil, fmt.Errorf("%w（%s）", err, source)
	}
	parsed, err := sfnt.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("fontkit: 解析字型失败（%s#%d）：%w", source, index, err)
	}
	face := &Face{Data: data, Source: source, Index: index, font: parsed, cover: make(map[rune]bool, 256)}
	// 名称优先取英文记录：x/image 的 Name() 返回的是「name 表里碰到的第一条」，
	// Windows 自带字体常把西班牙语记录排在前面，微软雅黑粗体会读成 "Negreta"。
	// 这些字符串会进日志与运维自检页面，读成外语等于没有诊断价值。
	face.Family = firstNonBlank(englishName(data, nameIDFamily), nameOrEmpty(parsed, &face.buf, sfnt.NameIDFamily), source)
	face.Subfamily = firstNonBlank(englishName(data, nameIDSubfamily), nameOrEmpty(parsed, &face.buf, sfnt.NameIDSubfamily))
	face.bold = readMacStyleBold(data)
	return face, nil
}

// IsBold 该字型是否为粗体。
//
// 判据是 head.macStyle 的粗体位，不是名称 —— 名称是本地化的，
// 靠字符串匹配会在非英语系统上把粗体认成常规体，进而把整份凭证排成粗的。
func (f *Face) IsBold() bool {
	if f == nil {
		return false
	}
	if f.bold {
		return true
	}
	// macStyle 位缺失的字体（部分裁剪过的子集字体）退回名称判定
	name := strings.ToLower(f.Subfamily)
	return strings.Contains(name, "bold") || strings.Contains(name, "heavy") || strings.Contains(name, "black")
}

func nameOrEmpty(font *sfnt.Font, buf *sfnt.Buffer, id sfnt.NameID) string {
	name, err := font.Name(buf, id)
	if err != nil {
		return ""
	}
	return name
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// LoadAll 把一个字体文件里的全部字型都归一化出来。
// TTC 里各 face 的字符覆盖可能完全不同（msyh.ttc 里既有微软雅黑也有 Microsoft YaHei UI），
// 让调用方按覆盖度挑，比武断地取 face 0 稳妥。
func LoadAll(source string, raw []byte) ([]*Face, error) {
	count := FaceCount(raw)
	if count <= 0 {
		return nil, fmt.Errorf("%w：%s", ErrNotFont, source)
	}
	faces := make([]*Face, 0, count)
	var firstErr error
	for i := range count {
		face, err := Load(source, raw, i)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		faces = append(faces, face)
	}
	if len(faces) == 0 {
		return nil, firstErr
	}
	return faces, nil
}

// Covers 该字型是否能画出这个字符。
func (f *Face) Covers(r rune) bool {
	if f == nil || f.font == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if hit, ok := f.cover[r]; ok {
		return hit
	}
	idx, err := f.font.GlyphIndex(&f.buf, r)
	hit := err == nil && idx != 0
	f.cover[r] = hit
	return hit
}

// Missing 返回这段文字里该字型画不出来的字符（去重、有序）。
// 空白与控制字符不计入 —— 它们在 PDF 里由排版而非字形承担。
func (f *Face) Missing(texts ...string) []rune {
	seen := make(map[rune]struct{})
	var missing []rune
	for _, text := range texts {
		for _, r := range text {
			if r == '\n' || r == '\r' || r == '\t' || unicode.IsSpace(r) || !unicode.IsPrint(r) {
				continue
			}
			if _, ok := seen[r]; ok {
				continue
			}
			seen[r] = struct{}{}
			if !f.Covers(r) {
				missing = append(missing, r)
			}
		}
	}
	slices.Sort(missing)
	return missing
}

// CoverageOf 该字型对这段文字的覆盖率（0~1）。用于在多个候选字体间择优。
func (f *Face) CoverageOf(texts ...string) float64 {
	seen := make(map[rune]struct{})
	total, hit := 0, 0
	for _, text := range texts {
		for _, r := range text {
			if unicode.IsSpace(r) || !unicode.IsPrint(r) {
				continue
			}
			if _, ok := seen[r]; ok {
				continue
			}
			seen[r] = struct{}{}
			total++
			if f.Covers(r) {
				hit++
			}
		}
	}
	if total == 0 {
		return 1
	}
	return float64(hit) / float64(total)
}

// cjkProbe 判定「是不是一个中日韩字体」的探针字符：
// 简体常用字、繁体、日文假名、韩文谚文各取一个，全中才算真正覆盖 CJK。
var cjkProbe = []rune{'中', '文', '電', 'あ', '한'}

// SupportsCJK 该字型是否覆盖中日韩文字。
func (f *Face) SupportsCJK() bool {
	for _, r := range cjkProbe {
		if !f.Covers(r) {
			return false
		}
	}
	return true
}

// SupportsHan 该字型是否覆盖汉字（不要求同时覆盖假名与谚文）。
// 中文收据只需要这个条件；simhei/simsun 这类纯中文字体没有谚文，
// 用 SupportsCJK 判会把它们误判成不可用。
func (f *Face) SupportsHan() bool {
	return f.Covers('中') && f.Covers('文') && f.Covers('电')
}

// String 诊断用描述
func (f *Face) String() string {
	if f == nil {
		return "<nil face>"
	}
	if f.Index > 0 {
		return fmt.Sprintf("%s（%s#%d）", f.Family, f.Source, f.Index)
	}
	return fmt.Sprintf("%s（%s）", f.Family, f.Source)
}

// normalize 把任意 sfnt 容器转成 PDF 引擎接受的独立 TrueType 字节流。
func normalize(raw []byte, index int) ([]byte, error) {
	switch tag := string(raw[:4]); {
	case binary.BigEndian.Uint32(raw[:4]) == versionTrueType:
		if index != 0 {
			return nil, ErrFaceIndexRange
		}
		return raw, nil
	case tag == tagAppleTrueType:
		// Apple 的 'true' 与 0x00010000 结构完全一致，只是版本标记不同。
		// 改写四个字节即可，不必重建整张表目录。
		if index != 0 {
			return nil, ErrFaceIndexRange
		}
		out := make([]byte, len(raw))
		copy(out, raw)
		binary.BigEndian.PutUint32(out[:4], versionTrueType)
		return out, nil
	case tag == tagOpenTypeCFF:
		return nil, ErrCFFOutlines
	case tag == tagTrueTypeCollection:
		return extractCollectionFace(raw, index)
	default:
		return nil, ErrNotFont
	}
}

// extractCollectionFace 从 TTC 中抽出第 index 个字型，重建成独立 sfnt。
//
// TTC 的结构是「一份表目录 per face，共享底下的表数据」。重建就是把该 face 的表记录
// 逐条抄出来，把表数据按 4 字节对齐依次追加到新文件里，同时改写记录中的偏移量。
func extractCollectionFace(raw []byte, index int) ([]byte, error) {
	if len(raw) < 12 {
		return nil, ErrNotFont
	}
	count := int(binary.BigEndian.Uint32(raw[8:12]))
	if index < 0 || index >= count {
		return nil, ErrFaceIndexRange
	}
	dirPos := 12 + 4*index
	if len(raw) < dirPos+4 {
		return nil, ErrNotFont
	}
	dirOff := int(binary.BigEndian.Uint32(raw[dirPos : dirPos+4]))
	if dirOff < 0 || len(raw) < dirOff+12 {
		return nil, ErrNotFont
	}
	numTables := int(binary.BigEndian.Uint16(raw[dirOff+4 : dirOff+6]))
	if numTables <= 0 || len(raw) < dirOff+12+16*numTables {
		return nil, ErrNotFont
	}

	// 表目录头（12 字节）原样沿用：numTables 不变，searchRange 等派生字段也就依然正确。
	headerLen := 12 + 16*numTables
	out := make([]byte, headerLen, headerLen+len(raw)/2)
	copy(out, raw[dirOff:dirOff+headerLen])
	binary.BigEndian.PutUint32(out[:4], versionTrueType)

	headRecord := -1
	cursor := headerLen
	for i := range numTables {
		rec := raw[dirOff+12+16*i : dirOff+12+16*(i+1)]
		tableOff := int(binary.BigEndian.Uint32(rec[8:12]))
		tableLen := int(binary.BigEndian.Uint32(rec[12:16]))
		if tableOff < 0 || tableLen < 0 || len(raw) < tableOff+tableLen {
			return nil, fmt.Errorf("%w：字型 #%d 的 %q 表越界", ErrNotFont, index, strings.TrimRight(string(rec[:4]), "\x00"))
		}
		if string(rec[:4]) == "head" {
			headRecord = cursor
		}
		binary.BigEndian.PutUint32(out[12+16*i+8:], uint32(cursor))
		out = append(out, raw[tableOff:tableOff+tableLen]...)
		if pad := (4 - tableLen%4) % 4; pad > 0 {
			out = append(out, make([]byte, pad)...)
		}
		cursor += tableLen + (4-tableLen%4)%4
	}

	// head.checkSumAdjustment 是对**整个文件**的校验，重建后必然失效。
	// 多数渲染器不校验，但留着一个错的值会让 fontTools 之类的工具报警，
	// 排查字体问题时平添噪音，所以按规范重算。
	if headRecord >= 0 && len(out) >= headRecord+headCheckSumAdjOffset+4 {
		adjPos := headRecord + headCheckSumAdjOffset
		binary.BigEndian.PutUint32(out[adjPos:], 0)
		binary.BigEndian.PutUint32(out[adjPos:], checkSumMagic-sfntCheckSum(out))
	}
	return out, nil
}

// ── name / head 表的直接解析 ──
//
// 这两处刻意不走 x/image/font/sfnt：它的 Name() 不区分语言，Windows 自带字体
// 常把非英语记录排在前面；macStyle 位它则完全没有暴露。字体诊断信息与粗体判定
// 都要求语言无关，只能自己读表。

const (
	nameIDFamily    = 1
	nameIDSubfamily = 2
)

// findTable 在已归一化的独立 sfnt 中定位一张表。
func findTable(data []byte, tag string) []byte {
	if len(data) < 12 || len(tag) != 4 {
		return nil
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	if len(data) < 12+16*numTables {
		return nil
	}
	for i := range numTables {
		rec := data[12+16*i : 12+16*(i+1)]
		if string(rec[:4]) != tag {
			continue
		}
		off := int(binary.BigEndian.Uint32(rec[8:12]))
		length := int(binary.BigEndian.Uint32(rec[12:16]))
		if off < 0 || length < 0 || len(data) < off+length {
			return nil
		}
		return data[off : off+length]
	}
	return nil
}

// readMacStyleBold 读 head.macStyle 的粗体位（bit 0）。
func readMacStyleBold(data []byte) bool {
	head := findTable(data, "head")
	const macStyleOffset = 44
	if len(head) < macStyleOffset+2 {
		return false
	}
	return binary.BigEndian.Uint16(head[macStyleOffset:macStyleOffset+2])&0x0001 != 0
}

// englishName 从 name 表里取指定名称的英文记录。
// 优先 Windows 平台的 en-US（3/1/0x0409），其次 Macintosh 平台的英文（1/0/0）。
func englishName(data []byte, nameID uint16) string {
	table := findTable(data, "name")
	if len(table) < 6 {
		return ""
	}
	count := int(binary.BigEndian.Uint16(table[2:4]))
	storage := int(binary.BigEndian.Uint16(table[4:6]))
	if len(table) < 6+12*count {
		return ""
	}
	best, bestRank := "", 99
	for i := range count {
		rec := table[6+12*i : 6+12*(i+1)]
		if binary.BigEndian.Uint16(rec[6:8]) != nameID {
			continue
		}
		platform := binary.BigEndian.Uint16(rec[0:2])
		encoding := binary.BigEndian.Uint16(rec[2:4])
		lang := binary.BigEndian.Uint16(rec[4:6])
		length := int(binary.BigEndian.Uint16(rec[8:10]))
		offset := storage + int(binary.BigEndian.Uint16(rec[10:12]))
		if length <= 0 || len(table) < offset+length {
			continue
		}
		rank := 99
		switch {
		case platform == 3 && lang == 0x0409:
			rank = 0 // Windows / en-US
		case platform == 1 && lang == 0:
			rank = 1 // Macintosh / English
		case platform == 3:
			rank = 2 // Windows 的其它语言，聊胜于无
		}
		if rank >= bestRank {
			continue
		}
		raw := table[offset : offset+length]
		// platform 3 与 platform 0 用 UTF-16BE；platform 1 是单字节 MacRoman，
		// 字体名里只会出现 ASCII，直接当 ASCII 读即可。
		var value string
		if platform == 3 || platform == 0 || encoding == 1 && platform == 0 {
			value = decodeUTF16BE(raw)
		} else {
			value = string(raw)
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		best, bestRank = value, rank
	}
	return best
}

func decodeUTF16BE(raw []byte) string {
	if len(raw)%2 != 0 {
		raw = raw[:len(raw)-1]
	}
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		units = append(units, binary.BigEndian.Uint16(raw[i:i+2]))
	}
	return string(utf16.Decode(units))
}

// sfntCheckSum 按 sfnt 规范求和：以 4 字节大端为单位、模 2^32 相加，尾部按 0 补齐。
func sfntCheckSum(data []byte) uint32 {
	var sum uint32
	full := len(data) / 4 * 4
	for i := 0; i < full; i += 4 {
		sum += binary.BigEndian.Uint32(data[i : i+4])
	}
	if rest := len(data) - full; rest > 0 {
		var tail [4]byte
		copy(tail[:], data[full:])
		sum += binary.BigEndian.Uint32(tail[:])
	}
	return sum
}
