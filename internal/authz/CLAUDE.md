# internal/authz — 授权引擎

> 面包屑：[Aegis](../../CLAUDE.md) › internal/authz

## 职责

平台**唯一**的授权判定引擎，同时拥有整套授权语义：权限词汇、角色定义、
路由映射、策略存储、判定与解释。

| 文件 | 内容 |
|---|---|
| `model.go` | Casbin 模型（一份，全平台共用） |
| `keys.go` | 主体 / 域 的命名空间构造函数 —— **拼这些字符串的地方只有这一处** |
| `catalog.go` | 权限点词汇表（约 90 个权限点 + 分组 + 中文名） |
| `roles.go` | 内置角色定义 → `BuiltinPolicies()` |
| `routes.go` | 路由 → (权限点, 作用域) 规则表 |
| `engine.go` | `Engine`：判定 / 展开 / 策略维护 / 重载 |
| `adapter.go` | Casbin `persist.Adapter`（读 `authz_policies` 表） |
| `watcher.go` | Casbin `persist.Watcher`（NATS 广播，跨实例同步） |

## 为什么会有这个包

重构前，授权语义散在三处、彼此不知道对方存在：

| 位置 | 做什么 | 问题 |
|---|---|---|
| `service/admin_service.go` | 平台 RBAC | Casbin 模型退化成 map 查表（`r.obj == p.obj`），策略只在内存 |
| `service/org_access_control.go` | 组织 RBAC | **第二个** enforcer、**第二套**模型，语义与上面不同 |
| `middleware/admin.go` | 路由 → 权限点 | 250 行 switch + 不锚定的 `Contains` + 后缀匹配 |

三处的共同后果是「非常乱 + 不能灵活配置」：

- **策略不落库。** 进程一重启回到编译期的样子；多实例部署时改一次角色只有
  处理那次请求的实例知道，表现为"改完发现只是偶尔生效"。
- **内置角色不可编辑。** 部署方一个字都改不了，只能建自定义角色重配一整套。
- **表达不了拒绝。** 要收回某人一项能力，只能把他从角色里整个摘掉。
- **表达不了通配。** 每加一个权限点，每个该有它的角色都要在代码里补一行。
- **`base_role` 只是装饰。** 这一列存了继承关系，却只被拿去画关系图 ——
  控制台上标着「继承自应用运营管理员」的角色，实际一个继承来的权限都没有。
- **加一个权限点要改三个包**，且没有任何机制保证三处对得上。

## 模型

```
[request_definition]  r = sub, dom, obj
[policy_definition]   p = sub, dom, obj, eft
[role_definition]     g = _, _
[policy_effect]       e = some(where (p.eft == allow)) && !some(where (p.eft == deny))
[matchers]            m = g(r.sub, p.sub) && keyMatch(r.dom, p.dom) && keyMatch(r.obj, p.obj)
```

三个维度全部走 `keys.go` 的构造函数，**不允许在别处拼这些字符串** ——
主体与域是安全边界的一部分，把 `app:5` 写成 `app5` 的表现是静默放行或静默拒绝，
而不是编译错误。

| 维度 | 取值 |
|---|---|
| 主体 `sub` | `role:<key>`（平台/应用角色）、`admin:<id>`（具体某人）、`orgrole:<key>`、`orgrole:<orgID>:<key>` |
| 域 `dom` | 请求侧：`platform` / `app:N` / `org:N`；策略侧另可用 `*`、`app:*` |
| 客体 `obj` | 权限点，支持结尾通配：`ticket:*` / `*` |
| 效果 `eft` | `allow` / `deny` |

`platform` 取一个不可能与 `app:` / `org:` 前缀相撞的字面量是有意的：
匹配函数是 `keyMatch`，平台域若取 `*` 或空串，一条 `app:*` 的策略会连平台级请求
一起放行 —— 那正是「应用管理员给自己解封」那一类越权。

## 判定链路

```
AdminService.Authorize(access, permission, appID)
  ├ 超管短路（IsSuperAdmin 是 DB 上的一列，每次请求现查）
  ├ 权限点为空 → 放行（细粒度下沉到 service 层，如工单）
  ├ subjectsFor：admin:<id> + 该作用域下生效的角色（scopeMatches 过滤）
  ├ Engine.Decide → 显式拒绝优先 → 逐主体 EnforceEx
  └ 临时权限（带过期时间，DB 现查）
```

两处刻意**不进策略表**，理由相同 —— 策略是带缓存的，而这两者的价值恰恰在于即时性：

- **「谁有哪个角色、绑在哪个应用上」**（`admin_assignments`）：每次请求随会话现查。
  灌进策略表会引入"撤销了角色但还能用一段时间"的窗口。
- **临时权限**（`admin_temp_permissions`）：它的全部意义就是"到点自动失效"，
  放进缓存意味着过期时刻起还有一段时间它仍然有效。

**显式拒绝跨主体生效**，这一点实现上有个坑：Casbin 的 deny-override 只在
单次 `Enforce`（单个主体及其继承链）内成立。照搬会让「禁止某人删工单」
这条规则只要那个人再有一个别的角色就失效 —— 因此引擎单独扫一遍拒绝策略。
拒绝策略在多数部署里是零条，空集直接返回，热路径上不产生额外开销。

## 策略来源（`authz_policies.source`）

| source | 谁维护 | 启动时 |
|---|---|---|
| `builtin` | 代码里的内置角色定义 | **整组重刷** |
| `custom` | 自定义角色 CRUD | 不动 |
| `override` | 人工对**任意**角色（含内置）的增减 | 不动 |
| `grant` | 直接授予/禁止到某个管理员 | 不动 |
| `org` | 组织角色 | 不动 |

`builtin` 重刷是为了让「升级后给 app_admin 加了个权限点」能到达所有既有部署；
代价是这一档不接受人工编辑，出口是 `override` —— 加一条 allow 是扩权，
加一条 deny 是在不动角色定义的前提下砍掉一项能力。这条分工线让
「跟随版本升级」和「按部署定制」同时成立。

写入**一律走 `Engine` 的方法**，Casbin 的 AutoSave 已关闭：它的
`AddPolicy`/`RemovePolicy` 传不了 `source` 与 `owner`，经它写进去的行没有归属，
下次重刷内置策略时既不会被更新也不会被清理，最后沉淀成谁也不敢删的幽灵授权。

## 路由规则表

按段锚定匹配：`:param` 吃一段，`*` 吃剩余任意段数（**含零段**）。
不用 Casbin 的 `util.KeyMatch2` 是因为它把 `/*` 换成 `/.*`，于是 `/a/b/*`
匹配不到 `/a/b` 本身，而"集合根"与"集合下的子资源"几乎总是同一组权限。

顺序即优先级（从具体到宽泛），两条测试守着：

- `TestRouteRulesMatchBaseline` —— `testdata/route_permissions.json` 是重构**之前**
  用旧 switch 跑出来的快照，逐条钉住 941 条真实路由的判定结果不变。
  以后要改某条路由的权限，改规则表的同时更新基线里那一行，
  diff 会把改动摆在评审者面前。
- `TestNoUnreachableRouteRule` —— 没有哪条规则被上面的规则完全遮住。
  被遮住的规则不报错，只是永不生效（旧实现里就沉淀了三段这样的死代码）。

另有 `TestBuiltinRolePermissionsUnchanged`：`roles.go` 把一批显式权限列表换成了
`content:*` 这类前缀通配，通配写宽一格（比如把 app_admin 的工单列表写成 `ticket:*`）
就会多出一个 `ticket:delete`，而这种越权不会有任何报错。
`testdata/builtin_roles.json` 是改动前的快照，逐个角色比对展开结果。

## 管理面

| 接口 | 用途 |
|---|---|
| `GET /api/admin/system/authz/model` | 权限目录 + 内置角色 + 路由规则表（模型自述） |
| `GET /api/admin/system/authz/policies` | 引擎**内存里**当前生效的策略 |
| `GET /api/admin/system/authz/policies/subject` | 某个主体名下的策略行 |
| `POST /api/admin/system/authz/explain` | 「某人在某作用域下能不能做某事，为什么」 |
| `POST /api/admin/system/authz/roles/override` | 角色的人工增减（超管） |
| `PUT /api/admin/system/authz/admins/:adminId/grants` | 按人授予/禁止（超管） |
| `POST /api/admin/system/authz/reload` | 手动重载（超管，排障用） |

`explain` 是这次补上的最有用的一件工具：一次 403 的排查以前要翻四个地方
（角色定义、权限点常量、路由映射、作用域），且四处都在代码里。

写操作一律叠加 `RequireSuperAdmin`：一条 allow 策略就能给自己提权，
把这个入口交给普通管理员等于让整套 RBAC 可以被它管辖的对象自行改写。

策略快照给的是**引擎里的**而不是库里的：两者不一致（比如某个实例重载失败）
恰恰是最需要看见的那种故障，而查库看不出来。
