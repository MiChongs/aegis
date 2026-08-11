package security

// 风控中心的自描述目录。
//
// 这份目录是**单一事实源**：后端判定读这里的常量，`/risk/metadata` 把同一份
// 下发给控制台驱动「场景下拉 / 条件类型参数表单 / 表达式变量提示 / 等级图例」。
// 在前端另抄一份枚举会让「后端新增一种条件类型」变成一次静默的漏项 ——
// 规则能存进去，但表单上没有它的参数字段，管理员配出来的是一条永不命中的规则。
//
// 与支付渠道的 `Provider.Describe()`、通知渠道的 `/notify/catalog` 是同一套约定。

// SceneCatalog 场景目录
func SceneCatalog() []CatalogEntry {
	return []CatalogEntry{
		{Value: SceneLogin, Label: "登录", Description: "用户与管理员登录，命中 block/ban 直接拒绝本次登录"},
		{Value: SceneRegister, Label: "注册", Description: "新账号注册，命中 block/ban 直接拒绝建号"},
		{Value: ScenePayment, Label: "支付", Description: "下单与支付前置校验"},
		{Value: SceneInvite, Label: "邀请", Description: "邀请码兑换 / 拉新奖励"},
		{Value: SceneLottery, Label: "抽奖", Description: "抽奖与活动参与"},
		{Value: SceneAPI, Label: "接口", Description: "通用接口调用"},
	}
}

// SceneValues 场景取值集合，供校验使用
func SceneValues() []string {
	entries := SceneCatalog()
	values := make([]string, 0, len(entries))
	for _, e := range entries {
		values = append(values, e.Value)
	}
	return values
}

// LevelCatalogEntries 等级目录（含分数区间，供图表图例与分段着色使用）
func LevelCatalogEntries() []LevelCatalog {
	meta := map[string]CatalogEntry{
		LevelNormal:   {Value: LevelNormal, Label: "正常", Description: "无风险信号", Tone: "neutral"},
		LevelLow:      {Value: LevelLow, Label: "低危", Description: "轻微异常，通常放行", Tone: "info"},
		LevelMedium:   {Value: LevelMedium, Label: "中危", Description: "建议加验证码", Tone: "warning"},
		LevelHigh:     {Value: LevelHigh, Label: "高危", Description: "建议人工复核或拦截", Tone: "danger"},
		LevelCritical: {Value: LevelCritical, Label: "严重", Description: "强烈建议拦截或封禁", Tone: "danger"},
	}
	out := make([]LevelCatalog, 0, len(RiskLevelBands))
	for _, band := range RiskLevelBands {
		out = append(out, LevelCatalog{
			CatalogEntry: meta[band.Level],
			MinScore:     band.MinScore,
			MaxScore:     band.MaxScore,
		})
	}
	return out
}

// ActionCatalog 处置动作目录
func ActionCatalog() []CatalogEntry {
	return []CatalogEntry{
		{Value: ActionPass, Label: "放行", Description: "不做任何干预", Tone: "success"},
		{Value: ActionCaptcha, Label: "验证码", Description: "要求补充人机验证后继续", Tone: "info"},
		{Value: ActionReview, Label: "人工复核", Description: "进入待复核队列，由管理员裁决", Tone: "warning"},
		{Value: ActionBlock, Label: "拦截", Description: "当次请求直接拒绝", Tone: "danger"},
		{Value: ActionBan, Label: "封禁", Description: "拒绝并按时长封禁", Tone: "danger"},
	}
}

// ActionValues 动作取值集合
func ActionValues() []string {
	entries := ActionCatalog()
	values := make([]string, 0, len(entries))
	for _, e := range entries {
		values = append(values, e.Value)
	}
	return values
}

// DeviceTagCatalog 设备风险标签目录
func DeviceTagCatalog() []CatalogEntry {
	return []CatalogEntry{
		{Value: TagNormal, Label: "正常", Tone: "neutral"},
		{Value: TagTrusted, Label: "受信", Description: "人工确认过的可信设备", Tone: "success"},
		{Value: TagSuspicious, Label: "可疑", Tone: "warning"},
		{Value: TagBlocked, Label: "封禁", Tone: "danger"},
	}
}

// IPTagCatalog IP 风险标签目录
func IPTagCatalog() []CatalogEntry {
	return []CatalogEntry{
		{Value: TagNormal, Label: "正常", Tone: "neutral"},
		{Value: TagTrusted, Label: "受信", Description: "人工加白，规则判定时视作干净 IP", Tone: "success"},
		{Value: TagProxy, Label: "代理", Tone: "warning"},
		{Value: TagVPN, Label: "VPN", Tone: "warning"},
		{Value: TagTor, Label: "Tor", Tone: "danger"},
		{Value: TagDatacenter, Label: "机房", Tone: "warning"},
		{Value: TagBot, Label: "Bot", Tone: "danger"},
		{Value: TagBlocked, Label: "封禁", Tone: "danger"},
	}
}

// 条件类型分组
const (
	condGroupFrequency = "频率与速度"
	condGroupDevice    = "设备"
	condGroupClient    = "客户端"
	condGroupNetwork   = "网络与情报"
	condGroupGeo       = "地理"
	condGroupAdvanced  = "高级"
)

var rateDimensionOptions = []CatalogEntry{
	{Value: "ip", Label: "按 IP"},
	{Value: "account", Label: "按账号"},
	{Value: "device", Label: "按设备"},
	{Value: "account_device", Label: "按账号+设备"},
}

// ConditionCatalogEntries 条件类型目录（含参数 schema）。
// 每一项都在 RiskService.evaluateCondition 里有对应的执行点 ——
// 目录里多一条而判定里没有，等于给管理员一个假的防线。
func ConditionCatalogEntries() []ConditionCatalog {
	return []ConditionCatalog{
		{
			Value:         CondIPFrequency,
			Label:         "IP 高频访问",
			Group:         condGroupFrequency,
			Description:   "同一 IP 在窗口内的请求数超过阈值即命中。计数来自 Redis 滑动窗口。",
			RequiresRedis: true,
			Fields: []FieldSchema{
				{Key: "threshold", Label: "窗口内最大请求数", Type: "number", Required: true, Default: 60, Min: new(1.0), Help: "超过该值即命中"},
				{Key: "dimension", Label: "统计维度", Type: "select", Default: "ip", Options: rateDimensionOptions, Help: "决定读取哪一路计数器"},
			},
		},
		{
			Value:         CondRateLimited,
			Label:         "触发限流",
			Group:         condGroupFrequency,
			Description:   "命中平台配置的 GCRA 限流阈值（RISK_RATE_LIMIT_* 环境变量）即命中，无需再配阈值。",
			RequiresRedis: true,
			Fields: []FieldSchema{
				{Key: "dimension", Label: "限流维度", Type: "select", Default: "ip", Options: rateDimensionOptions},
			},
		},
		{
			Value:         CondAccountVelocity,
			Label:         "账号扩散速度",
			Group:         condGroupFrequency,
			Description:   "同一 IP 或设备在窗口内触达的**不同账号数**超过阈值即命中，用于识别撞库与批量注册。基数统计走 Redis HyperLogLog。",
			RequiresRedis: true,
			Fields: []FieldSchema{
				{Key: "dimension", Label: "归集维度", Type: "select", Default: "ip", Options: []CatalogEntry{{Value: "ip", Label: "按 IP"}, {Value: "device", Label: "按设备"}}},
				{Key: "threshold", Label: "不同账号数阈值", Type: "number", Required: true, Default: 5, Min: new(2.0)},
			},
		},
		{
			Value:       CondDeviceNew,
			Label:       "新设备",
			Group:       condGroupDevice,
			Description: "设备首次出现距今不足指定小时数即命中。设备档案由每次评估自动落库。",
			Fields: []FieldSchema{
				{Key: "max_hours", Label: "设备存续时长上限（小时）", Type: "number", Required: true, Default: 1, Min: new(0.0), Help: "首次出现不超过此时长视为新设备"},
			},
		},
		{
			Value:         CondDeviceShared,
			Label:         "设备被多账号共用",
			Group:         condGroupDevice,
			Description:   "同一设备在窗口内出现过的不同账号数超过阈值即命中，用于识别工作室批量操作。",
			RequiresRedis: true,
			Fields: []FieldSchema{
				{Key: "threshold", Label: "不同账号数阈值", Type: "number", Required: true, Default: 3, Min: new(2.0)},
			},
		},
		{
			Value:       CondUABot,
			Label:       "Bot 客户端",
			Group:       condGroupClient,
			Description: "User-Agent 被识别为爬虫 / 机器人即命中，无需额外参数。",
		},
		{
			Value:       CondUAMissing,
			Label:       "缺失或异常 UA",
			Group:       condGroupClient,
			Description: "User-Agent 为空或短于指定长度即命中。正常浏览器与 App 都会带完整 UA。",
			Fields: []FieldSchema{
				{Key: "min_length", Label: "最短长度", Type: "number", Default: 16, Min: new(0.0), Help: "短于该长度视为异常"},
			},
		},
		{
			Value:       CondUADeviceClass,
			Label:       "客户端类型匹配",
			Group:       condGroupClient,
			Description: "命中指定的客户端类型（桌面 / 移动 / 平板 / Bot / 未知）。",
			Fields: []FieldSchema{
				{Key: "classes", Label: "客户端类型", Type: "list", Required: true, Placeholder: "desktop, mobile, tablet, bot, unknown",
					Options: []CatalogEntry{
						{Value: "desktop", Label: "桌面"},
						{Value: "mobile", Label: "移动"},
						{Value: "tablet", Label: "平板"},
						{Value: "bot", Label: "Bot"},
						{Value: "unknown", Label: "未知"},
					}},
			},
		},
		{
			Value:            CondIPProxy,
			Label:            "代理 / VPN",
			Group:            condGroupNetwork,
			Description:      "IP 被判定为代理或 VPN 即命中。可勾选是否把 Tor 与机房 IP 一并计入。",
			RequiresProvider: true,
			Fields: []FieldSchema{
				{Key: "include_tor", Label: "包含 Tor 出口", Type: "bool", Default: true},
				{Key: "include_datacenter", Label: "包含机房 IP", Type: "bool", Default: false, Help: "云服务器出口，误伤率较高"},
			},
		},
		{
			Value:            CondIPReputation,
			Label:            "IP 信誉分",
			Group:            condGroupNetwork,
			Description:      "外部情报源给出的 IP 风险分不低于阈值即命中。未配置情报源时该条件永不命中。",
			RequiresProvider: true,
			Fields: []FieldSchema{
				{Key: "min_score", Label: "信誉分阈值", Type: "number", Required: true, Default: 75, Min: new(0.0), Max: new(100.0)},
			},
		},
		{
			Value:       CondIPCIDR,
			Label:       "IP 网段匹配",
			Group:       condGroupNetwork,
			Description: "请求 IP 落在任一网段内即命中，支持 IPv4 / IPv6 CIDR。",
			Fields: []FieldSchema{
				{Key: "cidrs", Label: "网段列表", Type: "list", Required: true, Placeholder: "10.0.0.0/8, 2001:db8::/32"},
				{Key: "negate", Label: "反向匹配（不在列表内才命中）", Type: "bool", Default: false},
			},
		},
		{
			Value:       CondGeoAnomaly,
			Label:       "归属地异常",
			Group:       condGroupGeo,
			Description: "请求归属国与预期国家不一致即命中。归属地未知时按「不命中」处理，避免情报缺失被当成异常。",
			Fields: []FieldSchema{
				{Key: "expected_country", Label: "预期国家代码", Type: "text", Required: true, Default: "CN", Placeholder: "CN"},
			},
		},
		{
			Value:       CondGeoCountryIn,
			Label:       "归属地在名单内",
			Group:       condGroupGeo,
			Description: "请求归属国在名单内即命中，用于封禁高风险地区。",
			Fields: []FieldSchema{
				{Key: "countries", Label: "国家代码列表", Type: "list", Required: true, Placeholder: "RU, KP, IR"},
			},
		},
		{
			Value:       CondGeoCountryNotIn,
			Label:       "归属地不在名单内",
			Group:       condGroupGeo,
			Description: "请求归属国不在名单内即命中，用于只服务特定地区的业务。",
			Fields: []FieldSchema{
				{Key: "countries", Label: "允许的国家代码", Type: "list", Required: true, Placeholder: "CN, HK, MO, TW"},
			},
		},
		{
			Value:            CondASNIn,
			Label:            "ASN / ISP 匹配",
			Group:            condGroupNetwork,
			Description:      "IP 所属自治域或运营商名称包含任一关键词即命中。",
			RequiresProvider: true,
			Fields: []FieldSchema{
				{Key: "keywords", Label: "ASN / ISP 关键词", Type: "list", Required: true, Placeholder: "AS4134, Amazon, DigitalOcean"},
			},
		},
		{
			Value:       CondTimeWindow,
			Label:       "时段匹配",
			Group:       condGroupAdvanced,
			Description: "请求发生在指定时段内即命中（按平台默认时区计算）。跨零点写法如 23:00–06:00。",
			Fields: []FieldSchema{
				{Key: "start", Label: "起始时间", Type: "time", Required: true, Default: "00:00", Placeholder: "23:00"},
				{Key: "end", Label: "结束时间", Type: "time", Required: true, Default: "06:00", Placeholder: "06:00"},
			},
		},
		{
			Value:       CondCustomExpr,
			Label:       "自定义表达式",
			Group:       condGroupAdvanced,
			Description: "用 expr 表达式自由组合全部环境变量。表达式在保存时即做编译校验，写错的变量名当场报错而不是静默永不命中。",
			Fields: []FieldSchema{
				{Key: "expression", Label: "表达式", Type: "textarea", Required: true,
					Placeholder: "ip_request_count > 100 and device_age_hours < 24"},
			},
		},
	}
}

// ConditionTypeValues 条件类型取值集合
func ConditionTypeValues() []string {
	entries := ConditionCatalogEntries()
	values := make([]string, 0, len(entries))
	for _, e := range entries {
		values = append(values, e.Value)
	}
	return values
}

// VariableCatalogEntries 表达式可用变量目录。
// 与 RiskEvalEnv 的 `expr` 标签一一对应；两边漂移时
// TestRiskExprVariableCatalogMatchesEnv 会立刻失败。
func VariableCatalogEntries() []VariableCatalog {
	return []VariableCatalog{
		{Name: "scene", Type: "string", Group: "请求", Description: "评估场景", Example: `scene == "login"`},
		{Name: "ip", Type: "string", Group: "请求", Description: "客户端 IP"},
		{Name: "device_id", Type: "string", Group: "请求", Description: "客户端上报的设备标识"},
		{Name: "account", Type: "string", Group: "请求", Description: "登录 / 注册使用的账号标识"},
		{Name: "user_agent", Type: "string", Group: "请求", Description: "原始 User-Agent"},
		{Name: "app_id", Type: "int", Group: "请求", Description: "应用 ID，未携带时为 0"},
		{Name: "user_id", Type: "int", Group: "请求", Description: "用户 ID，未登录时为 0"},
		{Name: "hour_of_day", Type: "int", Group: "请求", Description: "请求发生的小时（平台默认时区，0–23）", Example: "hour_of_day >= 2 and hour_of_day <= 5"},
		{Name: "weekday", Type: "int", Group: "请求", Description: "星期几，周日为 0"},

		{Name: "ua_is_bot", Type: "bool", Group: "客户端", Description: "是否被识别为爬虫 / 机器人"},
		{Name: "ua_browser", Type: "string", Group: "客户端", Description: "浏览器名称"},
		{Name: "ua_browser_version", Type: "string", Group: "客户端", Description: "浏览器版本"},
		{Name: "ua_os", Type: "string", Group: "客户端", Description: "操作系统名称"},
		{Name: "ua_os_version", Type: "string", Group: "客户端", Description: "操作系统版本"},
		{Name: "ua_device", Type: "string", Group: "客户端", Description: "设备型号（如 iPhone）"},
		{Name: "ua_device_class", Type: "string", Group: "客户端", Description: "客户端类型：desktop/mobile/tablet/bot/unknown"},
		{Name: "ua_is_mobile", Type: "bool", Group: "客户端", Description: "是否移动端"},
		{Name: "ua_length", Type: "int", Group: "客户端", Description: "User-Agent 字符长度，0 表示未携带"},

		{Name: "ip_request_count", Type: "int", Group: "频率", Description: "当前窗口内同 IP 请求数"},
		{Name: "account_request_count", Type: "int", Group: "频率", Description: "当前窗口内同账号请求数"},
		{Name: "device_request_count", Type: "int", Group: "频率", Description: "当前窗口内同设备请求数"},
		{Name: "account_device_request_count", Type: "int", Group: "频率", Description: "当前窗口内同账号+设备请求数"},
		{Name: "ip_rate_limited", Type: "bool", Group: "频率", Description: "同 IP 是否已触发限流"},
		{Name: "account_rate_limited", Type: "bool", Group: "频率", Description: "同账号是否已触发限流"},
		{Name: "device_rate_limited", Type: "bool", Group: "频率", Description: "同设备是否已触发限流"},
		{Name: "account_device_rate_limited", Type: "bool", Group: "频率", Description: "同账号+设备是否已触发限流"},
		{Name: "ip_accounts_seen", Type: "int", Group: "频率", Description: "窗口内该 IP 触达过的不同账号数"},
		{Name: "device_accounts_seen", Type: "int", Group: "频率", Description: "窗口内该设备触达过的不同账号数"},
		{Name: "account_ips_seen", Type: "int", Group: "频率", Description: "窗口内该账号使用过的不同 IP 数"},

		{Name: "device_age_hours", Type: "float", Group: "设备", Description: "设备首次出现至今的小时数，从未出现过为 0"},
		{Name: "device_seen_count", Type: "int", Group: "设备", Description: "该设备的历史出现次数"},
		{Name: "device_known", Type: "bool", Group: "设备", Description: "设备是否已在档"},
		{Name: "device_risk_tag", Type: "string", Group: "设备", Description: "设备风险标签"},
		{Name: "device_blocked", Type: "bool", Group: "设备", Description: "设备是否已被标记封禁"},

		{Name: "ip_is_proxy", Type: "bool", Group: "网络", Description: "是否代理出口"},
		{Name: "ip_is_vpn", Type: "bool", Group: "网络", Description: "是否 VPN 出口"},
		{Name: "ip_is_tor", Type: "bool", Group: "网络", Description: "是否 Tor 出口"},
		{Name: "ip_is_datacenter", Type: "bool", Group: "网络", Description: "是否机房 / 云服务器 IP"},
		{Name: "ip_risk_score", Type: "int", Group: "网络", Description: "外部情报给出的信誉分（0–100）"},
		{Name: "ip_risk_tag", Type: "string", Group: "网络", Description: "IP 风险标签"},
		{Name: "ip_known", Type: "bool", Group: "网络", Description: "IP 是否已在风险库中"},
		{Name: "ip_trusted", Type: "bool", Group: "网络", Description: "IP 是否被人工加白"},
		{Name: "ip_total_blocks", Type: "int", Group: "网络", Description: "该 IP 的历史拦截次数"},
		{Name: "ip_asn", Type: "string", Group: "网络", Description: "自治域号"},
		{Name: "ip_isp", Type: "string", Group: "网络", Description: "运营商 / 组织名"},

		{Name: "geo_country", Type: "string", Group: "地理", Description: "归属国家代码，未知为空串"},
		{Name: "geo_region", Type: "string", Group: "地理", Description: "归属省 / 州"},
		{Name: "geo_city", Type: "string", Group: "地理", Description: "归属城市"},
		{Name: "geo_known", Type: "bool", Group: "地理", Description: "归属地是否已解析出来"},

		{Name: "extra", Type: "map", Group: "扩展", Description: "调用方透传的自定义字段", Example: `extra["channel"] == "h5"`},
	}
}

// FunctionCatalogEntries 表达式可用的内置函数
func FunctionCatalogEntries() []FunctionCatalog {
	return []FunctionCatalog{
		{Name: "in_cidr", Signature: "in_cidr(ip: string, cidr: string) -> bool", Description: "判断 IP 是否落在指定网段内", Example: `in_cidr(ip, "10.0.0.0/8")`},
		{Name: "any_cidr", Signature: "any_cidr(ip: string, cidrs: []string) -> bool", Description: "判断 IP 是否落在任一网段内", Example: `any_cidr(ip, ["10.0.0.0/8", "192.168.0.0/16"])`},
		{Name: "contains_any", Signature: "contains_any(text: string, keywords: []string) -> bool", Description: "文本是否包含任一关键词（忽略大小写）", Example: `contains_any(ip_isp, ["Amazon", "DigitalOcean"])`},
		{Name: "in_time_window", Signature: "in_time_window(start: string, end: string) -> bool", Description: "当前时刻是否落在时段内，支持跨零点", Example: `in_time_window("23:00", "06:00")`},
	}
}

// ExprSampleEntries 表达式示例
func ExprSampleEntries() []ExprSample {
	return []ExprSample{
		{Title: "高频 + 新设备", Expression: "ip_request_count > 100 and device_age_hours < 24", Note: "同一 IP 高频访问且设备是新的"},
		{Title: "机房 IP 上的批量注册", Expression: "ip_is_datacenter and ip_accounts_seen >= 3", Note: "云服务器出口触达多个账号"},
		{Title: "凌晨来的境外高危 IP", Expression: `hour_of_day >= 1 and hour_of_day <= 5 and geo_country != "CN" and ip_risk_score >= 60`},
		{Title: "被共用的设备", Expression: "device_accounts_seen >= 5 and not device_blocked", Note: "设备被多账号共用但尚未处置"},
		{Title: "排除加白 IP 的代理判定", Expression: "(ip_is_proxy or ip_is_vpn) and not ip_trusted"},
		{Title: "云厂商网段", Expression: `any_cidr(ip, ["13.104.0.0/14", "20.33.0.0/16"])`},
	}
}

// BuildRiskMetadata 组装完整目录
func BuildRiskMetadata() RiskMetadata {
	return RiskMetadata{
		Scenes:         SceneCatalog(),
		Levels:         LevelCatalogEntries(),
		Actions:        ActionCatalog(),
		ConditionTypes: ConditionCatalogEntries(),
		Variables:      VariableCatalogEntries(),
		Functions:      FunctionCatalogEntries(),
		DeviceTags:     DeviceTagCatalog(),
		IPTags:         IPTagCatalog(),
		Samples:        ExprSampleEntries(),
	}
}
