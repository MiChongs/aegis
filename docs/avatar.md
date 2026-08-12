# 头像服务

> 面包屑：[Aegis](../CLAUDE.md) › docs/avatar

## 这次修的是什么

**症状**：用户自己上传的头像，过一阵子就没了。第三方登录带来的头像和 Gravatar
从来不出问题 —— 只有「自定义上传」这条链路会。

**根因是两跳，第二跳才是致命的那一跳。**

原来的链路是：上传成功后把 `storage://{configID}/{objectKey}` 写进
`user_profiles.avatar`，每次读资料时现签一个 **30 分钟**的代理票据
（Redis 里的一次性 key），把 `/api/storage/proxy/{ticket}` 交给客户端。

```
上传 ──► storage://3/avatars%2F…jpg   （落库，永久）
读资料 ──► /api/storage/proxy/{ticket} （出网，30 分钟后失效）
```

第一跳的问题是：**交出去的是一个会过期的地址，而客户端理所当然会把它存下来。**
控制台存进 localStorage（zustand persist）、移动端存进本地库、邮件正文里嵌成
`<img>`、中间还可能有 CDN。半小时后这些副本全部变成死链。

第二跳是：**有客户端会把读回来的整份资料原样 PUT 回来。** 读-改-写是最常见的
表单写法 —— 拉一份资料、改个昵称、把整个对象提交回去。于是那个临时地址被写回
数据库，覆盖掉唯一那份 `storage://` 引用：

```
GET  /me/profile  → { "avatar": "https://api…/api/storage/proxy/abc123", … }
PUT  /me/profile  ← { "avatar": "https://api…/api/storage/proxy/abc123", … }
                                 ↑ 这一行把库里的引用永久覆盖掉了
```

这一步之后头像不是过期，是**永久丢失**：库里再也没有任何东西指向那个对象。
而且它只发生在自定义上传上，因为只有它才产生 `storage://` 引用。

## 现在的做法：地址编码的是「谁」，不是「哪个对象」

```
落库：storage://{configID}/{objectKey}      （不变，存量数据零迁移）
出网：/api/avatars/{ownerToken}?v={version}  （永久地址）
```

`ownerToken` 是对主体（`u{appID}.{userID}` 或 `a{adminID}`）的签名短串，
**与头像内容无关**。因此：

| | 结果 |
|---|---|
| 换了头像 | 地址不变（`v` 变，用于破缓存） |
| 客户端存了两年前的地址 | 今天点开拿到的是这个人今天的头像 |
| 客户端把地址原样 PUT 回来 | 被判回「不修改」，库里的引用毫发无损 |
| 从来没传过头像 | 返回服务端生成的默认头像，**不是 404，也不是空字符串** |

服务端解析时**不看** `v`：那个参数只用来破客户端与 CDN 的缓存。这正是
「地址不会失效」的全部含义 —— 没有任何输入能让这个地址解析失败。

签名不是为了保密（头像本来就是半公开的，工单、成员列表里到处都在显示），
是为了挡住按 ID 遍历把全站用户头像刷走。

### 免登录是前提，不是疏漏

`GET /api/avatars/:token` 不在任何鉴权组里。它出现在 `<img src>` 里，而浏览器
加载图片不会带 `Authorization` 头；它也出现在邮件正文里，那里根本没有登录态。
防遍历靠地址自带的签名。

跨应用隔离在解析时做：令牌里带着 `appid`，与用户真实归属对不上就当作不存在 ——
少了这一条，A 应用的管理员拿 B 应用用户的 ID 就能拼出一个能取图的地址。

### 缓存

判据是请求里的 `v` 与当前版本是否一致：

| 情况 | Cache-Control |
|---|---|
| `v` == 当前版本 | `public, max-age=31536000, immutable` |
| `v` 缺失或过期 | `public, max-age=300, stale-while-revalidate=86400` |

新鲜的地址确实指向不变的内容，可以放心长期缓存；旧地址的内容会跟着当前头像走，
不能让它被长期钉住。`ETag` 带上尺寸档（`"{version}-{size}"`）—— 同一版本的
64 与 512 是两份不同的字节，共用一个 ETag 会让 CDN 把小图当大图返回。

## 三道闸门：写回、伪造、协议

`NormalizeAvatarInput`（`internal/service/avatar_link.go`）是资料更新链路上
唯一的头像入口，用户端与管理端都走它：

| 客户端提交的值 | 结论 | 不这么做会怎样 |
|---|---|---|
| `…/api/storage/proxy/…` | 保持原值不动 | 亲手销毁唯一的引用（就是上面那个 bug） |
| `…/api/avatars/…` | 保持原值不动 | 把展示地址当引用存下来 |
| `storage://…`（与当前不一致） | **拒绝**（40092） | 任何登录用户都能把头像设成别人的私有文件，再从头像地址上读出来 |
| `http(s)://…` | 放行 | —— |
| 其它协议 | 拒绝（40093） | `javascript:` 进了 `<a href>` 是可执行的 |

## 自愈：已经被写坏的那些行

读资料时如果发现库里那一列是空的或者明显是临时票据地址，就把引用找回来并回写。
两条线索按顺序试：

```
raw 是空 / 是 /api/storage/proxy/…
  ├─► ① avatar_assets 的 active 行            （本次改动之后上传的，带全套元数据）
  └─► ② storage_objects 里键含 avatars/apps/{appID}/users/{userID}/ 的最近一条
        （本次改动**之前**上传的 —— 主体信息一直编码在对象键里，只是从来没人读它）
  ──► 回写 storage:// 引用（尽力而为，失败只是下次再来一遍）
```

第二条是必须的：`avatar_assets` 这次才建，而丢头像的恰恰是升级前那批用户。
只认第一条的话，他们唯一的出路是重新上传一次 —— 而他们根本不知道自己需要这么做
（界面上只是没有头像，不像出了错）。收编出来的记录没有变体与 blurhash
（那要重新解码原图，不值得在一次读资料的路径上做），用户下次换头像时自然补齐。

**只在这两种输入下动手。** 其它形态一律不碰 —— 用户可能就是想把头像设成某个外链，
自作主张改回去比丢了更糟。

这意味着**不需要迁移脚本**：受影响的用户下一次打开资料页时头像就回来了。
只有一种情况救不回来 —— 对象本身已经从存储桶里被删掉了。

## 图像处理管线

用 [disintegration/imaging](https://github.com/disintegration/imaging)。
每一步都有一个「不做会怎样」：

| 步骤 | 不做会怎样 |
|---|---|
| `DecodeConfig` 先量尺寸（5000 万像素 / 单边 20000 闸门） | 一张 32000×32000 的 PNG 解出来是 4GB，一个请求打死一台机器 |
| EXIF 方向纠正（`AutoOrientation`） | 所有竖拍的自拍在网页上是躺着的 |
| 重新编码 | 头像里带着拍摄地点的经纬度，谁下载谁就知道用户家在哪 |
| 方形裁剪 | 交一张 800×450 给客户端，等于让每一端各自决定裁哪一块 |
| 多尺寸变体（64/128/256/512） | 列表页每一行都在下载 512×512 |
| blurhash + 主色 | 弱网下头像位置先空着再"跳"出来 |

几处刻意的取舍：

- **编码格式由像素决定，不由声明决定。** 有非不透明像素 → PNG，否则 JPEG(q=88)。
  一律 JPEG 会把带透明的 logo 的背景压成黑块；一律 PNG 会让一张普通自拍从
  40KB 涨到 400KB。上传时声明的 content-type 和文件后缀都可能与内容不符。
- **各尺寸档从方图重采样，不从上一档链式缩小。** 链式会把每次重采样的损失累加到
  最小的那一档上，而最小的那一档恰恰是列表页里出现最多次的。
- **动图够小就原样留着**（≤2MB 且边长 ≤512），超限才拍平成静态首帧，
  并在上传结果里 `flattened: true` 如实告知。悄悄拍平的表现是「我传的动图怎么不动了」，
  用户完全无从判断是不是自己传错了。
- **变体键由基准键派生**（`{base}_{size}{ext}`）。万一资产表那一行丢了，
  光凭引用也能把所有尺寸找回来。
- **变体传失败不让整次上传失败。** 主图已经在了，缺的那一档在取图时自动回落到
  更大的一档，用户完全无感。
- **取变体只向上取。** 向下取会把 64px 的图拉到 256px 显示，糊得比多下几 KB 明显得多。

裁剪框走 multipart 的 `crop_x` / `crop_y` / `crop_width` / `crop_height`，
坐标基于**EXIF 纠正之后**的图像 —— 那正是用户在预览里看到的那张。
越界的值会被夹回图像范围（越界来自前端的缩放换算误差，是常态而非攻击）。

## 默认头像

没有自定义头像时服务端自己画，确定性生成（同一个人任何时候画出来都一样）。

老做法是拼一个 `weavatar.com` 的地址交给客户端，那意味着：每个用户的邮箱哈希
被送到第三方、内网部署的实例上头像一律加载失败、以及那个服务哪天改了域名平台
这边毫无察觉。而当用户既没传头像也没有邮箱/手机号时，连那个地址都拼不出来，
`avatar` 字段直接是空字符串 —— 于是每个客户端各自决定画什么。

| 样式 | 说明 |
|---|---|
| `identicon`（默认） | 5×5 左右对称几何图案 + HCL 空间派生的渐变底色 |
| `initials` | 首字母单字图；**中文名取拼音首字母**（内嵌字体只有拉丁字形，直接画汉字是豆腐块） |
| `gravatar` | 回到老做法，302 到 `AVATAR_GRAVATAR_BASE_URL` |
| `none` | 不生成，头像地址留空 |

底色在 **HCL 空间**取（固定 C/L、只让 H 随哈希转），不是直接摆弄 RGB：
RGB 里等距的两个值人眼看到的明度差可能相去甚远，随机出来的配色会有一部分
深得看不见白色文字。

对称是几何图案好看的关键：随机撒点看起来像噪声，镜像之后大脑会把它读成一个「图形」。

日文汉字在 Unicode 里同属 Han，因此也会走拼音分支（日 → R）。没有语言标注时
这是可接受的取舍 —— 一个读音不对的字母仍然是个字母，而豆腐块看起来就是
「这个系统坏了」。假名与西里尔落到「取不到」，回落几何图案。

## 接口

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/avatars/:token` | 永久头像地址，免登录。`?s=` 选尺寸，`?v=` 破缓存 |
| POST | `/api/v1/apps/{appKey}/me/avatar` | 上传（multipart，可带 `crop_*`） |
| DELETE | `/api/v1/apps/{appKey}/me/avatar` | 移除头像，回到默认头像 |
| GET | `/api/v1/apps/{appKey}/me/avatar/history` | 头像历史（含当前） |
| POST | `/api/v1/apps/{appKey}/me/avatar/restore` | 恢复历史里的某一张 |
| POST/DELETE | `/api/admin/profile/avatar` | 管理端同上（上传 / 移除） |
| GET | `/api/avatar/:hash` | 老的第三方头像 302 入口，保留兼容 |

**移除头像这个入口以前不存在。** 更新资料时空串的语义是「不修改」，
于是传过一次头像之后就再也回不到默认头像了。

上传结果的形状（`upload`）：

```json
{
  "avatar": "https://api.example.com/api/avatars/dTEuNDI.AbCdEf12?v=9f3a1c22",
  "reference": "storage://3/avatars%2Fapps%2F1%2Fusers%2F42%2F…",
  "view": {
    "url": "https://api.example.com/api/avatars/dTEuNDI.AbCdEf12?v=9f3a1c22",
    "kind": "custom",
    "version": "9f3a1c22",
    "blurhash": "LEHV6nWB2yk8pyo0adR*.7kCMdnj",
    "dominantColor": "#7c5a3f",
    "sizes": [64, 128, 256, 512]
  }
}
```

`upload.avatar` 仍是**字符串**（老客户端读的就是它）。新增的结构一律挂在
`upload.view` 上 —— 把 `avatar` 改成对象会让所有已发布的 App 在上传成功后
把一个 `[object Object]` 当成图片地址去加载。

## 配置

全部留空即可得到一套完整可用的头像服务。见 [.env.example](../.env.example)
的「头像服务」一节，配置结构在 `internal/config.AvatarConfig`。

改这里的任何一项都**不会**让已经流出去的地址失效 —— 这是设计前提：
头像地址会被客户端、邮件、CDN 长期持有，一个会因为改配置而失效的地址等于没有地址。

`AVATAR_SIZES` **只能加不能减**：减掉一档不会让存量资产少一个文件，
只会让请求那一档的客户端回落到更大的一档，白白多下字节。

## 代码索引

| 文件 | 职责 |
|---|---|
| `internal/domain/avatar/types.go` | 领域类型：Owner / Asset / Variant / View |
| `internal/service/avatar_service.go` | 解析、上传、移除、历史、取图 |
| `internal/service/avatar_link.go` | 主体令牌签名、永久地址、**写回闸门** |
| `internal/service/avatar_pipeline.go` | 解码 / EXIF / 裁剪 / 多尺寸 / 编码 / blurhash |
| `internal/service/avatar_identity.go` | 默认头像生成 |
| `internal/repository/postgres/avatar_repository.go` | `avatar_assets` 读写 |
| `internal/transport/http/avatar_handlers.go` | 路由处理与缓存头 |
| `migrations/postgres/000078_avatar_assets.up.sql` | 资产表 |
