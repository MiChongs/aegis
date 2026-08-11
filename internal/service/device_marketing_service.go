package service

import (
	"bufio"
	"context"
	_ "embed"
	"net/http"
	"regexp"
	"strings"
	"time"

	devicedomain "aegis/internal/domain/device"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// 嵌入种子文件：从开源 kotlin 项目 de.boehrsi.devicemarketingnames 转储而来
//
//go:embed seeds/device_identifiers.kt
var deviceMarketingSeed string

// DeviceMarketingService 设备营销名称字典服务
//
// 职责：
//   - 启动时从嵌入的 kt 源文件解析数据并种子到 DB（幂等）
//   - 提供 CRUD API（权限控制由 handler 层做超级管理员校验）
//   - 提供运行时查询接口（Lookup），业务模块（如审计/风控/session）可用其把
//     iPhone14,3 / SM-G998B 之类的原始识别码翻译为人类可读名称
type DeviceMarketingService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
}

func NewDeviceMarketingService(log *zap.Logger, pg *pgrepo.Repository) *DeviceMarketingService {
	return &DeviceMarketingService{log: log, pg: pg}
}

// SeedIfEmpty 启动时调用，表为空则自动从嵌入源种子数据（并发幂等，重复调用无副作用）
func (s *DeviceMarketingService) SeedIfEmpty(ctx context.Context) error {
	if s == nil || s.pg == nil {
		return nil
	}
	total, err := s.pg.CountDeviceMarketingNames(ctx)
	if err != nil {
		return err
	}
	if total > 0 {
		s.log.Info("device marketing names already seeded", zap.Int64("count", total))
		return nil
	}
	stats, err := s.seedFromEmbedded(ctx, devicedomain.SourceSeed, false)
	if err != nil {
		return err
	}
	s.log.Info("device marketing names seeded",
		zap.Int("parsed", stats.Parsed),
		zap.Int("inserted", stats.Inserted),
		zap.Int64("elapsedMs", stats.ElapsedMs))
	return nil
}

// Reseed 强制重新种子：按最新算法重跑厂商匹配规则 + 默认图标，覆盖所有 source='seed' 的记录。
// source='manual' 的人工记录永不被覆盖。
func (s *DeviceMarketingService) Reseed(ctx context.Context) (*devicedomain.SeedStats, error) {
	if s == nil || s.pg == nil {
		return nil, apperrors.New(50010, http.StatusServiceUnavailable, "service unavailable")
	}
	return s.seedFromEmbedded(ctx, devicedomain.SourceSeed, true)
}

func (s *DeviceMarketingService) seedFromEmbedded(ctx context.Context, source string, forceRefresh bool) (*devicedomain.SeedStats, error) {
	started := time.Now()
	items := parseDeviceIdentifiersKT(deviceMarketingSeed)
	inserted, err := s.pg.BulkUpsertDeviceMarketingNames(ctx, items, source, forceRefresh)
	if err != nil {
		return nil, err
	}
	elapsed := time.Since(started)
	return &devicedomain.SeedStats{
		Parsed:    len(items),
		Inserted:  inserted,
		Duplicate: len(items) - inserted,
		Elapsed:   elapsed,
		ElapsedMs: elapsed.Milliseconds(),
	}, nil
}

// List 分页查询
func (s *DeviceMarketingService) List(ctx context.Context, filter devicedomain.Filter) (*devicedomain.Page, error) {
	if filter.Platform != "" {
		p := strings.ToLower(strings.TrimSpace(filter.Platform))
		if p != devicedomain.PlatformIOS && p != devicedomain.PlatformAndroid {
			return nil, apperrors.New(40000, http.StatusBadRequest, "platform 仅支持 ios / android")
		}
		filter.Platform = p
	}
	return s.pg.ListDeviceMarketingNames(ctx, filter)
}

// Get 查询详情
func (s *DeviceMarketingService) Get(ctx context.Context, id int64) (*devicedomain.MarketingName, error) {
	if id <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "id 无效")
	}
	item, err := s.pg.GetDeviceMarketingName(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40404, http.StatusNotFound, "设备营销名称不存在")
	}
	return item, nil
}

// Lookup 根据平台 + 标识查询营销名称（主要用途：运行时翻译）
// 未找到返回 nil, nil（非错误语义，调用方可回退到 UA 推断）
func (s *DeviceMarketingService) Lookup(ctx context.Context, platform, identifier string) (*devicedomain.MarketingName, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	identifier = strings.TrimSpace(identifier)
	if platform == "" || identifier == "" {
		return nil, nil
	}
	return s.pg.FindDeviceMarketingName(ctx, platform, identifier)
}

// Create 管理员新增
func (s *DeviceMarketingService) Create(ctx context.Context, input devicedomain.CreateInput) (*devicedomain.MarketingName, error) {
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	input.Identifier = strings.TrimSpace(input.Identifier)
	input.MarketingName = strings.TrimSpace(input.MarketingName)
	input.Manufacturer = strings.TrimSpace(input.Manufacturer)
	input.ManufacturerIconURL = strings.TrimSpace(input.ManufacturerIconURL)
	input.DeviceImageURL = strings.TrimSpace(input.DeviceImageURL)
	// 若未提供厂商图标且能推断出厂商，自动带上默认图标
	if input.ManufacturerIconURL == "" && input.Manufacturer != "" {
		input.ManufacturerIconURL = manufacturerIconURL(input.Manufacturer)
	}
	// iOS 缺省厂商补齐为 Apple
	if input.Manufacturer == "" && input.Platform == devicedomain.PlatformIOS {
		input.Manufacturer = "Apple"
		if input.ManufacturerIconURL == "" {
			input.ManufacturerIconURL = manufacturerIconURL("Apple")
		}
	}
	if input.Platform != devicedomain.PlatformIOS && input.Platform != devicedomain.PlatformAndroid {
		return nil, apperrors.New(40000, http.StatusBadRequest, "platform 仅支持 ios / android")
	}
	if input.Identifier == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "identifier 不能为空")
	}
	if input.MarketingName == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "marketingName 不能为空")
	}
	// 检查是否已存在（给明确错误码，避免外键冲突原始错误裸露）
	existing, err := s.pg.FindDeviceMarketingName(ctx, input.Platform, input.Identifier)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperrors.New(40901, http.StatusConflict, "该平台下该标识符已存在")
	}
	return s.pg.CreateDeviceMarketingName(ctx, input, devicedomain.SourceManual)
}

// Update 管理员更新（部分更新）
func (s *DeviceMarketingService) Update(ctx context.Context, id int64, input devicedomain.UpdateInput) (*devicedomain.MarketingName, error) {
	if id <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "id 无效")
	}
	existing, err := s.pg.GetDeviceMarketingName(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, apperrors.New(40404, http.StatusNotFound, "设备营销名称不存在")
	}
	// 若修改 platform+identifier 组合，确保无冲突
	if input.Platform != nil || input.Identifier != nil {
		newPlatform := existing.Platform
		newIdentifier := existing.Identifier
		if input.Platform != nil {
			newPlatform = strings.ToLower(strings.TrimSpace(*input.Platform))
			if newPlatform != devicedomain.PlatformIOS && newPlatform != devicedomain.PlatformAndroid {
				return nil, apperrors.New(40000, http.StatusBadRequest, "platform 仅支持 ios / android")
			}
			input.Platform = &newPlatform
		}
		if input.Identifier != nil {
			newIdentifier = strings.TrimSpace(*input.Identifier)
			if newIdentifier == "" {
				return nil, apperrors.New(40000, http.StatusBadRequest, "identifier 不能为空")
			}
			input.Identifier = &newIdentifier
		}
		if newPlatform != existing.Platform || newIdentifier != existing.Identifier {
			dup, err := s.pg.FindDeviceMarketingName(ctx, newPlatform, newIdentifier)
			if err != nil {
				return nil, err
			}
			if dup != nil && dup.ID != id {
				return nil, apperrors.New(40901, http.StatusConflict, "该平台下该标识符已存在")
			}
		}
	}
	if input.MarketingName != nil {
		trimmed := strings.TrimSpace(*input.MarketingName)
		if trimmed == "" {
			return nil, apperrors.New(40000, http.StatusBadRequest, "marketingName 不能为空")
		}
		input.MarketingName = &trimmed
	}
	return s.pg.UpdateDeviceMarketingName(ctx, id, input)
}

// Delete 管理员删除
func (s *DeviceMarketingService) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return apperrors.New(40000, http.StatusBadRequest, "id 无效")
	}
	if err := s.pg.DeleteDeviceMarketingName(ctx, id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return apperrors.New(40404, http.StatusNotFound, "设备营销名称不存在")
		}
		return err
	}
	return nil
}

// ListManufacturers 去重返回所有已登记的厂商名称，供前端筛选
func (s *DeviceMarketingService) ListManufacturers(ctx context.Context, platform string) ([]string, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform != "" && platform != devicedomain.PlatformIOS && platform != devicedomain.PlatformAndroid {
		return nil, apperrors.New(40000, http.StatusBadRequest, "platform 仅支持 ios / android")
	}
	return s.pg.ListDeviceManufacturers(ctx, platform)
}

// ─── Kotlin 源文件解析 ─────────────────────────────────

// 匹配形如：put("iPhone14,3", "iPhone 13 Pro Max")
// 容忍前导空白和右侧注释（虽然源文件没有）
var devicePutRegex = regexp.MustCompile(`put\("([^"]+)",\s*"((?:[^"\\]|\\.)*)"\s*\)`)

// 厂商匹配规则（严格版）
//
// 匹配策略：仅匹配"高确信度"的品牌标识，不用短数字前缀避免误伤
//
//	idPrefix —— identifier 必须以此开头（大小写不敏感），最强确信度
//	idWord   —— identifier 作为**独立词**（被非字母数字字符包围）出现
//	nameWord —— marketingName 作为**独立词**出现
//
// 字典顺序即优先级：更特异的品牌排在前面（如 Honor 先于 Huawei）
type manufacturerRule struct {
	name     string
	idPrefix []string
	idWord   []string
	nameWord []string
}

var manufacturerRules = []manufacturerRule{
	{name: "Apple", idPrefix: []string{"iphone", "ipad", "ipod", "applewatch", "appletv", "homepod"}, nameWord: []string{"iphone", "ipad", "ipod", "apple tv", "apple watch", "homepod"}},

	// Samsung 前缀独特
	{name: "Samsung", idPrefix: []string{"sm-", "sm_", "sch-", "shv-", "shw-", "scv", "scg", "sph-", "sgh-", "gt-", "yp-", "samsung", "galaxy"}, nameWord: []string{"galaxy", "samsung"}},

	// Honor 独立出来，避免被 Huawei 吞掉
	{name: "Honor", idPrefix: []string{"honor ", "honor_", "hry-", "jsn-", "jnye-"}, idWord: []string{"honor"}, nameWord: []string{"honor"}},
	{name: "Huawei", idPrefix: []string{"huawei", "hw-"}, idWord: []string{"huawei"}, nameWord: []string{"huawei", "nova", "mate ", "p30", "p40", "p50", "p60", "pura"}},

	{name: "Xiaomi", idPrefix: []string{"xiaomi", "redmi", "poco", "mi-", "mi_", "black shark", "23076", "23049", "23053"}, idWord: []string{"mi", "xiaomi", "redmi"}, nameWord: []string{"xiaomi", "redmi", "poco", "black shark", "pocofone"}},

	{name: "OPPO", idPrefix: []string{"oppo", "cph", "padm", "pafm", "paam", "pabt", "pacm", "pagm", "pahm", "paim", "pajm", "pakm", "palm", "pamm", "panm", "paom", "papm", "paqm", "parm", "pasm", "patm"}, nameWord: []string{"oppo", "reno", "find x", "find n"}},

	{name: "vivo", idPrefix: []string{"vivo "}, idWord: []string{"vivo"}, nameWord: []string{"vivo"}},
	{name: "iQOO", idPrefix: []string{"iqoo"}, idWord: []string{"iqoo"}, nameWord: []string{"iqoo"}},

	{name: "OnePlus", idPrefix: []string{"oneplus", "op5", "op56"}, nameWord: []string{"oneplus"}},

	{name: "Motorola", idPrefix: []string{"moto ", "moto_", "motorola", "mot-", "xt16", "xt17", "xt18", "xt19", "xt20", "xt21", "xt22", "xt23", "xt24"}, nameWord: []string{"motorola", "moto g", "moto e", "moto z", "moto x", "moto c", "moto ", "droid"}},

	{name: "Google", idPrefix: []string{"pixel"}, idWord: []string{"pixel", "nexus"}, nameWord: []string{"pixel", "nexus"}},

	{name: "Sony", idPrefix: []string{"sony", "xperia", "sgp", "xq-", "xq_"}, nameWord: []string{"sony", "xperia"}},

	{name: "LG", idPrefix: []string{"lg-", "lg_", "lg ", "lgl", "lgm", "lgh", "lgk", "lge-", "vs9", "vs5"}, nameWord: []string{"lg "}},

	{name: "HMD / Nokia", idPrefix: []string{"nokia", "ta-"}, nameWord: []string{"nokia"}},

	{name: "ASUS", idPrefix: []string{"asus", "zenfone", "rog phone"}, idWord: []string{"asus", "zenfone"}, nameWord: []string{"asus", "zenfone", "rog phone"}},

	// Lenovo 前缀很明确；另 Tab 系列用 TB- / TB2 / TB3（带连字符）
	{name: "Lenovo", idPrefix: []string{"lenovo", "thinkpad", "thinkbook", "tb-", "tb2-", "tb3-", "tb7", "tb8", "tab"}, idWord: []string{"lenovo"}, nameWord: []string{"lenovo", "thinkpad", "thinkbook", "yoga "}},

	{name: "realme", idPrefix: []string{"realme", "rmx", "rmp"}, nameWord: []string{"realme"}},

	{name: "TCL", idPrefix: []string{"tcl "}, idWord: []string{"tcl"}, nameWord: []string{"tcl "}},

	{name: "Infinix", idPrefix: []string{"infinix"}, idWord: []string{"infinix"}, nameWord: []string{"infinix"}},

	// TECNO 及旗下 DroiPad 平板子品牌
	{name: "TECNO", idPrefix: []string{"tecno", "droipad"}, idWord: []string{"tecno", "droipad"}, nameWord: []string{"tecno", "spark", "pova", "phantom", "camon", "droipad"}},

	{name: "Alcatel", idPrefix: []string{"alcatel"}, idWord: []string{"alcatel"}, nameWord: []string{"alcatel"}},

	// 把 nubia 和 ZTE 分开
	{name: "nubia", idPrefix: []string{"nubia", "nx"}, idWord: []string{"nubia"}, nameWord: []string{"nubia"}},
	{name: "ZTE", idPrefix: []string{"zte "}, idWord: []string{"zte"}, nameWord: []string{"zte "}},

	{name: "Meizu", idPrefix: []string{"meizu"}, idWord: []string{"meizu"}, nameWord: []string{"meizu"}},

	{name: "BlackBerry", idPrefix: []string{"blackberry", "bb "}, idWord: []string{"blackberry"}, nameWord: []string{"blackberry"}},

	{name: "Microsoft", idPrefix: []string{"surface", "microsoft"}, idWord: []string{"surface"}, nameWord: []string{"microsoft", "surface"}},

	// HTC：覆盖 "htc6545lvw" / "HTC_0P3P5" / "HTC One" 等多种拼写
	// 直接用 "htc" 作前缀是安全的 —— 无其它主流 OEM 以此开头
	{name: "HTC", idPrefix: []string{"htc"}, idWord: []string{"htc", "htc_"}, nameWord: []string{"htc ", "desire", "butterfly"}},

	{name: "Archos", idPrefix: []string{"archos"}, idWord: []string{"archos"}, nameWord: []string{"archos"}},
	{name: "Wiko", idPrefix: []string{"wiko"}, idWord: []string{"wiko"}, nameWord: []string{"wiko"}},
	{name: "Luvo", idPrefix: []string{"luvo "}, idWord: []string{"luvo"}, nameWord: []string{"luvo"}},
	{name: "Doogee", idPrefix: []string{"doogee"}, idWord: []string{"doogee"}, nameWord: []string{"doogee"}},
	{name: "Ulefone", idPrefix: []string{"ulefone"}, idWord: []string{"ulefone"}, nameWord: []string{"ulefone"}},
	{name: "CUBOT", idPrefix: []string{"cubot"}, idWord: []string{"cubot"}, nameWord: []string{"cubot"}},
	{name: "BLU", idPrefix: []string{"blu "}, idWord: []string{"blu"}, nameWord: []string{"blu "}},
	{name: "Panasonic", idPrefix: []string{"panasonic", "eluga"}, idWord: []string{"panasonic"}, nameWord: []string{"panasonic", "eluga"}},
	{name: "Sharp", idPrefix: []string{"sharp", "aquos", "sh-", "sh_"}, idWord: []string{"sharp"}, nameWord: []string{"sharp", "aquos"}},
	{name: "Kyocera", idPrefix: []string{"kyocera", "kyy", "kyv", "kyf"}, idWord: []string{"kyocera"}, nameWord: []string{"kyocera", "duraforce"}},
	{name: "Hisense", idPrefix: []string{"hisense"}, idWord: []string{"hisense"}, nameWord: []string{"hisense"}},
	{name: "Gionee", idPrefix: []string{"gionee"}, idWord: []string{"gionee"}, nameWord: []string{"gionee"}},
	{name: "Coolpad", idPrefix: []string{"coolpad"}, idWord: []string{"coolpad"}, nameWord: []string{"coolpad"}},
	{name: "Micromax", idPrefix: []string{"micromax"}, idWord: []string{"micromax"}, nameWord: []string{"micromax"}},
	{name: "Lava", idPrefix: []string{"lava "}, idWord: []string{"lava"}, nameWord: []string{"lava"}},
	{name: "Karbonn", idPrefix: []string{"karbonn"}, idWord: []string{"karbonn"}, nameWord: []string{"karbonn"}},
	{name: "Intex", idPrefix: []string{"intex"}, idWord: []string{"intex"}, nameWord: []string{"intex"}},
	{name: "Amazon", idPrefix: []string{"kftt", "kfthwi", "kfthwa", "kfot", "kfgiwi", "kfdowi", "kfauwi", "kfapwi", "kfarwi", "kfasawi", "kfsuwi", "kfsawa", "kfsawi", "kfmewi", "kfmuwi", "kfkawi", "kfsowi", "kfraw", "fire "}, nameWord: []string{"kindle", "fire phone", "fire tv", "fire hd", "fire hdx"}},
	{name: "Azumi", idPrefix: []string{"azumi"}, idWord: []string{"azumi"}, nameWord: []string{"azumi"}},
	{name: "Cherry Mobile", idPrefix: []string{"cherry "}, idWord: []string{"cherrymobile"}, nameWord: []string{"cherry mobile"}},
	{name: "OUKITEL", idPrefix: []string{"oukitel"}, idWord: []string{"oukitel"}, nameWord: []string{"oukitel"}},
	{name: "Blackview", idPrefix: []string{"blackview"}, idWord: []string{"blackview"}, nameWord: []string{"blackview"}},
	{name: "Umidigi", idPrefix: []string{"umidigi"}, idWord: []string{"umidigi"}, nameWord: []string{"umidigi"}},
	{name: "Fairphone", idPrefix: []string{"fairphone", "fp"}, idWord: []string{"fairphone"}, nameWord: []string{"fairphone"}},
	{name: "Nothing", idPrefix: []string{"a063", "a065", "a142", "a143", "nothing"}, idWord: []string{"nothing"}, nameWord: []string{"nothing phone"}},
	{name: "Vsmart", idPrefix: []string{"vsmart"}, idWord: []string{"vsmart"}, nameWord: []string{"vsmart"}},
	{name: "Lephone", idPrefix: []string{"lephone"}, idWord: []string{"lephone"}, nameWord: []string{"lephone"}},
	{name: "Lyf", idPrefix: []string{"lyf "}, idWord: []string{"lyf"}, nameWord: []string{"lyf"}},
	{name: "itel", idPrefix: []string{"itel"}, idWord: []string{"itel"}, nameWord: []string{"itel"}},
	{name: "Vestel", idPrefix: []string{"vestel"}, idWord: []string{"vestel"}, nameWord: []string{"vestel"}},
	{name: "Panasonic KX", idPrefix: []string{"kx-"}, nameWord: []string{"panasonic kx"}},

	// Prestigio：俄/东欧老牌平板手机厂，MultiPad / MultiPhone / Wize / Grace 等产品线
	{name: "Prestigio",
		idPrefix: []string{"multipad", "multiphone", "prestigio", "pmp", "pmt", "pap"},
		idWord:   []string{"multipad", "multiphone", "prestigio", "wize", "grace", "muze"},
		nameWord: []string{"multipad", "multiphone", "prestigio", "wize", "muze", "grace"}},

	// Azpen：北美平板品牌，Prestige 系列（Prestige_10QH_Plus 等）
	// 用带分隔符的前缀避免 "Prestigio" 被误伤（含 "prestigi" 而非 "prestige_"）
	{name: "Azpen",
		idPrefix: []string{"prestige_", "prestige-", "prestige ", "azpen"},
		idWord:   []string{"prestige", "azpen"},
		nameWord: []string{"prestige", "azpen"}},

	// Hampoo：深圳 ODM，Hampoo Pad 系列通常以 HPPR 开头（如 HPPR10A / HPPR07S）
	{name: "Hampoo",
		idPrefix: []string{"hppr", "hampoo"},
		idWord:   []string{"hampoo"},
		nameWord: []string{"hampoo"}},

	// CHUWI：深圳平板厂，HiPad / HiBook / SurBook / HeroBook / CoreBook / MiniBook
	{name: "CHUWI",
		idPrefix: []string{"hipad", "hibook", "herobook", "corebook", "surbook", "minibook", "ubook", "chuwi"},
		idWord:   []string{"hipad", "hibook", "herobook", "corebook", "surbook", "minibook", "ubook", "chuwi"},
		nameWord: []string{"hipad", "hibook", "herobook", "corebook", "surbook", "minibook", "ubook", "chuwi"}},

	// Olivetti：意大利厂商，Olipad Smart / Olipad chagall / Olipad M-Touch 等
	{name: "Olivetti",
		idPrefix: []string{"olipad", "olivetti"},
		idWord:   []string{"olipad", "olivetti", "chagall"},
		nameWord: []string{"olipad", "olivetti", "chagall"}},

	// Klipad：法国平板品牌，Klipad A1040M / Klipad Pro 等
	{name: "Klipad",
		idPrefix: []string{"klipad"},
		idWord:   []string{"klipad"},
		nameWord: []string{"klipad"}},

	// HP (Hewlett-Packard)：HP Slate / HP 10 / HP Stream 等平板 & 二合一
	// 用带空格 / 下划线 / 连字符 的前缀避免 "hpd"/"hpc" 这种非 HP 的误伤
	{name: "HP",
		idPrefix: []string{"hp ", "hp_", "hp-", "hewlett", "slate"},
		idWord:   []string{"hp", "hewlett", "hewlett-packard"},
		nameWord: []string{"hewlett", "hewlett-packard", "hp slate", "hp stream", "hp elitebook", "hp probook", "hp omnibook", "hp 10", "hp 11", "hp 12", "hp 13", "hp 14", "hp 15", "hp 17"}},

	// INOI：俄罗斯品牌，机型 inoiPad / T107 / R7 / 5 等
	// nameWord 必须包含 "inoipad" —— 因为 "inoipad_pro" 这种串里 "inoi" 左边是开头 但右边是 "p"（字母），
	// 按词边界规则不算独立词；放 "inoipad" 才能命中
	{name: "INOI",
		idPrefix: []string{"inoi", "inoipad"},
		idWord:   []string{"inoi", "inoipad"},
		nameWord: []string{"inoi", "inoipad"}},
}

// isAsciiAlnum 是字母或数字
func isAsciiAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z')
}

// containsWord 判断 haystack 是否包含 needle 作为"独立词"（边界为非字母数字字符）
// 输入 haystack 和 needle 均应为小写
func containsWord(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] != needle {
			continue
		}
		// 左边界
		if i > 0 && isAsciiAlnum(haystack[i-1]) {
			continue
		}
		// 右边界
		end := i + len(needle)
		if end < len(haystack) && isAsciiAlnum(haystack[end]) {
			continue
		}
		return true
	}
	return false
}

// guessManufacturer 依据 identifier + 营销名 综合推断厂商
//
// 匹配次序（一旦命中立即返回）：
//  1. identifier 以 idPrefix 开头 —— 强确信（HTC_xxx / LG-xxx / SM-xxx）
//  2. identifier 独立词包含 idWord —— 中确信（"HTC One M9" 中的 HTC）
//  3. marketingName 独立词包含 nameWord —— 兜底
func guessManufacturer(platform, identifier, marketingName string) string {
	if platform == devicedomain.PlatformIOS {
		return "Apple"
	}
	idLower := strings.ToLower(strings.TrimSpace(identifier))
	nameLower := strings.ToLower(strings.TrimSpace(marketingName))
	for _, rule := range manufacturerRules {
		for _, p := range rule.idPrefix {
			if strings.HasPrefix(idLower, p) {
				return rule.name
			}
		}
	}
	for _, rule := range manufacturerRules {
		for _, w := range rule.idWord {
			if containsWord(idLower, w) {
				return rule.name
			}
		}
	}
	for _, rule := range manufacturerRules {
		for _, w := range rule.nameWord {
			if containsWord(nameLower, w) {
				return rule.name
			}
		}
	}
	return ""
}

// 厂商 → 官方域名映射，供 Clearbit Logo API 拼接
// Clearbit Logo API：https://logo.clearbit.com/{domain}
// 返回 128×128 PNG，适配所有主流品牌，稳定的公共服务，无需 API Key
var manufacturerDomain = map[string]string{
	"Apple":         "apple.com",
	"Samsung":       "samsung.com",
	"Xiaomi":        "mi.com",
	"Huawei":        "huawei.com",
	"Honor":         "hihonor.com",
	"OPPO":          "oppo.com",
	"vivo":          "vivo.com",
	"iQOO":          "iqoo.com",
	"OnePlus":       "oneplus.com",
	"Motorola":      "motorola.com",
	"Google":        "google.com",
	"Sony":          "sony.com",
	"LG":            "lg.com",
	"HMD / Nokia":   "nokia.com",
	"ASUS":          "asus.com",
	"Lenovo":        "lenovo.com",
	"realme":        "realme.com",
	"TCL":           "tcl.com",
	"Infinix":       "infinixmobility.com",
	"TECNO":         "tecno-mobile.com",
	"Microsoft":     "microsoft.com",
	"HTC":           "htc.com",
	"Alcatel":       "alcatelmobile.com",
	"nubia":         "nubia.com",
	"ZTE":           "ztedevices.com",
	"Meizu":         "meizu.com",
	"BlackBerry":    "blackberry.com",
	"Archos":        "archos.com",
	"Wiko":          "wikomobile.com",
	"Luvo":          "luvomobile.com",
	"Doogee":        "doogee.cc",
	"Ulefone":       "ulefone.com",
	"CUBOT":         "cubot.net",
	"BLU":           "bluproducts.com",
	"Panasonic":     "panasonic.com",
	"Panasonic KX":  "panasonic.com",
	"Sharp":         "sharp.co.jp",
	"Kyocera":       "kyocera-mobile.com",
	"Hisense":       "hisense.com",
	"Gionee":        "gionee.com",
	"Coolpad":       "coolpad.com",
	"Micromax":      "micromaxinfo.com",
	"Lava":          "lavamobiles.com",
	"Karbonn":       "karbonnmobiles.com",
	"Intex":         "intex.in",
	"Amazon":        "amazon.com",
	"Azumi":         "azumimobile.com",
	"Cherry Mobile": "cherrymobile.com",
	"OUKITEL":       "oukitel.com",
	"Blackview":     "blackview.hk",
	"Umidigi":       "umidigi.com",
	"Fairphone":     "fairphone.com",
	"Nothing":       "nothing.tech",
	"Vsmart":        "vsmart.net",
	"Lephone":       "lephone.com",
	"Lyf":           "lyf.com",
	"itel":          "itel-india.com",
	"Vestel":        "vestel.com",
	"Prestigio":     "prestigio.com",
	"INOI":          "inoimobile.com",
	"HP":            "hp.com",
	"Azpen":         "azpentech.com",
	"Hampoo":        "hampoo.com",
	"CHUWI":         "chuwi.com",
	"Olivetti":      "olivetti.com",
	"Klipad":        "klipad.fr",
}

// manufacturerIconURL 使用 Google S2 Favicon 代理返回品牌图标
//   - Clearbit Logo API (logo.clearbit.com) 已于 2025 年停用
//   - 选型：Google S2 免费、稳定、全球可达、支持 sz 参数（推荐 sz=128 获得高清 PNG）
//   - 未知厂商返回空字符串，前端自动降级为占位图标
//
// URL 形如：https://www.google.com/s2/favicons?domain=apple.com&sz=128
func manufacturerIconURL(manufacturer string) string {
	domain, ok := manufacturerDomain[manufacturer]
	if !ok || domain == "" {
		return ""
	}
	return "https://www.google.com/s2/favicons?domain=" + domain + "&sz=128"
}

// parseDeviceIdentifiersKT 解析 kotlin 源，提取所有 put(id, name) 映射
// 通过 object 块声明判定平台：Ios → ios，AndroidX / AndroidLowercaseX → android
func parseDeviceIdentifiersKT(src string) []devicedomain.CreateInput {
	if src == "" {
		return nil
	}
	items := make([]devicedomain.CreateInput, 0, 32768)
	scanner := bufio.NewScanner(strings.NewReader(src))
	// 默认 64KB 的缓冲可能不够大（某些单行很长），提升到 1MB
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	currentPlatform := ""
	depth := 0 // 跟踪花括号层级，识别 object 结束

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// object 声明：object Ios { / object AndroidA { / object AndroidLowercaseA {
		if strings.HasPrefix(trimmed, "object ") {
			name := strings.TrimPrefix(trimmed, "object ")
			// name 形如 "Ios {" 或 "AndroidA {"
			name = strings.TrimSuffix(strings.TrimSpace(name), "{")
			name = strings.TrimSpace(name)
			switch {
			case name == "Ios":
				currentPlatform = devicedomain.PlatformIOS
			case strings.HasPrefix(name, "Android"):
				currentPlatform = devicedomain.PlatformAndroid
			default:
				currentPlatform = ""
			}
			depth = 1
			continue
		}
		if currentPlatform == "" {
			continue
		}

		// 简单的 { } 计数，遇到 object 结束重置
		openCount := strings.Count(line, "{")
		closeCount := strings.Count(line, "}")
		depth += openCount - closeCount

		if matches := devicePutRegex.FindAllStringSubmatch(line, -1); len(matches) > 0 {
			for _, m := range matches {
				if len(m) < 3 {
					continue
				}
				identifier := strings.TrimSpace(m[1])
				name := strings.TrimSpace(m[2])
				if identifier == "" || name == "" {
					continue
				}
				manufacturer := guessManufacturer(currentPlatform, identifier, name)
				items = append(items, devicedomain.CreateInput{
					Platform:            currentPlatform,
					Identifier:          identifier,
					MarketingName:       name,
					Manufacturer:        manufacturer,
					ManufacturerIconURL: manufacturerIconURL(manufacturer),
				})
			}
		}

		if depth <= 0 {
			currentPlatform = ""
		}
	}
	return items
}
