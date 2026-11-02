package lottery

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// 抽奖记录检索式。
//
// 一个输入框里可以写多段，空格分隔，全部同时满足：
//
//	LTD2026…            单号（完整单号走唯一索引，前缀按前缀匹配）
//	203.0.113.7         IP
//	10086               纯数字：用户 ID、记录 ID、账号，或奖品名里的数
//	月度会员 / 张三      奖品名、账号、昵称（三元组索引）
//	user:张三  prize:会员  device:abc  ip:1.2.3.4  no:LTD…  id:42   指定字段
//
// 字段名也认中文：用户、奖品、设备、单号。引号包起来的一段可以带空格：prize:"会员 15 天"。

// 检索字段。
const (
	SearchAny    = ""
	SearchNo     = "no"
	SearchUser   = "user"
	SearchPrize  = "prize"
	SearchDevice = "device"
	SearchIP     = "ip"
	SearchID     = "id"
)

// MaxSearchTerms 一次最多几段（多出来的忽略）。
const MaxSearchTerms = 6

// SearchTerm 检索式里的一段。Field 为空时按值的样子自动判断（见 Kind）。
type SearchTerm struct {
	Field string
	Value string
}

var searchFieldAlias = map[string]string{
	"no": SearchNo, "单号": SearchNo, "draw": SearchNo,
	"user": SearchUser, "u": SearchUser, "用户": SearchUser, "账号": SearchUser, "account": SearchUser,
	"prize": SearchPrize, "p": SearchPrize, "奖品": SearchPrize,
	"device": SearchDevice, "d": SearchDevice, "设备": SearchDevice,
	"ip": SearchIP,
	"id": SearchID,
}

var searchToken = regexp.MustCompile(`(?:([\p{L}\w]+):)?(?:"([^"]*)"|(\S+))`)

var drawNoPattern = regexp.MustCompile(`^(?i)LTD\d{1,30}$`)

// ParseDrawSearch 把检索式拆成段。未知字段名按普通文本处理（「a:b」整体当关键词）。
func ParseDrawSearch(input string) []SearchTerm {
	out := make([]SearchTerm, 0, 2)
	for _, match := range searchToken.FindAllStringSubmatch(strings.TrimSpace(input), -1) {
		if len(out) >= MaxSearchTerms {
			break
		}
		field, value := strings.ToLower(match[1]), match[2]
		if match[3] != "" {
			value = match[3]
		}
		value = strings.TrimSpace(value)
		if field != "" {
			if alias, ok := searchFieldAlias[field]; ok {
				field = alias
			} else {
				value, field = match[1]+":"+value, SearchAny
			}
		}
		if value == "" {
			continue
		}
		out = append(out, SearchTerm{Field: field, Value: value})
	}
	return out
}

// Kind 没指定字段的一段按样子归类：单号、IP、纯数字或文本（SearchAny）。
func (t SearchTerm) Kind() string {
	if t.Field != SearchAny {
		return t.Field
	}
	switch {
	case drawNoPattern.MatchString(t.Value):
		return SearchNo
	case isIP(t.Value):
		return SearchIP
	case isDigits(t.Value):
		return SearchID
	}
	return SearchAny
}

// DrawNoLength 完整单号的长度：LTD + 14 位时间 + 10 位随机数。
const DrawNoLength = 27

func isIP(value string) bool {
	_, err := netip.ParseAddr(value)
	return err == nil
}

func isDigits(value string) bool {
	if value == "" || len(value) > 19 {
		return false
	}
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}
