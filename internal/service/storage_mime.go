package service

import (
	"bufio"
	"errors"
	"io"
	"mime"
	"path"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// 嗅探窗口。3072 字节是 mimetype 内置全部魔数签名所需的上界，
// 再多读只会让 bufio 缓冲变大而不会提高识别率。
const mimeSniffLimit = 3072

// resolveUploadContentType 判定上传内容的真实类型，并返回一个可继续完整读取的 reader。
//
// 重构前这里只做两件事：信客户端给的 Content-Type，客户端没给就按扩展名猜。
// 两者都是**调用方说了算**的信息，于是 content_type 这一列实际上不可信：
//
//   - 控制台的类型筛选、图标、缩略图判定全都读这一列，一个改了扩展名的文件
//     就能让它显示成图片；
//   - 代理下载按这一列回 Content-Type，谎报的类型会一路传到浏览器。
//
// 现在改成魔数优先：只有魔数识别不出来（application/octet-stream）时，
// 才回落到声明值与扩展名。返回的 reader 是带缓冲的包装 —— 嗅探读掉的头部
// 仍在缓冲里，上传链路拿到的依旧是完整内容。
func resolveUploadContentType(content io.Reader, declared string, fileName string) (io.Reader, string, string) {
	fallback := normalizeContentType(declared)
	if fallback == "" || isGenericContentType(fallback) {
		if byExt := normalizeContentType(mime.TypeByExtension(strings.ToLower(path.Ext(strings.TrimSpace(fileName))))); byExt != "" {
			fallback = byExt
		}
	}
	if content == nil {
		return content, fallback, ""
	}

	// Peek 不消费数据：嗅探完之后把这个 bufio.Reader 交给上传链路即可，
	// 不需要把整个文件读进内存再回放（大文件上传会因此爆内存）。
	buffered := bufio.NewReaderSize(content, mimeSniffLimit+512)
	head, err := buffered.Peek(mimeSniffLimit)
	if err != nil && !errors.Is(err, io.EOF) {
		// 读不出头部就不改判，让真正的读取错误在上传阶段如实抛出
		return buffered, fallback, ""
	}
	if len(head) == 0 {
		return buffered, fallback, ""
	}

	detected := normalizeContentType(mimetype.Detect(head).String())
	if detected == "" || isGenericContentType(detected) {
		return buffered, fallback, ""
	}
	// 只有大类不同才算「谎报」。image/jpeg 声明成 image/jpg 这类同类别的写法差异
	// 天天都有，报出来只会变成噪音。
	masquerade := ""
	if fallback != "" && contentTypeFamily(fallback) != contentTypeFamily(detected) {
		masquerade = fallback
	}
	return buffered, detected, masquerade
}

// normalizeContentType 去掉参数与大小写差异，保留完整类型串。
// 参数（charset 等）刻意保留在 detected 里不剥离 —— 文本类型丢了 charset
// 会让代理下载回一个没有编码声明的 text/plain，中文直接乱码。
func normalizeContentType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil {
		return strings.ToLower(value)
	}
	mediaType = strings.ToLower(mediaType)
	if charset := strings.TrimSpace(params["charset"]); charset != "" {
		return mediaType + "; charset=" + strings.ToLower(charset)
	}
	return mediaType
}

// isGenericContentType 判断是不是「等于没说」的类型
func isGenericContentType(value string) bool {
	switch contentTypeMediaType(value) {
	case "", "application/octet-stream", "binary/octet-stream", "application/unknown", "*/*":
		return true
	default:
		return false
	}
}

// contentTypeMediaType 取出不含参数的类型部分（text/plain; charset=utf-8 → text/plain）
func contentTypeMediaType(value string) string {
	if idx := strings.IndexByte(value, ';'); idx >= 0 {
		value = value[:idx]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

// contentTypeFamily 取大类（image / video / text / application …）
func contentTypeFamily(value string) string {
	media := contentTypeMediaType(value)
	if idx := strings.IndexByte(media, '/'); idx > 0 {
		return media[:idx]
	}
	return media
}

// isImageContentType 判定是否为可尝试渲染缩略图的图片类型
func isImageContentType(value string) bool {
	return contentTypeFamily(value) == "image"
}

// 这几类内容即便被当作图片/文档存进来，浏览器也会按脚本上下文解析。
// 直接 inline 展示等于在 API 域上开了一个任人上传的 HTML 页面。
var scriptableContentTypes = map[string]struct{}{
	"text/html":              {},
	"application/xhtml+xml":  {},
	"image/svg+xml":          {},
	"application/xml":        {},
	"text/xml":               {},
	"application/xslt+xml":   {},
	"application/javascript": {},
	"text/javascript":        {},
	"application/rdf+xml":    {},
	"application/mathml+xml": {},
}

// IsScriptableContentType 判断该类型内联展示时是否可能执行脚本。
// 传输层用它决定代理下载要不要叠 CSP sandbox。
func IsScriptableContentType(value string) bool {
	_, ok := scriptableContentTypes[contentTypeMediaType(value)]
	return ok
}
