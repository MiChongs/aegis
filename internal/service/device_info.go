package service

import (
	"context"
	"strings"
	"sync"
	"time"

	devicedomain "aegis/internal/domain/device"
)

// 设备字典在用户接口里的运行时映射。
//
// 会话、登录记录、扫码登录发起端都只保存客户端上报的原始信息，展示时经 Describe
// 按字典现查。列表一次几十条、同一台设备反复出现，查询结果按「平台 + 标识」缓存，
// 未命中也缓存（否则每个没收录的型号每次都打一次库）。字典有写入时整体失效；
// 多实例部署下其他实例最多滞后一个 TTL。

const (
	deviceLookupTTL      = 10 * time.Minute
	deviceLookupCapacity = 20000
)

type deviceLookupEntry struct {
	item    *devicedomain.MarketingName
	expires time.Time
}

type deviceLookupCache struct {
	mu    sync.RWMutex
	items map[string]deviceLookupEntry
}

func newDeviceLookupCache() *deviceLookupCache {
	return &deviceLookupCache{items: map[string]deviceLookupEntry{}}
}

func (c *deviceLookupCache) get(key string) (*devicedomain.MarketingName, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	return entry.item, true
}

func (c *deviceLookupCache) put(key string, item *devicedomain.MarketingName) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// 容量到顶就整体清空：字典查询是纯读缓存，清空的代价只是下一轮重新查库，
	// 用不着为它维护一条 LRU 链
	if len(c.items) >= deviceLookupCapacity {
		c.items = map[string]deviceLookupEntry{}
	}
	c.items[key] = deviceLookupEntry{item: item, expires: time.Now().Add(deviceLookupTTL)}
}

func (c *deviceLookupCache) reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = map[string]deviceLookupEntry{}
	c.mu.Unlock()
}

// cachedLookup 带缓存的字典查询。查询出错时不缓存、按未命中处理。
func (s *DeviceMarketingService) cachedLookup(ctx context.Context, platform, identifier string) *devicedomain.MarketingName {
	key := platform + "\x00" + identifier
	if item, ok := s.cache.get(key); ok {
		return item
	}
	item, err := s.Lookup(ctx, platform, identifier)
	if err != nil {
		return nil
	}
	s.cache.put(key, item)
	return item
}

// lookupPlatforms 只有安卓与苹果有字典。已知是其中之一时先查它再查另一个
// （有的客户端把平台报错），已知是别的平台时不查，未知时安卓优先。
func lookupPlatforms(platform string) []string {
	switch platform {
	case devicedomain.PlatformIOS:
		return []string{devicedomain.PlatformIOS, devicedomain.PlatformAndroid}
	case devicedomain.PlatformAndroid:
		return []string{devicedomain.PlatformAndroid, devicedomain.PlatformIOS}
	case "":
		return []string{devicedomain.PlatformAndroid, devicedomain.PlatformIOS}
	}
	return nil
}

// Match 在字典里找这台设备：依次以原始型号、已保存的名称、设备 ID 为标识。
func (s *DeviceMarketingService) Match(ctx context.Context, platform string, keys ...string) *devicedomain.MarketingName {
	if s == nil || s.pg == nil {
		return nil
	}
	candidates := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		// 字典标识都很短；超长的是 UA 或拼接出来的描述，查了也白查
		if key == "" || len(key) > 64 {
			continue
		}
		dup := false
		for _, existing := range candidates {
			if existing == key {
				dup = true
				break
			}
		}
		if !dup {
			candidates = append(candidates, key)
		}
	}
	for _, plat := range lookupPlatforms(devicedomain.NormalizePlatform(platform)) {
		for _, key := range candidates {
			if item := s.cachedLookup(ctx, plat, key); item != nil && strings.TrimSpace(item.MarketingName) != "" {
				return item
			}
		}
	}
	return nil
}

// Describe 把一台设备的原始信息映射成对外的 Info。s 为 nil 时只做不查字典的兜底，
// 调用方因此不必判空。
//
// 字典依次以原始型号、设备代号、已保存的名称、设备 ID 为键查询。客户端上报的补充信息
// （品牌、系统、App 版本等）原样给出；厂商在命中字典时以字典为准。
func (s *DeviceMarketingService) Describe(ctx context.Context, in devicedomain.DescribeInput) devicedomain.Info {
	model := strings.TrimSpace(in.Model)
	name := strings.TrimSpace(in.Name)
	extra := in.Extra.Normalize()
	platform := devicedomain.NormalizePlatform(in.Platform)
	if platform == "" {
		platform = devicedomain.PlatformFromUserAgent(in.UserAgent)
	}
	info := devicedomain.Info{
		Identifier:   model,
		Platform:     platform,
		Manufacturer: extra.Manufacturer,
		Brand:        extra.Brand,
		Codename:     extra.Codename,
		OS:           extra.OS,
		OSVersion:    extra.OSVersion,
		AppVersion:   extra.AppVersion,
	}

	if item := s.Match(ctx, platform, model, extra.Codename, name, in.DeviceID); item != nil {
		info.Name = item.DisplayName()
		info.Identifier = item.Identifier
		info.Platform = item.Platform
		info.MarketingName = item.MarketingName
		if strings.TrimSpace(item.Manufacturer) != "" {
			info.Manufacturer = item.Manufacturer
		}
		info.ManufacturerIconURL = item.ManufacturerIconURL
		info.DeviceImageURL = item.DeviceImageURL
		info.DictionaryID = item.ID
		info.Matched = true
		info.Source = devicedomain.InfoSourceDictionary
		return info
	}
	switch {
	case model != "" && (name == "" || name == model):
		// 没收录的型号：带上厂商，「Xiaomi 2312DRA50C」比一串型号码好认
		info.Name = withManufacturer(extra.Manufacturer, model)
		info.Source = devicedomain.InfoSourceClient
	case name != "":
		info.Name = name
		info.Source = devicedomain.InfoSourceClient
	default:
		if guess := devicedomain.GuessFromUserAgent(in.UserAgent); guess != "" {
			info.Name = guess
			info.Source = devicedomain.InfoSourceUserAgent
		} else {
			info.Source = devicedomain.InfoSourceUnknown
		}
	}
	return info
}

// withManufacturer 「厂商 型号」，型号里已含厂商时不重复。
func withManufacturer(manufacturer, model string) string {
	manufacturer = strings.TrimSpace(manufacturer)
	if manufacturer == "" || strings.HasPrefix(strings.ToLower(model), strings.ToLower(manufacturer)) {
		return model
	}
	return manufacturer + " " + model
}

// DescribeRef 同 Describe，返回指针，便于直接挂到可选字段上。
func (s *DeviceMarketingService) DescribeRef(ctx context.Context, in devicedomain.DescribeInput) *devicedomain.Info {
	info := s.Describe(ctx, in)
	return &info
}

// describeFromMetadata 从审计记录的 metadata 里还原设备：登录事件的载荷整体落在 metadata 里，
// 新记录带 device / device_model / device_platform，旧记录只有 device_id 与 user_agent。
func (s *DeviceMarketingService) describeFromMetadata(ctx context.Context, metadata map[string]any, deviceID, userAgent string) *devicedomain.Info {
	text := func(key string) string {
		if v, ok := metadata[key].(string); ok {
			return v
		}
		return ""
	}
	var extra devicedomain.Extra
	if raw, ok := metadata["device_extra"].(map[string]any); ok {
		field := func(key string) string {
			v, _ := raw[key].(string)
			return v
		}
		extra = devicedomain.Extra{
			Manufacturer: field("manufacturer"), Brand: field("brand"), Codename: field("codename"),
			OS: field("os"), OSVersion: field("osVersion"), AppVersion: field("appVersion"),
		}
	}
	return s.DescribeRef(ctx, devicedomain.DescribeInput{
		Model:     text("device_model"),
		Platform:  text("device_platform"),
		DeviceID:  firstNonEmpty(deviceID, text("device_id")),
		Name:      text("device"),
		UserAgent: firstNonEmpty(userAgent, text("user_agent")),
		Extra:     extra,
	})
}
