# 个性装扮

头像框、资料卡背景、昵称样式、勋章、号码铭牌、会员铭牌（见「铭牌」）。外观与素材**全部由服务端下发**：运营在控制台设计器里做一份设计稿（图层 + 关键帧 + 素材），
发布后客户端按设计稿实时绘制；客户端不内置任何一件装扮。

代码：`internal/domain/cosmetic`（规则、规格、模板、判定）、`internal/repository/postgres/cosmetic_repository.go`、
`internal/service/cosmetic_service.go`、`internal/transport/http/cosmetic_handlers.go`；迁移 000161（000176 加铭牌槽位）。

## 三张账

| 表 | 是什么 |
|---|---|
| `cosmetic_items` | 一件装扮：槽位、编码、名称、稀有度、系列、获取方式、**已发布设计稿** `design` 与**草稿** `draft`、版本号、预览图、是否隐藏 |
| `user_cosmetics` | 持有记录，与功能解锁同构：来源、来源单号、天数（0 永久）、起止、收回 |
| `user_cosmetic_equips` | 佩戴：每人每槽位一行，勋章按 `position`（0 起）多行 |

另有素材库 `cosmetic_assets`（按 sha256 去重，同一应用的装扮共用）、引用关系 `cosmetic_item_assets`（被引用的素材不能删）、
版本 `cosmetic_revisions`（每次发布一份，保留最近 50 份）、应用设置 `cosmetic_settings`（开关、勋章位数 1–5）。

## 能不能戴：唯一判据

`cosmetic.Eligibility(item, facts, now)`：

1. 装扮未上架或从未发布 → `unavailable`
2. 有仍生效的持有记录 → 能
3. 否则看获取方式：`free` 能；`vip` 有效会员（可限定档位）；`level` 等级达到；`feature` 持有功能；`grant` → `not_owned`（持有过但已结束时装扮中心显示 `expired`）

**佩戴时判一次、读取时再判一次。** 会员到期、限时装扮过期、下架、被收回之后，佩戴行不必清理，`/cosmetics/me` 读出来就没有它。
没有任何后台扫描，也不存在「过期了但还挂着」的窗口。会员事实用的是 `vipEntitlementFactsSQL` + `vip.Evaluate`，与 `/vip/status`、
积分商城「已拥有」同一份。

## 获得：各系统的发放分支

全部走仓储的 `grantCosmeticTx`：锁用户行 → 装扮必须已上架且发布过（否则整笔回滚）→ 已永久持有则 `AlreadyPermanent`（不记新段，
发放方按「已拥有」处理）→ 限时顺延（新的一段从这件装扮仍生效的最晚终点开始）。

奖励目录里的形状统一为 `{type: "cosmetic", refId: 装扮编号, amount: 天数(0 永久)}`。接入的系统：积分商城、抽奖、卡密、物品发放、
邀请（排行榜、推广活动复用邀请的奖励目录）、管理员授予。来源常量见 `cosmetic.Source*`。

**发放时佩戴**：奖励可以写成 `{type: "cosmetic", refId, amount, equip: true}`（管理端授予是 `POST /users/:userId/grant {cosmeticId, days, equip}`）。
`grantCosmeticTx` 发放成功后（含 `AlreadyPermanent`）在同一事务里把这件戴到它的槽位上：单戴槽位替换原有的佩戴；勋章已戴着就不动，
否则放到第一个空位（没有佩戴行的位置），满了就不戴。佩戴前照常用 `Eligibility` 判一次，不能戴只发不戴、不报错。戴上后结果带
`equipped`（勋章还有 `position`），发 `cosmetic.changed`（action=equipped）；物品发放的记录里这一项 `equipped = true`、说明后缀「（已戴上）」。
目前物品发放与管理端授予接了这个开关；积分商城、抽奖、卡密、邀请的奖励解码时忽略这个字段（等同 false）。

装扮中心的「获取渠道」从上架中的积分商城商品与进行中的抽奖活动里反查（奖励里 `type=cosmetic` 且 `refId` 是这件）。

## 接口

### 用户端（`/api/v1/apps/{appKey}`，均需登录）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/cosmetics` | 装扮中心：`settings.badgeSlots`、`slots`、`rarities`、`items[]`、`loadout`；未开启报 40361 |
| GET | `/cosmetics/me` | 我身上的装扮 `Loadout`；未开启时为空（不报错） |
| PUT | `/cosmetics/me` | `{slot, cosmeticId, position}`（slot：avatar_frame / profile_card / name_style / badge / id_plate / vip_plate）；`cosmeticId=0` 摘下，勋章 `position=-1` 摘下全部；返回新的 `Loadout` |
| GET | `/cosmetics/history` | 我的获得记录 `{items, total}`（page / limit） |
| GET | `/cosmetics/users?ids=1,2` | 别人身上的装扮 `{users: {对外ID: Loadout}}`，最多 100 个，没戴的人不出现 |

`items[]` 每件：`id slot code name description rarity series revision preview design` + `unlock{mode,label,hint,minLevel,feature,tiers[]}`
+ `state{usable,owned,permanent,expiresAt,reason,equipped,position}` + `channels[]{kind(points_store|lottery),refId,title,cost,days}`。

`Loadout`：`{avatarFrame?, profileCard?, nameStyle?, badges: [], idPlate?, vipPlate?}`，每项是 `View`（`id slot code name rarity revision preview design`）。

实时事件：`cosmetic.changed`（`data.action` = granted / revoked / equipped / unequipped），收到后重拉 `/cosmetics/me`。
站内信 `type=cosmetic`（分类 reward），跳转路由键 `cosmetics`。

错误码：40360 不能佩戴（`data.reason`）、40361 未开启、40425 装扮不存在。

### 管理端（`/api/admin/apps/:appkey/cosmetics`，权限 `app:cosmetic:*`）

| 方法 | 路径 | 权限 |
|---|---|---|
| GET | `/catalog` | read：slots / rarities / unlockModes / sources / vocabulary / templates / tiers / features / settings / assetTypes |
| GET, PUT | `/settings` | read / write：`{enabled, badgeSlots}` |
| GET | `/assets?kind=&keyword=&page=&limit=` | read：`{items, total}` |
| POST | `/assets`（multipart `file`, `name`, `kind`=image\|sprite） | write，同内容返回已有那条 |
| DELETE | `/assets/:assetId` | delete，被引用报 40958 |
| GET | `/items?slot=&status=&rarity=&keyword=` | read，不带设计稿 |
| POST | `/items` `{slot, code, name, description, rarity, series, unlock, sortOrder, hidden, template}` | write |
| PUT | `/item-order` `{itemIds}` | write |
| GET, PUT, DELETE | `/items/:itemId` | read / write / delete（有人持有过不能删，40956） |
| PUT | `/items/:itemId/draft` `{design}` | write，整份校验 |
| POST | `/items/:itemId/publish` `{note}` | publish |
| POST | `/items/:itemId/preview`（multipart `file`, PNG/WEBP ≤2MB） | write |
| PUT | `/items/:itemId/status` `{status: active\|archived\|draft}` | write；未发布不能上架（40959） |
| POST | `/items/:itemId/duplicate` | write |
| GET | `/items/:itemId/revisions` | read |
| POST | `/items/:itemId/revisions/:revision/restore` | publish（放回草稿，不直接发布） |
| GET | `/ownerships?userId=&cosmeticId=&source=&state=live\|ended\|revoked` | read |
| GET | `/stats` | read |
| GET | `/users/:userId` | read：`ownerships`、`equips`（各自 effective / reason）、`loadout`、`facts` |
| POST | `/users/:userId/grant` `{cosmeticId, days, equip}` | grant（equip 见「发放时佩戴」） |
| POST | `/users/:userId/ownerships/:ownershipId/revoke` `{reason}` | grant |
| POST | `/users/:userId/unequip` `{slot, position}` | grant |

## 设计稿（规格 v1）

同一份 JSON 由 Go 校验（`cosmetic.ValidateDesign`）、控制台与客户端各自绘制。三端必须对同一份语义负责。

```jsonc
{
  "version": 1,
  "canvas": {"width": 300, "height": 300},
  "avatar": {"x": 50, "y": 50, "size": 200, "shape": "circle", "radius": 0},   // 只有头像框
  "duration": 6000,             // 一个循环多少毫秒；0 静态
  "background": "#FF0F172A",    // 画布底色，资料卡用
  "layers": [ /* 见下 */ ],
  "nameStyle": { /* 只有昵称样式 */ },
  "assets": {"ring": {"id": 12, "url": "https://…", "width": 512, "height": 512, "contentType": "image/png", "sha256": "…"}}
}
```

### 坐标与绘制顺序

- 原点在画布左上角，单位是画布像素；客户端按「显示宽度 / canvas.width」等比缩放。
- 头像框：显示时**外框尺寸 = 头像尺寸 × canvas.width / avatar.size**，头像放在 avatar 指定的区域（按 shape 裁剪：circle / rounded（圆角 = radius × size）/ square）。
- `layers` 数组顺序即绘制顺序（先画的在下）。`placement: "below"` 的图层画在头像之下，其余在头像之上。
- 颜色一律 `#RRGGBB` 或 `#AARRGGBB`（**alpha 在前**，与 Android 的 ARGB 一致；控制台要换成 CSS 的 `#RRGGBBAA`）。

### 图层公共字段

`id name type hidden placement x y width height anchorX anchorY rotation scale opacity blend glow glowColor tracks`

变换：图层矩形是 `(x, y, width, height)`；以锚点 `(x + anchorX·width, y + anchorY·height)` 为中心先缩放（`scale`，以及轨道里的 `scaleX`/`scaleY` 再乘上去）、
再旋转（度，顺时针），透明度乘 `opacity`。`hue` 轨道对整个图层做色相旋转（度）。`blend`：normal / screen / multiply / add（加亮）/ overlay。
`hidden` 的图层不画。`glow`>0 时对形状 / 文字加外发光（模糊半径 = glow 画布像素，颜色 glowColor）。

### 图层类型

| type | 字段 | 绘制 |
|---|---|---|
| `image` | `asset fit(fill/contain/cover) tint` | 素材按 fit 放进图层矩形；tint 非空时按 SrcIn 染色 |
| `sprite` | `asset columns rows frameCount fps fit tint` | 素材是网格图（从左到右、从上到下），第 `floor(t/1000·fps) mod frameCount` 格；不受 duration 影响，按自己的时钟一直播 |
| `text` | `text fontSize fontWeight color align letterSpacing stroke strokeWidth` | 在图层矩形内按 align 水平对齐、垂直居中 |
| `shape` | `shape(rect/circle/ring/star/polygon) fill stroke strokeWidth cornerRadius sides innerRatio sweep startAngle dash dashOffset` | 见下 |
| `particles` | `particles{…}` | 见下 |

形状：`rect` 圆角 cornerRadius；`circle` 内切椭圆；`ring` 是描边圆弧（直径取 min(width,height) − strokeWidth，居中），
从 12 点方向起、偏 startAngle 度、顺时针扫 sweep 度，端点圆头；`star` 为 sides 个角、内径 = 外径 × innerRatio、第一个角朝上；
`polygon` 正 sides 边形、第一个顶点朝上。星形与多边形的外接形状是图层矩形的内切椭圆（横向半径 width/2、纵向半径 height/2，与 circle 同口径），宽高不等时随之压扁。`dash` 是描边虚线 `[实, 虚, …]`，`dashOffset` 为偏移（可动画，做「流光跑边」）。
填充与描边都可以有，先填充后描边。

### 粒子

`emitter(point/ring/rect) radius shape(circle/star/spark/image) asset colors[] count lifetime speed direction spread outward gravity sizeMin sizeMax spin fade shrink seed`

粒子必须**确定性**生成，三端看起来一致：

- 第 i 个粒子（0 ≤ i < count）的随机数来自 `seed` 与 `i`：用 `mulberry32(seed + i·0x9E3779B9)` 依次取 r0…r5（各在 [0,1)）。
- 出生偏移 `phase = i / count · lifetime`；时刻 t 的年龄 `age = (t + phase) mod lifetime`，进度 `p = age / lifetime`。
- 出生点：point 为图层中心；ring 为中心 + radius·(sin θ, −cos θ)，θ = r0·360°；rect 为图层矩形内 (r0·w, r1·h)。
- 方向：`outward` 时为 θ，否则 `direction − spread/2 + r2·spread`（度，0 向上、顺时针）。速度 `speed·(0.6 + 0.4·r3)`。
- 位置 = 出生点 + 速度·dir·age/1000 + (0, ½·gravity·(age/1000)²)。
- 大小（直径，画布像素）`sizeMin + (sizeMax − sizeMin)·r4`，`shrink` 时乘 (1 − p)；透明度 `fade` 时为 `1 − p`（前 10% 渐入）；颜色 `colors[floor(r5·len)]`；自转 `spin·age/1000` 度。
- 形状：circle 圆；star 五角星（内径 0.45）；spark 沿运动方向拉长的菱形（长 = 3×大小）；image 素材。

### 动画轨道

`tracks: [{property, keyframes: [{t, v, easing}]}]`，property ∈ `x y scale scaleX scaleY rotation opacity hue sweep dashOffset`。

- 时刻 `t` 为毫秒，落在 [0, duration]；整份设计稿以 `elapsed mod duration` 取当前时刻。
- 当前时刻在两帧之间时用**后一帧**的 easing 插值；早于第一帧取第一帧的值，晚于最后一帧取最后一帧的值。
- 轨道存在时覆盖静态值（`x`/`y` 覆盖图层左上角坐标；`scale` 覆盖 scale；`opacity` 乘到 opacity 上之前先替换；`rotation` 替换）。
- easing：linear；easeIn = 三次 `t³`；easeOut = `1−(1−t)³`；easeInOut = 三次缓入缓出；step = 未到下一帧前保持上一帧；
  backOut = `1 + 2.70158(t−1)³ + 1.70158(t−1)²`；elastic = `2^(−10t)·sin((10t−0.75)·2π/3) + 1`（t=0 取 0，t=1 取 1）。

### 昵称样式

`nameStyle: {color, colors[], effect, speed, fontWeight, glowColor, glowRadius, highlightColor}`，effect：

| effect | 效果 |
|---|---|
| none | 纯色 |
| shimmer | 一道 highlightColor（缺省白色 70%）高光从左到右扫过，周期 speed |
| pulse | 亮度在 75%–100% 之间呼吸 |
| cycle | 在 colors 之间依次平滑过渡，一圈 speed |
| glow | 外发光（glowColor、glowRadius） |
| wave | 逐字上下起伏（振幅 0.12em，相邻字相位差 0.15 周期） |

### 铭牌（号码铭牌 `id_plate`、会员铭牌 `vip_plate`）

两个单戴的槽位，装饰的是一枚**写着动态文字的胶囊**：号码铭牌换掉资料卡上的「ID 66666」标签，会员铭牌换掉会员有效期那一行（「永久有效」「2027-01-01 到期」）。
文字由客户端填，设计稿只决定写在哪、怎么写：

```jsonc
{
  "canvas": {"width": 360, "height": 96},          // 默认；任意比例，最长边 32–1024
  "layers": [ /* 与其他槽位相同 */ ],
  "plate": {
    "x": 96, "y": 18, "width": 240, "height": 60,  // 文字区域（画布像素，须落在画布内）
    "template": "ID {value}",                      // 1–32 字，恰好含一个 {value}
    "align": "center",                             // left / center / right
    "fontSize": 40,                                // 画布像素；一行放不下时等比缩小到刚好放下，不换行、不省略
    "fontWeight": 700,                             // 100–900
    "letterSpacing": 0.04,                         // em，-0.2–1
    "font": "sans"                                 // sans / serif / mono
  },
  "nameStyle": { /* 文字的颜色与动态效果，规格同昵称样式，必填 */ }
}
```

- `{value}`：号码铭牌是号码（不带「ID」字样，要的话写进 template）；会员铭牌是客户端原本那行有效期文字（「永久有效」「2027-01-01 到期」）。
- `{tier}`（**只有会员铭牌**可用）：会员标签上原本的名字（套餐名，没有时用档位名，如「领航者」）。
  号码铭牌的 template 恰好一个 `{value}`；会员铭牌的 template 里 `{value}`、`{tier}` 各最多一次、至少有一个，例如 `{tier} · {value}`。
- **会员铭牌合并替换**：戴着会员铭牌且是有效会员时，原来的会员标签（领航者胶囊）与有效期那一行**都不再显示**，只显示这一枚铭牌，
  点铭牌进会员页（与原会员标签相同）。号码铭牌只替换号码标签（点了复制）。
  会员铭牌只在用户有有效会员时出现；号码铭牌只在用户有号码时出现；不满足时照原样显示原来的标签。
- 绘制顺序：`placement: "below"` 的图层在文字之下，`"above"` 在文字之上（与头像框里「头像」的位置同理）。
  **铭牌槽位里 placement 缺省按 below 处理**（服务端规范化时补 below，控制台新建图层也默认 below）。
- 显示：客户端按**高度**定尺寸（「我的」页 26dp），宽度 = 高度 × canvas.width / canvas.height；文字按 plate 区域垂直居中、按 align 水平对齐，
  字号 = fontSize × 缩放比，放不下时整体缩小。
- **按字伸缩**（`plate.stretch: {"x0": 120, "x1": 300}`，可选，推荐）：画布上 x0–x1 这一段横向可伸缩，两端固定（徽章、圆头不变形）。
  - 客户端先按 fontSize（画布像素，含 letterSpacing）量出文字宽 `tw`，`delta = tw − plate.width`，下限 `−(x1 − x0) + 1`（中段至少留 1 像素）。
    显示用的画布宽 = canvas.width + delta，显示宽 = 高度 × (canvas.width + delta) / canvas.height；这时文字按原字号绘制、不缩小
    （父级给的宽度不够时才整体等比缩小）。
  - 横坐标映射 `X(x)`：x ≤ x0 不变；x0 < x < x1 时 `x0 + (x − x0)·(x1 − x0 + delta)/(x1 − x0)`；x ≥ x1 时 `x + delta`。纵向不变。
  - 图层：矩形左右边各自过 `X`（完全在 x0 左边或 x1 右边的图层只是平移，跨过伸缩段的图层被拉宽）。x 轨道的值同样过 `X`。
    `image` / `sprite` 且 `fit = fill` 跨过伸缩段时**三段式绘制**：素材按图层矩形里 x0、x1 对应的源像素列切成左中右三块，左右原样、只拉伸中间
    （序列帧每一格各自切）；其他 fit 只映射矩形。形状画进映射后的矩形（圆角半径不变，所以胶囊拉长仍是胶囊）；circle / star / polygon / ring
    只平移中心、不拉伸。粒子：rect 发射区映射，point / ring 发射中心过 `X`。
  - 文字区域：左 = `X(plate.x)`，右 = `X(plate.x + plate.width)`。
  - 校验：0 ≤ x0 < x1 ≤ canvas.width。没有 stretch 时按固定宽度、字放不下再缩小（上面一条）。
- `plate` 只出现在这两个槽位（其他槽位带了报错）；这两个槽位必须有 `plate` 与 `nameStyle`。不进自定义工坊。
- `Loadout` 多两项 `idPlate?`、`vipPlate?`；`PUT /cosmetics/me` 的 slot 可取这两个值。
- 目录与校验的细节：`catalog.slots` 里铭牌的 `plate = true`、`workshop = false`（工坊首页 `slots` 不列出铭牌，零件也不能用在铭牌上）；
  `vocabulary.plateAligns` / `plateFonts` 是两个枚举，`limits.maxPlateTemplateRunes = 32`。字号上限 1024；字重以 `plate.fontWeight` 为准
  （`nameStyle.fontWeight` 仍须合法，缺省跟 plate 一致）。新建铭牌的空白稿：plate `{24, 18, 312, 60}`、`stretch {24, 336}`，号码铭牌模板 `ID {value}`、会员铭牌 `{value}`。

### 上界

见 `cosmetic.DesignVocabulary().limits`（图层 40、每层轨道 8、每轨关键帧 64、时长 20 秒、粒子 240、文字 32 字、每件素材 32、
序列帧 256 格、素材 8MB、预览 2MB）。

## 模板

`cosmetic.Templates()`：鎏金光环、霓虹轨道、星河流光（头像框），极光尘埃（资料卡），星章、文字章（勋章），流金、幻彩、霓光（昵称），黑金号牌（号码铭牌）、黑金尊享（会员铭牌，`{tier} · {value}`，两枚都带按字伸缩 x0 84–x1 336）。
只用形状、文字与粒子，不依赖素材；`TestTemplatesValidate` 钉住每个模板都能过校验。三端渲染器的回归样例也用它们。

## 自定义工坊

用户在 App 里用**官方零件**拼自己的头像框、资料卡背景、昵称样式、勋章。**不能上传图片**，也不能有文字图层（免审核）。
代码：`internal/domain/cosmetic/workshop.go`（规则）、`internal/repository/postgres/cosmetic_workshop_repository.go`、
`internal/service/cosmetic_workshop_service.go`、`internal/transport/http/cosmetic_workshop_handlers.go`；迁移 000167。

### 作品是什么

作品就是 `cosmetic_items` 里 `origin = 'workshop'` 的一行（`creator_user_id` 是作者，获取方式 `creator`），与官方装扮同一份设计稿规格、
同一套佩戴（`PUT /cosmetics/me`）与绘制，所以三端渲染器、头像框、排行榜、名片等不用改。区别：

- **只有作者本人能戴，且作者此刻要有工坊额度**（`Eligibility` 的 `creator` 分支；额度为 0 时原因 `need_workshop`）。会员到期 → 额度归零 →
  作品在 `/cosmetics/me` 里消失，与会员装扮同理，不需要扫描。
- 保存即生效：新建就是上架、发布过的第 1 版；每次修改版本号 +1（客户端据此丢缓存）。
- 不进装扮中心、管理端装扮列表与统计，不能被任何系统发放（`grantCosmeticTx` 只认官方）。
- **名称只给作者看**：别人（`/cosmetics/users`、排行榜等）看到的 `name` 是「工坊作品」、`code` 为空。`View.workshop = true`，客户端可以标角标。
- 运营可以下架（`status = archived` + 原因，作者在工坊里看得到），下架后戴不上、不能再改，只能删除；恢复后照常。

### 额度

`cosmetic_settings.workshop = {enabled, freeQuota, vipQuota, tierQuotas: {"档位编号": 额度}}`。一个人的额度 =
max(freeQuota, 有效会员时的 vipQuota, 所在会员档位的 tierQuotas)，上限 50；工坊关闭时为 0。额度决定能存几件、作品能不能戴。
额度降下来时已存的作品不删，只是不能再新建（作品数 ≥ 额度）。

### 零件

`cosmetic_workshop_parts`：运营从素材库里挑出来的图片或序列帧，`{assetId, name, category, slots[], columns, rows, frameCount, fps, sortOrder, enabled}`。
`slots` 只能是 avatar_frame / profile_card / badge；序列帧零件必须给切分，作品里的序列帧图层必须照用。零件停用或改了槽位后，
已有作品照常能戴，只是再保存时会被拒。零件还在时素材不能删（40958）。

### 作品设计稿的额外限制

在 `ValidateDesign` 之外（`ValidateWorkshopDesign`）：画布固定为槽位默认值（头像框 300×300、头像区域 50/50/200 圆形；资料卡 1080×608；勋章 96×96）；
最多 16 个图层；粒子总数 ≤ 160；没有文字图层；image / sprite / 图片粒子只能引用**启用中、允许这个槽位**的零件（按素材编号对）。
昵称样式作品就是一份 `nameStyle`。名称 1–16 字。

### 用户端（`/api/v1/apps/{appKey}`，均需登录；装扮未开启 40361）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/cosmetics/workshop` | `settings{quota, used, canCreate, reason, badgeSlots, freeQuota, vipQuota}`、`slots`（不含铭牌）、`vocabulary`、`limits`、`parts[]`（带 `asset{url,width,height,contentType,kind}`）、`starters[]`（可套用的内置模板，`{key, slot, name, description, design}`）、`items[]` |
| POST | `/cosmetics/workshop/items` | `{slot, name, design}` → 作品；没有额度 40362（`data.reason = need_workshop`）、额度已满 40954 |
| PUT | `/cosmetics/workshop/items/{itemId}` | `{name, design}` → 作品；被下架 40363、不是自己的 40425 |
| DELETE | `/cosmetics/workshop/items/{itemId}` | 删除，正戴着的随之摘下 |

`items[]` 每件：`View`（`id slot code name rarity revision design workshop`）+ `status`（active / archived）+ `takedownReason` +
`equipped` + `position` + `createdAt` + `updatedAt`。工坊未开启时 `GET /cosmetics/workshop` 报 40362；装扮中心 `GET /cosmetics` 多返回
`workshop{enabled, quota}` 供客户端决定是否显示入口。作品里的 `design.assets` 只需给 `{id}`，地址由服务端补。

### 管理端（`/api/admin/apps/:appkey/cosmetics`，权限 `app:cosmetic:*`）

| 方法 | 路径 | 说明 |
|---|---|---|
| PUT | `/settings` | 多一个可选的 `workshop`（不传保持原样） |
| GET | `/workshop/parts` | `{items: Part[]}`（含停用的，带素材） |
| POST | `/workshop/parts` | `PartInput`；同一素材只能登记一次（40953） |
| PUT, DELETE | `/workshop/parts/:partId` | 修改 / 删除（40429 不存在） |
| GET | `/workshop/items?userId=&slot=&status=&keyword=&page=&limit=` | `{items, total}`，每件带 `creatorUserId creatorAccount creatorNickname` 与设计稿 |
| POST | `/workshop/items/:itemId/takedown` `{reason}` | 下架（原因必填，作者可见） |
| POST | `/workshop/items/:itemId/restore` | 恢复 |
