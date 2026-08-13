# sqlc —— 从迁移文件生成类型安全的数据访问代码

> 面包屑：[Aegis](../CLAUDE.md) › docs/sqlc
> 配置文件：[sqlc.yaml](../sqlc.yaml) ｜ 查询目录：[sql/queries](../sql/queries) ｜ 产物：`internal/repository/postgres/sqlcgen`

## 它解决的是哪一类问题

本仓库的数据访问层是手写 pgx SQL（见 [internal/repository](../internal/repository/CLAUDE.md)）。
手写的代价集中在一处：**Scan 目标与真实列类型的对应关系，编译期查不出来**。
`*string` 和 `**string` 都是合法的 `Scan` 参数，写错要等到运行期，
而且往往只在某一类数据上才触发。

最典型的一种是 `LEFT JOIN` 不命中：

```sql
FROM users u
LEFT JOIN vip_trial_claims c ON c.appid = u.appid AND c.user_id = u.id
```

`vip_trial_claims.plan_name` 在建表语句里是 `NOT NULL`，但 JOIN 不命中时整行都是
NULL —— 没领过试用的用户全部走这一支。把它扫进 `string` 的表现是运行期一句
`cannot scan NULL into *string`，而这条链路同时是 `/vip/status`、管理端
`/vip/entitlement` 与远程函数 `aegis.user.get()` 的唯一入口。

sqlc 读的是 `migrations/postgres/` 里的真实建表语句，因此它算得出每一列的可空性，
**包括被 LEFT JOIN 污染成可空的那些列** —— 上面那条查询由它生成时，
`plan_name` 直接就是 `*string`，对不上的写法根本编译不过。

## 定位：渐进接管，不是重写

| | 手写层 | sqlc |
|---|---|---|
| 位置 | `internal/repository/postgres/*.go` | `sql/queries/*.sql` → `sqlcgen/` |
| 适用 | 存量查询、需要动态拼接 where / order by 的列表接口 | 新查询、列固定的读写 |
| 连接 | `*pgxpool.Pool` | 同一个池（`sqlcgen.New(pool)`） |

两者共用同一个 pgxpool，互不干扰。**不要求存量查询迁移** ——
动态拼 SQL（分页列表的可选筛选、`ORDER BY` 白名单）是 sqlc 覆盖不了的形状，
硬套只会得到十几个近乎重复的查询。

## 安装

版本**必须与 CI 一致**（`1.31.1`）：不同版本的生成结果有差异，浮动版本会让 `sqlc diff` 随机变红。

```bash
# Windows（本仓库开发机的做法）
scoop install sqlc

# macOS
brew install sqlc

# 任意平台：GitHub Releases 的预编译二进制
#   https://github.com/sqlc-dev/sqlc/releases/tag/v1.31.1

# 源码安装。1.31 起 SQL 解析走 wazero 跑的 WASM 版 pg_query，
# 不再需要 cgo（本机 CGO_ENABLED=0 实测可用），代价是编译慢、产物约 75MB。
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
```

**不要**把 sqlc 加成 go.mod 的 tool 依赖：它会把上百个模块拖进本仓库的依赖图，
而这些模块与运行时代码毫无关系。

## 命令

```bash
sqlc generate   # 生成代码；改了 migrations/ 或 sql/queries/ 之后必须跑
sqlc vet        # 按 sqlc.yaml 末尾的规则检查每条查询（不连库）
sqlc diff       # 产物与当前 schema/queries 是否一致；不一致退出码非 0
```

CI（`.github/workflows/go-ci.yml` 的 `sqlc` job）跑的是 `vet` + `diff`。
因此**生成产物要提交进仓库** —— 不提交的话 `diff` 无从比对，
而"改了迁移忘了重新生成"的后果是产物里还留着旧列、且照样编译得过。

## 配置里的几个决定

逐条理由写在 [sqlc.yaml](../sqlc.yaml) 的注释里，这里只列结论：

| 选项 | 取值 | 效果 |
|---|---|---|
| `schema` | `./migrations/postgres` | 库结构的事实来源就是迁移目录本身，不另维护 schema.sql（必然漂移） |
| `emit_pointers_for_null_types` | `true` | 可空列 → `*string` / `*int64`，与 `internal/domain` 的可选字段一致 |
| `omit_unused_structs` | `true` | 只生成查询用得到的结构体，不产出 80 张表的模型（那会与领域类型重复一份） |
| `rename.appid` | `AppID` | `appid` 是一个词，切不出 `app`+`id` |
| `overrides` 时间类 | `time.Time` / `*time.Time` | pgx/v5 档默认给 `pgtype.Timestamptz`，且不分可空与否 |
| `overrides` numeric | `string` / `*string` | 与手写层一致：读出来先进 string 再 `decimal.RequireFromString` |

### 两个会让人栽跟头的地方

1. **`db_type` 的写法逐类型不同，写错时不报错、只是静默不生效。**
   时间类要写裸名 `timestamptz`（写 `pg_catalog.timestamptz` 无效），
   numeric 要写 `pg_catalog.numeric`（写 `numeric` 无效）。
   判断有没有生效的唯一办法是生成一次再看产物类型。

2. **规则要在 `sql` 块里逐条登记才会跑。** 只在文件末尾写 `rules:` 定义、
   不在 `sql[].rules` 里列出名字的话，`sqlc vet` 一条都不跑并且返回 0 ——
   看起来是"全部通过"。

### 刻意没有做的事

- **没有 `no-select-star` 规则**：`query.sql` 拿到的是展开后的 SQL，
  sqlc 在解析阶段就把 `*` 换成了完整列名清单，这条规则永远匹配不到。
  一条永不命中的规则比没有更危险。而这件事本来也不需要防 ——
  展开结果会写进产物，改库结构时 `sqlc diff` 直接报出来。
- **没有映射 PostGIS 的 `geometry` / `geography`**：sqlc 不认识它们，会生成
  `interface{}`。这是有意留着的信号 —— 几何列本来就不该被直接 SELECT，
  要坐标写 `ST_X`/`ST_Y`，要图形写 `ST_AsGeoJSON`。
- **`sqlc/db-prepare` 没有默认开启**：它把每条查询拿去真库 `PREPARE` 一次，
  能查出 sqlc 自己解析不了的问题，但要求 CI 起一个装了 PostGIS 的实例。
  本地想跑的话在 `sqlc.yaml` 里临时加 `database: { uri: "${POSTGRES_DSN}" }`。

## 写查询的约定

```sql
-- name: GetUserByID :one
SELECT id, appid, account, password_hash, enabled, created_at
FROM users
WHERE id = $1
LIMIT 1;
```

- `:one` / `:many` / `:exec` / `:execrows` / `:copyfrom` —— 返回形状由注解决定
- **列名写全，不用 `*`**：产物里是展开后的清单，评审时看得见加了哪一列
- 文件按主题切分，与 `internal/repository/postgres/*_repository.go` 对齐
- 需要动态筛选的列表查询**不要**硬塞进来，留在手写层

### UUID 与 IP 取字符串

`uuid` 生成 `pgtype.UUID`、`inet` 生成 `netip.Addr`。要字符串就在查询里转，
与手写层既有写法一致（这样 sqlc 直接判成 `text`）：

```sql
SELECT o.uuid::text, HOST(l.login_ip)
```

## 接进代码

```go
import "aegis/internal/repository/postgres/sqlcgen"

q := sqlcgen.New(pool)               // pool 即 bootstrap 里那个 *pgxpool.Pool
row, err := q.GetUserByID(ctx, userID)
```

分层规矩不变：**只有 repository / service 能碰它，handler 不行**
（见 [CLAUDE.md 全局规范](../CLAUDE.md#全局规范)）。
生成的 `Row` 结构是数据访问层的内部形状，往上仍然要转成 `internal/domain` 的领域类型 ——
把 `sqlcgen.XxxRow` 直接 JSON 出网等于把库结构变成对外 API 契约。
