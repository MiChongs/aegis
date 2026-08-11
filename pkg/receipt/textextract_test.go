package receipt

import (
	"bytes"
	"compress/zlib"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// PDF 内容还原：把生成出来的 PDF 反解成「哪段文字画在哪个坐标」。
//
// 为什么要做这件事：渲染器不出错**不等于**凭证是对的。文字画到页外、
// 两栏叠在一起、金额落在错误的列、某个字段整段没画出来 —— 这些在字节层面都是合法 PDF。
// 只有把文字连同坐标读回来，才能对内容与版式下断言。
//
// 可行的前提是 gopdf 会为子集字体写 /ToUnicode CMap（不写的话 PDF 里只有字形编号，
// 复制粘贴出来是乱码，阅读器也搜不到字）。顺带说明：这份 CMap 存在本身就值得测 ——
// 它决定了用户能不能在凭证里搜索订单号。

// textRun 一次落笔：在 (x, y) 处以 size 号字画出的一段文字。
// 坐标是 PDF 用户空间，原点在左下角。
type textRun struct {
	x, y float64
	size float64
	text string
}

var (
	// gopdf 每次落笔都自开 BT/ET，因此 TD 里的位移就是绝对坐标
	textBlockPattern = regexp.MustCompile(`(?s)BT\n(-?[\d.]+) (-?[\d.]+) TD\n/F\d+ ([\d.]+) Tf.*?\[(.*?)\] TJ`)
	bfRangePattern   = regexp.MustCompile(`<([0-9A-Fa-f]+)><([0-9A-Fa-f]+)><([0-9A-Fa-f]+)>`)
	hexRunPattern    = regexp.MustCompile(`<([0-9A-Fa-f]+)>`)
)

// extractPages 还原每一页上的全部文字，按阅读顺序（自上而下、自左而右）排列。
func extractPages(pdf []byte) [][]textRun {
	toUnicode := map[uint16]rune{}
	var contents []string
	for _, stream := range pdfStreams(pdf) {
		switch {
		case strings.Contains(stream, "beginbfrange"):
			parseToUnicode(stream, toUnicode)
		case strings.Contains(stream, " TJ"):
			contents = append(contents, stream)
		}
	}
	pages := make([][]textRun, 0, len(contents))
	for _, content := range contents {
		var runs []textRun
		for _, match := range textBlockPattern.FindAllStringSubmatch(content, -1) {
			x, _ := strconv.ParseFloat(match[1], 64)
			y, _ := strconv.ParseFloat(match[2], 64)
			size, _ := strconv.ParseFloat(match[3], 64)
			if text := decodeGlyphs(match[4], toUnicode); text != "" {
				runs = append(runs, textRun{x: x, y: y, size: size, text: text})
			}
		}
		slices.SortStableFunc(runs, func(a, b textRun) int {
			// y 越大越靠上；同一行内按 x 排。同行判定给 1.5pt 容差，
			// 因为同一行里不同字号的基线本来就不完全对齐。
			if diff := b.y - a.y; diff > 1.5 || diff < -1.5 {
				if diff > 0 {
					return 1
				}
				return -1
			}
			if a.x < b.x {
				return -1
			}
			return 1
		})
		pages = append(pages, runs)
	}
	return pages
}

// parseToUnicode 解析 /ToUnicode CMap 的 bfrange 段：<起><止><目标>。
func parseToUnicode(stream string, out map[uint16]rune) {
	body := stream
	if idx := strings.Index(body, "beginbfrange"); idx >= 0 {
		body = body[idx:]
	}
	if idx := strings.Index(body, "endbfrange"); idx >= 0 {
		body = body[:idx]
	}
	for _, match := range bfRangePattern.FindAllStringSubmatch(body, -1) {
		from, err1 := strconv.ParseUint(match[1], 16, 32)
		to, err2 := strconv.ParseUint(match[2], 16, 32)
		dst, err3 := strconv.ParseUint(match[3], 16, 32)
		if err1 != nil || err2 != nil || err3 != nil || to < from {
			continue
		}
		for code := from; code <= to; code++ {
			out[uint16(code)] = rune(dst + (code - from))
		}
	}
}

// decodeGlyphs 把 TJ 数组里的字形编号译回文字。
// 数组形如 [<00380029>-12<004B>]，其中的数字是字距微调，跳过即可。
func decodeGlyphs(array string, toUnicode map[uint16]rune) string {
	var b strings.Builder
	for _, match := range hexRunPattern.FindAllStringSubmatch(array, -1) {
		hex := match[1]
		for i := 0; i+4 <= len(hex); i += 4 {
			code, err := strconv.ParseUint(hex[i:i+4], 16, 16)
			if err != nil {
				continue
			}
			if r, ok := toUnicode[uint16(code)]; ok {
				b.WriteRune(r)
			} else {
				b.WriteRune('�')
			}
		}
	}
	return b.String()
}

// pdfStreams 取出 PDF 里所有可读的流内容（必要时解压）。
// 带 /Length1 的是内嵌字体二进制，跳过。
func pdfStreams(pdf []byte) []string {
	var out []string
	rest := pdf
	for {
		start := bytes.Index(rest, []byte("stream"))
		if start < 0 {
			return out
		}
		dictStart := bytes.LastIndex(rest[:start], []byte("<<"))
		dict := ""
		if dictStart >= 0 {
			dict = string(rest[dictStart:start])
		}
		body := bytes.TrimLeft(rest[start+len("stream"):], "\r\n")
		end := bytes.Index(body, []byte("endstream"))
		if end < 0 {
			return out
		}
		payload := body[:end]
		if !strings.Contains(dict, "/Length1") {
			if strings.Contains(dict, "FlateDecode") {
				if reader, err := zlib.NewReader(bytes.NewReader(payload)); err == nil {
					if data, err := io.ReadAll(reader); err == nil {
						out = append(out, string(data))
					}
					_ = reader.Close()
				}
			} else {
				out = append(out, string(payload))
			}
		}
		rest = body[end+len("endstream"):]
	}
}

// pageText 把一页的全部文字拼成一行行字符串，用于「这份凭证上有没有这句话」这类断言。
func pageText(runs []textRun) string {
	var lines []string
	var current []string
	lastY := 0.0
	for i, run := range runs {
		if i > 0 && (lastY-run.y > 1.5 || run.y-lastY > 1.5) {
			lines = append(lines, strings.Join(current, " "))
			current = current[:0]
		}
		current = append(current, run.text)
		lastY = run.y
	}
	if len(current) > 0 {
		lines = append(lines, strings.Join(current, " "))
	}
	return strings.Join(lines, "\n")
}

// asciiPage 把一页渲染成等宽字符画，用来在终端里核对版式。
// 不追求像素级还原，只要「哪一列、哪一行、有没有互相压住」看得出来即可。
func asciiPage(runs []textRun, cols int) string {
	th := defaultTheme()
	const rows = 78
	scaleX := float64(cols) / th.pageW
	scaleY := float64(rows) / th.pageH

	grid := make([][]rune, rows)
	for i := range grid {
		grid[i] = []rune(strings.Repeat(" ", cols))
	}
	for _, run := range runs {
		row := int((th.pageH - run.y) * scaleY)
		col := int(run.x * scaleX)
		if row < 0 || row >= rows {
			continue
		}
		for _, r := range run.text {
			if col < 0 || col >= cols {
				col++
				continue
			}
			// 已有字符的位置不覆盖：留着才能看出两段文字是不是叠在一起了
			if grid[row][col] == ' ' {
				grid[row][col] = r
			}
			col++
		}
	}
	var b strings.Builder
	for _, line := range grid {
		b.WriteString(strings.TrimRight(string(line), " "))
		b.WriteByte('\n')
	}
	return b.String()
}
