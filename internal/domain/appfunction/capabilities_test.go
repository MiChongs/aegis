package appfunction

import (
	"errors"
	"strings"
	"testing"
)

var errUnbalanced = errors.New("大括号不平衡")

// 目录本身的自洽性。它同时驱动服务端校验、SDK 绑定、控制台勾选框与编辑器类型，
// 任何一条不变量被破坏都会在其中某一处静默失效。
func TestCapabilityCatalogIsWellFormed(t *testing.T) {
	t.Parallel()

	validGroups := map[string]struct{}{
		CapGroupIdentity: {}, CapGroupAsset: {}, CapGroupState: {},
		CapGroupReach: {}, CapGroupAudit: {}, CapGroupIntel: {},
		CapGroupEgress: {}, CapGroupLegacy: {},
	}
	validRisks := map[string]struct{}{RiskLow: {}, RiskMedium: {}, RiskHigh: {}}

	seen := make(map[string]struct{})
	for _, capability := range CapabilityCatalog() {
		if _, duplicate := seen[capability.Key]; duplicate {
			t.Errorf("能力键重复：%s", capability.Key)
		}
		seen[capability.Key] = struct{}{}

		if _, ok := validGroups[capability.Group]; !ok {
			t.Errorf("%s 的分组 %q 不在已知分组里 —— 控制台会把它渲染成一个没有标题的组",
				capability.Key, capability.Group)
		}
		if _, ok := validRisks[capability.Risk]; !ok {
			t.Errorf("%s 的风险档 %q 无效", capability.Key, capability.Risk)
		}
		if capability.Label == "" || capability.Hint == "" {
			t.Errorf("%s 缺少 Label 或 Hint —— 勾选框上会是一片空白", capability.Key)
		}
		if capability.Deprecated {
			// 废弃能力不绑定任何对象，因此绝不能带类型声明：
			// 带了就会在编辑器里提示一个运行时根本不存在的成员
			if capability.Declaration != "" {
				t.Errorf("%s 已废弃却仍带类型声明", capability.Key)
			}
			continue
		}
		if capability.Declaration == "" {
			t.Errorf("%s 没有类型声明 —— 勾上它编辑器里也不会出现任何成员", capability.Key)
		}
		if capability.ReplacedBy != "" {
			if _, ok := CapabilityByKey(capability.ReplacedBy); !ok {
				t.Errorf("%s 指向的替代能力 %s 不存在", capability.Key, capability.ReplacedBy)
			}
		}
	}
}

// Members 是静态分析器反查「这行代码需要哪项能力」的唯一依据。
//
// 它与 Declaration 是同一件事的两种写法（一份给编译器、一份给人看），
// 两者漂移的表现极难发现：分析器认不出 `aegis.points.add`，于是
// 「没声明 points.write」这条错误再也不会被报出来，发布照过、调用时 TypeError。
func TestCapabilityMembersAppearInDeclaration(t *testing.T) {
	t.Parallel()

	for _, capability := range CapabilityCatalog() {
		if capability.Deprecated {
			continue
		}
		if len(capability.Members) == 0 {
			t.Errorf("%s 没有登记 Members —— 静态分析器认不出用到它的代码", capability.Key)
			continue
		}
		for _, member := range capability.Members {
			// 命名空间成员在声明里长成 `get(` / `ban(`；
			// 根成员长成 `kv: AegisKV` / `fetch(url`。
			if strings.Contains(capability.Declaration, member+"(") ||
				strings.Contains(capability.Declaration, member+":") ||
				strings.Contains(capability.Declaration, member+"<") {
				continue
			}
			t.Errorf("%s 登记了成员 %q，但类型声明里找不到它", capability.Key, member)
		}
	}
}

// 反查表的两个方向都要对：认得出需要哪项能力，也认得出「这个成员不存在」。
func TestCapabilitiesForMemberResolvesBothDirections(t *testing.T) {
	t.Parallel()

	cases := []struct {
		root, member string
		wantKnown    bool
		wantNeeded   []string
	}{
		{"points", "add", true, []string{CapPointsWrite}},
		{"user", "get", true, []string{CapUserRead}},
		{"user", "ban", true, []string{CapUserWrite}},
		// 裸取命名空间无从判断要读还是要写，两项都算数
		{"user", "", true, []string{CapUserRead, CapUserWrite}},
		{"kv", "", true, []string{CapKVRead, CapKVWrite}},
		{"fetch", "", true, []string{CapHTTPFetch}},
		// 免声明成员：认得，但不需要任何能力
		{"crypto", "sha256", true, nil},
		{"decimal", "add", true, nil},
		// 命名空间对但成员名拼错：算「不认识」，由调用方按成员报错
		{"points", "increase", false, nil},
		{"nosuch", "", false, nil},
	}

	for _, testCase := range cases {
		needed, known := CapabilitiesForMember(testCase.root, testCase.member)
		if known != testCase.wantKnown {
			t.Errorf("aegis.%s.%s known=%v，期望 %v",
				testCase.root, testCase.member, known, testCase.wantKnown)
		}
		if strings.Join(needed, ",") != strings.Join(testCase.wantNeeded, ",") {
			t.Errorf("aegis.%s.%s 需要 %v，期望 %v",
				testCase.root, testCase.member, needed, testCase.wantNeeded)
		}
	}
}

// 免声明成员的清单必须与 SDKDeclaration 的 AegisSDK 头部一致：
// 少一项，静态分析器会把一个真实存在的成员报成「SDK 上没有」。
func TestBaseSDKMembersMatchDeclaration(t *testing.T) {
	t.Parallel()

	declaration := SDKDeclaration(nil)
	for _, member := range BaseSDKMembers() {
		if strings.Contains(declaration, "\n  "+member+"(") ||
			strings.Contains(declaration, "\n  "+member+":") {
			continue
		}
		t.Errorf("免声明成员 %q 不在零能力时生成的类型里", member)
	}
}

// 旧能力名必须映射到等价的新能力上，否则存量函数升级后会静默少一项能力。
func TestNormalizeCapabilitiesMapsLegacyNames(t *testing.T) {
	t.Parallel()

	got := NormalizeCapabilities([]string{legacyCapUserProfileRead, CapKVRead, CapKVRead, " "})
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, CapUserRead) {
		t.Errorf("旧能力 %s 应映射出 %s，实际 %v", legacyCapUserProfileRead, CapUserRead, got)
	}
	if strings.Count(joined, CapKVRead) != 1 {
		t.Errorf("重复能力应被去重，实际 %v", got)
	}
	for _, key := range got {
		if key == "" {
			t.Errorf("空白项应被剔除，实际 %v", got)
		}
	}
}

// 同一命名空间由多项能力共同组成（user.read 出 get、user.write 出 ban）。
// 拼错会在同一个接口里产生两个 `user:` 成员，TypeScript 报重复声明之后
// **整份类型静默失效** —— 表现是编辑器补全突然什么都没有了。
func TestSDKDeclarationMergesNamespaces(t *testing.T) {
	t.Parallel()

	declaration := SDKDeclaration([]string{CapUserRead, CapUserWrite})
	if count := strings.Count(declaration, "\n  user: {"); count != 1 {
		t.Fatalf("aegis.user 应只声明一次，实际 %d 次：\n%s", count, declaration)
	}
	for _, member := range []string{"get(userId?: number)", "ban(reason: string"} {
		if !strings.Contains(declaration, member) {
			t.Errorf("合并后的 user 命名空间缺少成员 %q", member)
		}
	}
	if err := checkBraceBalance(declaration); err != nil {
		t.Errorf("生成的 .d.ts 括号不平衡：%v", err)
	}
}

// kv.read 与 kv.write 贡献的是同一行成员与同一份接口，必须按文本去重。
func TestSDKDeclarationDeduplicatesKV(t *testing.T) {
	t.Parallel()

	declaration := SDKDeclaration([]string{CapKVRead, CapKVWrite})
	if count := strings.Count(declaration, "kv: AegisKV;"); count != 1 {
		t.Errorf("kv 成员应只出现一次，实际 %d 次", count)
	}
	if count := strings.Count(declaration, "declare interface AegisKV "); count != 1 {
		t.Errorf("AegisKV 接口应只定义一次，实际 %d 次", count)
	}
}

// 未声明的能力绝不能出现在类型里：编辑器提示什么，运行时就必须绑定什么。
func TestSDKDeclarationOmitsUndeclaredCapabilities(t *testing.T) {
	t.Parallel()

	declaration := SDKDeclaration([]string{CapUserRead})
	for _, absent := range []string{"points: {", "wallet: {", "fetch(url", "kv: AegisKV"} {
		if strings.Contains(declaration, absent) {
			t.Errorf("未声明的能力不应出现在类型里，但找到了 %q", absent)
		}
	}
	// 免声明的基础能力必须始终存在
	for _, present := range []string{"crypto: AegisCrypto;", "time: AegisTime;", "config: AegisConfig;"} {
		if !strings.Contains(declaration, present) {
			t.Errorf("基础能力 %q 应始终存在", present)
		}
	}
}

// 模板必须自带它需要的能力，否则「选了模板、写完脚本、发现调不通」。
func TestScriptTemplatesDeclareTheirCapabilities(t *testing.T) {
	t.Parallel()

	for _, template := range ScriptTemplates() {
		if template.Source == "" || template.Title == "" {
			t.Errorf("模板 %s 缺少标题或正文", template.Key)
		}
		if !strings.Contains(template.Source, "function handle(ctx)") {
			t.Errorf("模板 %s 必须包含 handle 入口", template.Key)
		}
		declared := map[string]struct{}{}
		for _, key := range template.Capabilities {
			if !IsKnownCapability(key) {
				t.Errorf("模板 %s 声明了未知能力 %s", template.Key, key)
			}
			declared[key] = struct{}{}
		}
		// 正文里用到的命名空间必须都被声明过
		for _, capability := range CapabilityCatalog() {
			if capability.Deprecated || capability.Namespace == "" {
				continue
			}
			usage := "aegis." + capability.Namespace + "."
			if !strings.Contains(template.Source, usage) {
				continue
			}
			if _, ok := declared[capability.Key]; ok {
				continue
			}
			// 同一命名空间可能由别的能力提供，逐一确认没有任何一项被声明
			provided := false
			for _, other := range CapabilityCatalog() {
				if other.Namespace != capability.Namespace {
					continue
				}
				if _, ok := declared[other.Key]; ok {
					provided = true
					break
				}
			}
			if !provided {
				t.Errorf("模板 %s 用到了 %s 却没有声明对应能力", template.Key, usage)
			}
		}
	}
}

func checkBraceBalance(source string) error {
	depth := 0
	for _, char := range source {
		switch char {
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return errUnbalanced
			}
		}
	}
	if depth != 0 {
		return errUnbalanced
	}
	return nil
}
