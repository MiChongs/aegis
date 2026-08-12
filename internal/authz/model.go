package authz

import (
	"github.com/casbin/casbin/v2/model"
)

// modelText 全平台唯一的 Casbin 模型。
//
// 与被它替换掉的那两个模型的差别，就是这次重构的全部意义：
//
//	旧平台模型  m = g(r.sub, p.sub) && r.obj == p.obj
//	旧组织模型  m = g(r.sub, p.sub, r.dom) && (p.dom == "*" || p.dom == r.dom) && (p.obj == "*" || p.obj == r.obj)
//	现在        m = g(r.sub, p.sub) && keyMatch(r.dom, p.dom) && keyMatch(r.obj, p.obj)
//
// 逐项说明为什么是这三段：
//
//   - `g(r.sub, p.sub)` —— 二元角色继承。旧平台模型里 `g` 也在，但**从来没有人往里加过边**：
//     `admin_roles.base_role` 这一列存了「继承自哪个角色」，却只被拿去画关系图。
//     现在它是真的继承：`custom_x` 继承 `app_operator` 就自动拥有后者的全部权限，
//     后者加一个权限点，前者跟着有。
//     继承不带域 —— 「A 角色包含 B 角色」是角色自身的性质，与租户无关；
//     带上域会让同一个角色在不同应用里含义不同，那已经不是角色了。
//
//   - `keyMatch(r.dom, p.dom)` —— 域按前缀通配匹配。`*` 的策略对所有域成立
//     （内置角色的权限就是这样一份"到处都一样"的定义），`app:*` 可以表达
//     「所有应用，但不含平台级」，`app:5` 精确到一个应用。
//     旧写法 `p.dom == "*" || p.dom == r.dom` 只有"全部"和"精确"两档。
//
//   - `keyMatch(r.obj, p.obj)` —— 权限点按前缀通配匹配。`ticket:*` 一条策略顶九条，
//     `*` 表示不受权限点约束。旧写法是字符串全等，于是每加一个权限点，
//     每个该有它的角色都要在代码里补一行。
//
// 效果表达式 `some(allow) && !some(deny)` 是 Casbin 的 allow-and-deny：
// 显式拒绝压倒放行。旧模型只有 `some(allow)`，**表达不了任何形式的拒绝** ——
// 要收回某人一个能力只能把他从角色里整个摘掉。
//
// 注意 `p` 的第四列 `eft` 必须显式写在策略行里（Casbin 不会给缺省值），
// 因此 Engine 的写入口一律补齐这一列，adapter 读到三列的历史行也会补成 allow。
const modelText = `
[request_definition]
r = sub, dom, obj

[policy_definition]
p = sub, dom, obj, eft

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))

[matchers]
m = g(r.sub, p.sub) && keyMatch(r.dom, p.dom) && keyMatch(r.obj, p.obj)
`

// newModel 构造模型实例。keyMatch 是 Casbin 内置函数，无需注册。
func newModel() (model.Model, error) {
	return model.NewModelFromString(modelText)
}
