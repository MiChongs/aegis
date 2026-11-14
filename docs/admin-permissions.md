# 管理员分级与权限体系

控制台「用户与权限」的后端设计。代码在 `internal/authz`（引擎、权限树、路由规则）与
`internal/service/admin_{tier,roles,authorization,policy_service}.go`。

## 1. 身份档位

| 档位 | 列 | 能管谁 |
|---|---|---|
| 系统管理员 `system` | `is_system_admin = is_super_admin = TRUE` | 所有人（含其他系统管理员） |
| 超级管理员 `super` | `is_super_admin = TRUE` | 普通管理员 |
| 管理员 `admin` | 两列皆假 | 档位更低的人（即其他普通管理员），且受授权上限约束 |

规则（`service/admin_tier.go` 的 `manageDecision`）：

- 只能管理档位**严格低于**自己的人；系统管理员之间例外，可以互相管理。
- 谁都不能停用 / 启用自己，不能修改自己的授权、档位、个人授权、临时权限。只能下线自己的会话。
- 超级管理员停用不了系统管理员，也停用不了自己；也不能停用、改授权另一位超级管理员。
- 只有系统管理员能授予或收回超级管理员、系统管理员身份；最后一位活跃的系统管理员不能被停用或降级。
- 守卫覆盖的入口：启用停用、角色授权、档位、个人授权、临时权限、强制下线（含按会话号撤销）、
  角色成员增删、代理授权。权限点只决定能不能进入口，档位决定能不能对这个人动手。

超管与系统管理员都不走权限点判定（`IsSuperAdmin` 短路），所以既有的几十处超管判定无需改动；
系统管理员只是多回答「谁能管超管」这一件事。

### 生产迁移

`000153_admin_tiers_and_role_model` 随每次启动重放（全部幂等）：

1. 加 `is_system_admin` 列与「系统管理员必为超管」的 CHECK；
2. **没有任何系统管理员时**，把最早创建的活跃超级管理员提升为系统管理员（引导超管总是第一个账号）；
3. `admin_roles` 加状态、排序、数据权限；`admin_role_permissions` 加 `effect`（allow / deny）。

启动时 `EnsureSystemAdmin` 再兜一次底（配置了 `ADMIN_BOOTSTRAP_ACCOUNT` 时优先提升它）。
现场恢复用运维命令（绕过会话，只能在能直连库的机器上执行）：

```bash
aegis-server promote-system-admin <account>
```

## 2. 权限树

`authz/tree.go` 定义「目录 → 菜单 → 操作」三级（对标若依的 M / C / F），一份定义同时驱动：

- 角色编辑器的勾选树（落进策略的只有操作节点的代码）；
- 路由权限规则表（每个操作节点声明它覆盖的接口，规则表由此生成并按特异性排序）；
- 生效权限快照（`/api/admin/auth/me`）与控制台按钮显隐。

约 270 个操作节点覆盖全部 1100+ 条管理接口。命名约定：

- 读类操作一律以 `:read` 结尾（只读 MCP 密钥靠这个后缀放行用 POST 承载的查询）；
- 新代码落在它所替换的旧代码的前缀家族里（替换 `app:user:read` 的写成 `app:user:…`）；
- 一个代码要么是操作节点，要么是旧权限点（兼容组），不能两者都是。

新增管理接口：在某个操作节点的 `routes(...)` 里登记。忘了登记的表现是 40315，不是静默放行；
`TestEveryAdminRouteRegistered` 与 `TestEveryPermissionReachable` 守着。

## 3. 模型：RBAC + 资源层级

```
m = g(r.sub, p.sub) && keyMatch(r.dom, p.dom) && objMatch(r.obj, p.obj)
```

`objMatch` 是注册给 Casbin 的函数（与 `authz.Implies` 同一段代码）：前缀通配 + 资源层级。
细分权限点是旧粗粒度权限点的成员（`app:lottery:read ∈ app:read`），于是：

- 持有旧代码的内置角色、历史自定义角色、override、按人授权、MCP 密钥、临时权限**原样生效**，
  生产库里的策略一行不改；
- 旧的前缀通配（`app:*`、`system:admin:*`）同样覆盖拆出来的细分项；
- 旧代码上的 deny 压住它名下的每个细分项。

`authz/compat_test.go` 用拆分前的快照（`testdata/legacy_*.json`）逐条比对：
每个旧权限点及其每一级前缀通配、每个内置角色，在新体系下能进的接口与以前**一项不多、一项不少**。

不用 Casbin 的 g2 表达层级：g2 只认精确边，`system:admin:*` 这种旧通配匹配不到前缀不同的
`system:announcement:read`，拆分后会静默失效。

## 4. 角色

| | 内置角色 | 自定义角色 |
|---|---|---|
| 定义 | 代码（`authz/roles.go`），每次启动重刷 | `admin_roles` |
| 编辑功能权限 | 额外允许 / 禁止写进 override | 允许 / 禁止写进角色定义 |
| 继承 | — | `base_role`（防成环） |
| 状态 | 恒启用 | 可停用：授权关系保留，但不贡献任何权限与可见范围 |
| 数据权限 | 按授权绑定 | 按授权绑定 / 全部应用 / 本人创建 / 指定应用 |

数据权限只对应用级角色有意义，在会话组装时展开（`resolveAssignments`）：

- `assigned`：分配时选的应用；
- `all`：展开成 `AllApps` 分配——每个应用里都生效，但**不满足平台级请求**；
- `created`：该管理员 `apps.created_by` 为自己的应用；
- `custom`：角色上指定的应用。

下游（判定、应用列表过滤、工单可见范围、治理视图）拿到的已是具体应用。
多实例下，角色元数据随策略广播刷新（`Engine.OnReload` → `SetupRoleSync`）。

## 5. 授权上限

非超管只能授予自己此刻持有的权限（`EnsureCanGrant` / `EnsureCanAssignRole`）：

- 模式先展开成细分权限点再逐项核对，通配绕不过去；
- 不带应用的授予要求操作者「在任意应用都有」（全局角色或「全部应用」），只在某个应用持有的不能授给全局；
- 编辑角色只核对**新增**的允许项与新继承的父角色；改授权只核对**新增**的分配。

## 6. 二次把关的变化

处理函数里原有的「仅超管」判定（`requireSuperAdminSession`、`RequireSuperAdmin`）现在认可
**已按具体权限点通过 AdminAccess 的请求**（`middleware.AuthorizedPermission`）。于是被授予
「出海网关：查看」的人能打开网关页；只挂了 AdminAuth、或路由不要求权限点的接口仍然只放行超管。
拆分前这些路由都要求 `system:admin:manage`，线上只有超管持有它，所以升级当下没有任何人的权限变化。

## 7. 接口

| 接口 | 权限点 |
|---|---|
| `GET /api/admin/system/authz/catalog` | 已登录即可（静态目录） |
| `GET /api/admin/system/authz/permission-routes?code=` | 角色权限：查看 |
| `GET /api/admin/system/roles/:roleKey` | 角色权限：查看 |
| `PUT /api/admin/system/roles/:roleKey/permissions` | 角色权限：编辑 |
| `PUT /api/admin/system/roles/:roleKey/status` | 角色权限：编辑 |
| `POST /api/admin/system/roles/:roleKey/copy` | 角色权限：新增 |
| `GET/POST /api/admin/system/roles/:roleKey/members` | 查看 / 编辑 |
| `DELETE /api/admin/system/roles/:roleKey/members/:adminId?appid=` | 角色权限：编辑 |
| `GET /api/admin/system/admins/:adminId` | 管理员：查看 |
| `GET/PUT /api/admin/system/authz/admins/:adminId/grants` | 管理员：查看 / 个人授权 |
| `PUT /api/admin/system/admins/:adminId/access`（`tier` 字段） | 管理员：角色授权 |

错误码：40392 停用自己 · 40393 改自己的授权 · 40394 只有系统管理员能管系统管理员 ·
40395 只有系统管理员能管超管 · 40396 不能管更高档位 · 40397 只有系统管理员能授予超管 / 系统管理员 ·
40398 最后一位系统管理员不能降级 · 40391 最后一位系统管理员 / 超管不能停用 · 40399 超出授权上限。
