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
		CapGroupReach: {}, CapGroupAudit: {}, CapGroupEgress: {}, CapGroupLegacy: {},
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
