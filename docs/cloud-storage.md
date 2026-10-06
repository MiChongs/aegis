# 用户云存储

> 面包屑：[Aegis](../CLAUDE.md) › docs/cloud-storage

每个用户在应用内有一块私有空间，按「命名空间 / 键」存放任意文档：收藏夹、偏好设置、
草稿、游戏存档……接入方自己决定命名空间怎么分。它是**应用级**能力：开关、配额、
写到哪个存储配置都在控制台 `/apps/{appKey}?tab=cloud-storage` 里按应用配置。

## 内容放在哪

内容**不进数据库**。每次写入都经 `StorageService.UploadForApp` 传到应用解析出的存储配置
（与 `/storage/upload` 同一套解析：指定了配置名就用那个，否则应用级默认 → 平台级默认），
因此 S3 / MinIO / 阿里云 OSS / 腾讯云 COS / 七牛 / WebDAV / OneDrive / Dropbox /
Google Drive / Azure Blob / 本地磁盘都能直接用，平台治理的「限制存储」也随之生效。

数据库里只有索引与账目：

| 表 | 一行是什么 |
|---|---|
| `app_cloud_storage_configs` | 一个应用的配置 |
| `app_cloud_storage_users` | 一个用户的配额覆盖、冻结状态与用量账目 |
| `app_cloud_items` | 一个（用户, 命名空间, 键），指向当前修订 |
| `app_cloud_item_revisions` | 一次写入，对应存储桶里的一个对象 |

对象键形如 `cloud/{appid}/{userId}/{namespace}/{key}/{时间}-{随机}`：**每个修订一个独立对象**。
覆盖写同一个键会让「上传成功、事务失败」毁掉上一个修订的内容。

修订行记下写入时实际用到的 `storage_config_id`，管理员后来换了存储配置，旧修订照样能从原处读出来。

## 一次写入

```
1. 校验命名空间 / 键 / 编码 / 大小，冻结与明显超额先拦一道
2. 内容传到存储桶                      ← 慢、可能失败，但不持有任何数据库锁
3. 事务：锁用户行 → 锁条目行 → ifRevision → 条目数 → 配额
         → 新修订 → 裁掉超出保留数的旧修订 → 从修订表重算账目
4. 提交后删掉被裁掉的旧对象；事务失败则删掉第 2 步刚传的那个
```

反过来（先事务后上传）要么在事务里等上传、把用户行锁住几秒，要么留下指向不存在对象的修订。
现在的顺序最坏只是在桶里留一个孤儿对象（删除失败时），不会让任何一个修订读不出来。

**锁用户行**把同一个用户的并发写入串起来 —— 否则两次各自没超配额的写入可以一起把配额撑破。
**账目是重算的**，不是加减的：`used_bytes` 等列每次都在事务里从修订表 `SUM` 一遍写回，
加减式计数器漏掉一个分支就会永久漂移。保留这几列只是为了让管理端能按用量排序。

## 并发：修订号

条目的 `revision` 单调递增（进回收站再恢复也不重置）。写入带 `ifRevision`：

| `ifRevision` | 含义 |
|---|---|
| 省略 | 无条件覆盖（只适合一个键只有一台设备在写） |
| `0` | 只在条目不存在（或在回收站里）时创建 |
| `N` | 服务端当前修订必须等于 `N`，否则 `40965` |

冲突之后客户端重新读取、与本地合并、带新的修订号再写。Voyage 的收藏夹同步就是这么做的
（三方合并：上次同步的快照 × 本地 × 云端）。

## 修订、回滚与回收站

- 每个条目保留 `max_revisions` 个修订（含当前），超出的最旧修订在**写入的同一事务里**裁掉
- 回滚是「以某个历史修订的内容生成一个新修订」，历史不改写，回滚本身也能再回滚
- 删除默认进回收站，保留 `trash_retention_days` 天，期间可恢复；对已在回收站里的条目写入会直接复活它
- 保留期为 0 时删除即清除；`permanent=true` 也直接清除
- 过期清理由 API 进程里的后台循环每小时跑一次（多实例同时跑是安全的：逐条进事务并重新确认过期），
  控制台也能手动触发

## 配额

配额计入**全部留存修订**，含回收站里的条目 —— 它们在被清除之前确实占着存储桶。
覆盖写时会被裁掉的旧修订**算作腾出的空间**，否则一个快满的用户连覆盖都写不进去。

单条目上限另受网关请求体约束：JSON 写入（`PUT`）的内容不超过 6 MiB（JSON 请求体上限 8 MiB，
base64 膨胀三分之一），更大的走 multipart 上传（上限 25 MiB）。`/cloud` 下发的 `limits`
里有客户端需要的全部数字。

## 删除对象：存储提供商的新能力

存储中心原本只删索引不删对象（人工管理的文件这样更安全）。云存储每次写入都产生新对象，
只删索引等于让存储桶随写入次数无限增长，所以给提供商补了一个**可选**的 `Delete`
（`storage_provider_delete.go`），十一家全部实现、全部幂等（对象本来不在算成功），
熔断包装同样转发。`TestEveryStorageProviderImplementsDelete` 钉住「每一家都有」。

`StorageService.DeleteObject` 删对象的同时移除存储中心的索引行，存储中心的文件列表与用量统计因此不会一直算着已清除的修订。

## 隐私

- 下载地址一律是**代理票据**（`CreatePrivateObjectLink`），即使应用的存储配置是公开桶 ——
  用户的私有数据不能因为管理员选了公开桶就变成一个永久有效的直链
- 管理端单用户接口挂在 `users/:userId/cloud-storage` 下，与资料、钱包同一组权限（`app:user:*`），
  并且核对路径上的用户确实属于路径上的应用
- 应用级接口（配置 / 概览 / 用户列表 / 清理）归存储权限（`storage:read` / `storage:write`）
- 控制台在目标存储是公开桶时给出提示

## 接口

用户端（网关，需 Bearer）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/cloud` | 状态：开没开、能不能写、用量、限制、命名空间目录 |
| GET | `/cloud/items` | 条目列表（`status=deleted` 看回收站） |
| GET | `/cloud/items/{namespace}/{key}` | 读取（`revision` 读历史修订） |
| PUT | `/cloud/items/{namespace}/{key}` | 写入（JSON 请求体） |
| DELETE | `/cloud/items/{namespace}/{key}` | 删除（`ifRevision` / `permanent`） |
| POST | `…/upload` | multipart 写入 |
| POST | `…/restore` | 从回收站恢复 |
| GET | `…/revisions` | 留存修订 |
| POST | `…/rollback` | 回滚 |
| POST | `…/link` | 短时下载地址 |

`content` 的形态由 `encoding` 决定：`json` 时是任意 JSON 值（原样落盘、原样读回，字段顺序不变），
`text` 时是字符串，`base64` 时是 base64 字符串（标准 / URL 安全、有无 padding 都认）。

管理端：`/api/admin/apps/{appkey}/cloud-storage/{config,stats,users,purge-expired}`，
以及 `/api/admin/apps/{appkey}/users/{userId}/cloud-storage[/items[/{itemId}[/revisions|rollback|restore|link]]]`。

## 错误码

| 码 | 名称 | 何时 |
|---|---|---|
| 40360 | CLOUD_STORAGE_DISABLED | 应用没开启 |
| 40361 | CLOUD_STORAGE_FROZEN | 管理员冻结了该用户（只读） |
| 40362 | CLOUD_NAMESPACE_FORBIDDEN | 限定了命名空间目录，这个不在里面 |
| 40465 | CLOUD_ITEM_NOT_FOUND | 条目不存在（或在回收站里） |
| 40466 | CLOUD_REVISION_NOT_FOUND | 修订不存在或已被裁掉 |
| 40965 | CLOUD_REVISION_CONFLICT | `ifRevision` 与当前修订不一致 |
| 40966 | CLOUD_NOT_IN_TRASH | 恢复一个不在回收站里的条目 |
| 41360 | CLOUD_QUOTA_EXCEEDED | 配额不足 |
| 41361 | CLOUD_ITEM_TOO_LARGE | 单条目超过上限 |
| 41362 | CLOUD_ITEM_LIMIT | 条目数已达上限 |
| 42260–42263 | CLOUD_INVALID_* | 命名空间 / 键 / 内容 / 元数据格式不对 |
| 50260 | CLOUD_CONTENT_UNREADABLE | 存储桶里的对象读不出来 |

## 已知边界

- 用户被删除时，数据库行随外键级联删除，但存储桶里的对象不会跟着删（与头像相同）。
  需要彻底清理时先在控制台「清空云存储」，再删除用户
- 不支持删除的提供商（目前没有）会把对象留在桶里并记日志
