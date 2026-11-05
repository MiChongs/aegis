package service

import (
	"strings"
	"sync"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// 美团、UAPIs 只给中文国名，地理封禁与「首次从新国家登录」都按 ISO 代码比对，
// 这里把中文国名换回两位代码。表从 CLDR 简体中文地区名反推，再补上归属地接口的常见简称。
var (
	countryCodeOnce   sync.Once
	countryCodeByName map[string]string
)

var countryNameAliases = map[string]string{
	"中国香港": "HK", "香港": "HK", "中国澳门": "MO", "澳门": "MO", "中国台湾": "TW", "台湾": "TW",
	"阿联酋": "AE", "英国": "GB", "美国": "US", "俄罗斯": "RU", "韩国": "KR", "朝鲜": "KP",
	"刚果金": "CD", "刚果民主共和国": "CD", "刚果布": "CG", "刚果共和国": "CG", "捷克共和国": "CZ",
	"梵蒂冈": "VA", "东帝汶": "TL", "北马其顿": "MK", "马其顿": "MK", "斯威士兰": "SZ", "缅甸": "MM",
	"沙特": "SA", "新西兰": "NZ", "波黑": "BA", "中非": "CF", "多米尼加": "DO", "密克罗尼西亚": "FM",
	"巴勒斯坦": "PS", "科特迪瓦": "CI", "佛得角": "CV", "圣多美和普林西比": "ST",
}

func countryCodeFromName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if code, ok := countryNameAliases[name]; ok {
		return code
	}
	countryCodeOnce.Do(func() {
		names := display.Chinese.Regions()
		countryCodeByName = make(map[string]string, 300)
		for a := 'A'; a <= 'Z'; a++ {
			for b := 'A'; b <= 'Z'; b++ {
				code := string([]rune{a, b})
				region, err := language.ParseRegion(code)
				if err != nil || !region.IsCountry() || region.String() != code {
					continue
				}
				if label := names.Name(region); label != "" {
					countryCodeByName[label] = code
				}
			}
		}
	})
	return countryCodeByName[name]
}
