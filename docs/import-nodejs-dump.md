# 旧用户系统（Node.js）数据导入指南

`import-dump` 命令直接解析旧系统的 **mysqldump 文件**导入用户，无需启动临时 MySQL 实例。

## 快速开始

```bash
# 1. 试运行：只解析统计，不写库（确认行数与表名）
go run ./cmd/server import-dump backup.sql --appid 10001 --password 'Tmp@2026' --dry-run

# 2. 正式导入
go run ./cmd/server import-dump backup.sql --appid 10001 --password 'Tmp@2026'

# 旧库多应用混存时，只导其中一个应用的用户
go run ./cmd/server import-dump backup.sql --appid 10001 --source-appid 10000 --password 'Tmp@2026'
```

需要的环境变量与服务端一致（`POSTGRES_DSN` 等，见 `.env.example`）；**不需要** `LEGACY_MYSQL_DSN`。

## 参数

| 参数 | 默认 | 说明 |
|---|---|---|
| `<dump.sql>` | — | mysqldump 文件路径（位置参数，必填） |
| `--appid` | — | 导入到哪个 Aegis 应用（必填） |
| `--password` | — | 统一密码（必填，见下） |
| `--table` | `user` | dump 中的用户表名 |
| `--source-appid` | `0` | 仅导入 dump 中该 appid 的行；0 = 全部 |
| `--require-password-change` | `true` | 首次登录强制改密 |
| `--dry-run` | `false` | 只解析统计，不写库 |
| `--limit` | `0` | 最多导入条数；0 = 不限 |
| `--concurrency` | `4` | 写库并发 |

## 行为约定

- **密码**：旧系统密码哈希与 Aegis（bcrypt）不兼容，**不保留**。所有导入用户统一设置为
  `--password` 指定的密码（bcrypt 哈希只计算一次），并默认 `password_change_required = true`
  —— 用户首次登录后必须改密。
- **用户 ID**：重新分配（Postgres 序列），与现有用户永不冲突。旧 `id` / 旧 `appid` 记入
  `profile.extra`（`importSource=nodejs_mysqldump, legacyId, legacyAppid, importedAt`）备查。
- **冲突**：目标应用下账号已存在 → 跳过并计数（命令幂等，可中断后重复执行）。
- **保留字段**：昵称 / 头像 / 邮箱 / 手机 / 角色 / 设备标识 / customId / 邀请关系 / 注册 IP·ISP·省市·时间 /
  封禁原因 / 积分 / 经验 / VIP 到期（`vip_time=999999999` 归一化为 2099-12-31）/ 启用状态 / 原建号时间。
- **三方登录**：`open_qq` / `open_wechat` 重建为新应用下的 OAuth 绑定，移动端 QQ/微信直登无缝衔接。
- **邀请码**：优先沿用旧码，撞库自动重生成；customId 撞库时置空重试。

## dump 文件要求

标准 `mysqldump` 输出即可（含 `CREATE TABLE` 与 `INSERT`）：

```bash
mysqldump -u root -p --default-character-set=utf8mb4 旧库名 user > backup.sql
```

解析器支持：扩展 INSERT（单语句多行）、显式列清单、`\'` 与 `''` 转义、NULL、条件注释
`/*!...*/`、`--` 行注释、库名前缀 `` `db`.`user` ``、字符串内的分号/括号。
若 dump 不含 `CREATE TABLE`（如 `--no-create-info`），需保证 INSERT 携带列清单
（`mysqldump --complete-insert`）。

列名按旧系统表结构识别（与 `internal/repository/legacymysql` 一致）：
`id, appid, account, name, avatar, email, phone, enabled, disabledEndTime, vip_time, role,
markcode, open_qq, open_wechat, integral, experience, register_ip, register_time,
register_province, register_city, register_isp, reason, parent_invite_account, invite_code,
customId, customIdCount, created_at`。缺列按零值处理，多余列忽略。

## 导入后验收

```bash
# 单元测试（dump 解析器）
go test ./internal/service/ -run 'TestStreamDump|TestParseCreate|TestStatementSplit|TestSourceTable'

# 抽查（psql）
SELECT count(*) FROM users WHERE appid = 10001;
SELECT extra->>'legacyId', account FROM user_profiles p JOIN users u ON u.id = p.user_id
 WHERE extra->>'importSource' = 'nodejs_mysqldump' LIMIT 5;
```

客户端验证：用任一导入账号 + 统一密码登录 → 服务端返回正常会话，且安全概览中
`passwordChangeRequired = true`（Voyage App 改密入口：账号安全 → 修改密码）。

## 注意

- 统一密码会出现在 shell 历史中，建议导入完成后让用户尽快改密，并清理历史（`history -d`）。
- 时间字段按 UTC 解析；若旧库存的是东八区本地时间，注册时间会整体偏移 8 小时（仅展示性字段，
  如需校正可在导入前 `sed` 处理或告知我加 `--tz` 偏移参数。
- 大文件（百万级）建议先 `--dry-run` 确认行数，再正式导入；解析为流式，内存占用与文件大小无关。
