package cloudstorage

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// 内容在请求 / 响应里的形态与落盘字节之间的转换。
//
// 三种编码各对应一种「客户端手里本来就有的东西」：
//
//	json    一个 JSON 值（对象 / 数组 / 标量），原样落盘，读回时字节完全一致
//	text    一段文本，请求里是 JSON 字符串，落盘的是 UTF-8 字节
//	base64  任意二进制，请求里是 base64 字符串，落盘的是解码后的字节
//
// json 不做重新序列化：客户端可能依赖字段顺序算自己的摘要，服务端改写一遍
// 就会让「上传前后 sha256 一致」这件事不成立。

// ErrInvalidContent 内容与声明的编码对不上。
var ErrInvalidContent = errors.New("invalid content")

// DecodeContent 把请求里的 content 按编码还原成落盘字节。
func DecodeContent(encoding string, raw json.RawMessage) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, ErrInvalidContent
	}
	switch encoding {
	case EncodingJSON:
		if !json.Valid(trimmed) {
			return nil, ErrInvalidContent
		}
		return append([]byte(nil), trimmed...), nil
	case EncodingText:
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, ErrInvalidContent
		}
		if !utf8.ValidString(text) {
			return nil, ErrInvalidContent
		}
		return []byte(text), nil
	case EncodingBase64:
		var encoded string
		if err := json.Unmarshal(trimmed, &encoded); err != nil {
			return nil, ErrInvalidContent
		}
		return decodeBase64Flexible(encoded)
	default:
		return nil, ErrInvalidContent
	}
}

// EncodeContent 把落盘字节按编码转回响应里的 content。
func EncodeContent(encoding string, data []byte) (json.RawMessage, error) {
	switch encoding {
	case EncodingJSON:
		if !json.Valid(data) {
			return nil, ErrInvalidContent
		}
		return json.RawMessage(data), nil
	case EncodingText:
		encoded, err := json.Marshal(string(data))
		return encoded, err
	default:
		encoded, err := json.Marshal(base64.StdEncoding.EncodeToString(data))
		return encoded, err
	}
}

// ValidateUploadEncoding 上传（multipart）的内容是原始字节，编码只能是 base64 或
// 声明为 json / text 时满足对应约束 —— 否则读回时交不出声明的形态。
func ValidateUploadEncoding(encoding string, data []byte) error {
	switch encoding {
	case EncodingJSON:
		if !json.Valid(data) {
			return ErrInvalidContent
		}
	case EncodingText:
		if !utf8.Valid(data) {
			return ErrInvalidContent
		}
	case EncodingBase64:
	default:
		return ErrInvalidContent
	}
	return nil
}

// decodeBase64Flexible 标准与 URL 安全两种字母表、有无 padding 都认：
// 各平台默认的 base64 实现不一样，拒掉其中一种只会制造无意义的接入问题。
func decodeBase64Flexible(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, value)
	if strings.ContainsAny(value, "-_") {
		value = strings.NewReplacer("-", "+", "_", "/").Replace(value)
	}
	value = strings.TrimRight(value, "=")
	data, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return nil, ErrInvalidContent
	}
	return data, nil
}
