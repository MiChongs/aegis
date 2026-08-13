import {
  BadgeCheck,
  Ban,
  Bell,
  Boxes,
  Braces,
  Building2,
  CircleUserRound,
  Coins,
  CreditCard,
  FileJson,
  FileSearch,
  FileText,
  Fingerprint,
  Gauge,
  Gavel,
  Globe,
  Images,
  KeyRound,
  LayoutTemplate,
  LifeBuoy,
  ListChecks,
  ListTree,
  Lock,
  LogIn,
  MapPin,
  Megaphone,
  MonitorSmartphone,
  Network,
  Package,
  Radar,
  ReceiptText,
  Rocket,
  Route,
  ScanFace,
  ScrollText,
  Send,
  ShieldCheck,
  Siren,
  Terminal,
  Trophy,
  UserCog,
  Users,
  Wallet,
  Workflow,
  type LucideIcon,
} from "lucide-react";
import type { BrandLogoSlug } from "@/components/brand/sponsors/brand-logos.generated";

/**
 * 首页文案与数据目录，单一事实源。
 *
 * 分区组件只负责排版，一个字都不在组件里写：文案改动集中在这一个文件，
 * 不必去八个 tsx 里翻。
 *
 * 两条撰写约束：
 *
 * 1. 采用产品文档的中性书面语，不写第一人称口吻、反问句与旁白。
 * 2. 数字一律取自仓库里数得出来的事实（路由清单、渠道目录、迁移文件数），
 *    不写「海量」「极致」这类无法核对的形容词，并在旁边说明出处。
 */

/* ─────────────────────────── 冷开场 ─────────────────────────── */

/**
 * 开场的三拍是平台的三个能力域，不是修辞。每一拍报出域名与它包含的能力，
 * 落版是产品定位；首屏用同样三列接住，因此这段动画是目录的一部分，
 * 而不是内容前面的一段广告。
 *
 * 撰写口径与全站一致：陈述能力，不写问句、不写口号。
 */
export const intro = {
  slate: "Identity Platform · v2",
  beats: [
    { kicker: "认证与账号", line: "统一的登录、多因子与会话管理" },
    { kicker: "授权与组织", line: "细粒度权限策略与组织架构" },
    { kicker: "资产与风控", line: "积分、钱包、会员与实时风险判定" },
  ],
  finale: {
    title: "多租户用户系统平台",
    sub: "自托管部署，数据存储于自有 PostgreSQL 实例",
  },
  skip: "跳过",
} as const;

/* ─────────────────────────── 首屏 ─────────────────────────── */

export const hero = {
  /** 顶部那行等宽元信息，替代原来那颗胶囊徽章 */
  meta: {
    index: "01",
    label: "AEGIS · IDENTITY PLATFORM",
    version: "Auth Protocol v2",
  },
  title: "多租户用户系统平台",
  subtitle: "统一承载认证、授权、组织、资产与风控",
  /** 三个能力域，与冷开场的三拍一一对应 */
  pillars: [
    { label: "认证与账号", items: "登录 / 多因子 / 会话" },
    { label: "授权与组织", items: "策略 / 部门 / 审批" },
    { label: "资产与风控", items: "账本 / 会员 / 实时判定" },
  ],
  description:
    "Aegis 是一套自托管的多租户用户系统平台。每个接入应用拥有独立的用户库、认证策略与配置空间，客户端仅需对接单一命名空间，其余能力通过管理控制台配置。",
  primary: { authed: "进入控制台", guest: "登录控制台" },
  secondary: { label: "查看接入文档", href: "/developers" },
  footnote: "自托管部署，数据存储于自有 PostgreSQL 实例",
} as const;

/**
 * 首屏底部跑马灯的技术栈标记。
 *
 * 说清这套系统真实跑在什么之上，比任何形容词都短。图标在
 * `hero-section.tsx` 里登记，此处只保留可读名称与顺序。
 */
export const heroStack = [
  "Go 1.26",
  "Gin",
  "PostgreSQL",
  "Redis",
  "NATS JetStream",
  "Temporal",
  "OpenTelemetry",
  "Coraza WAF",
  "JWT / Passkey",
  "Stripe",
  "Next.js 16",
  "React 19",
  "Tailwind CSS 4",
] as const;

/* ─────────────────────────── 数字 ─────────────────────────── */

export type HomeMetric = {
  value: string;
  label: string;
  hint: string;
};

export const metrics: HomeMetric[] = [
  {
    value: "1000+",
    label: "已注册接口",
    hint: "管理端与接入端合计，OpenAPI 由运行中的路由树实时生成",
  },
  {
    value: "16",
    label: "内置支付渠道",
    hint: "覆盖内部钱包、Stripe、支付宝、微信等，渠道能力由服务端自描述",
  },
  {
    value: "9",
    label: "邮件服务商",
    hint: "SMTP 直连与八家 REST 服务商，优先采用官方 SDK",
  },
  {
    value: "6",
    label: "社交登录渠道",
    hint: "QQ / 微信 / GitHub / Google / Microsoft / 微博",
  },
];

/* ─────────────────────────── 核心能力 ─────────────────────────── */

/** 卡片配图的键，对应 `feature-visuals.tsx` 里的组件 */
export type HomeFeatureVisual =
  | "tenancy"
  | "policy"
  | "protocol"
  | "risk"
  | "ledger"
  | "function";

export type HomeFeature = {
  icon: LucideIcon;
  eyebrow: string;
  title: string;
  body: string;
  points: string[];
  visual: HomeFeatureVisual;
  /** 在 bento 网格里占两列 */
  wide?: boolean;
};

export const features: HomeFeature[] = [
  {
    icon: Boxes,
    eyebrow: "多租户",
    title: "应用之间的用户数据天然隔离",
    body: "每个应用拥有独立的用户库、认证策略与配置空间。隔离边界由请求路径中的 appKey 确定，不依赖业务代码逐条附加过滤条件。",
    points: ["应用级数据隔离", "认证与密码策略逐应用配置", "平台治理结论优先于应用自身开关"],
    visual: "tenancy",
    wide: true,
  },
  {
    icon: ShieldCheck,
    eyebrow: "授权",
    title: "细粒度授权，无需为单个权限新建角色",
    body: "平台、应用、组织三种作用域共用同一个策略引擎与同一张策略表。显式拒绝优先于放行，策略变更经 NATS 广播至全部实例。",
    points: ["一份策略表覆盖三种作用域", "临时权限到期自动失效", "内置接口可解释判定结果"],
    visual: "policy",
  },
  {
    icon: Lock,
    eyebrow: "接入协议",
    title: "提升安全等级仅需替换传输层适配器",
    body: "standard、signed、sealed 三档共用同一批路径与同一份 JSON 结构。标准档基于 HTTPS，签名档增加 HMAC-SHA256 请求签名，密封档在此之上叠加端到端加密载荷。",
    points: ["请求签名覆盖 query string", "控制台提供接入自检", "提供官方 Kotlin/JVM 客户端"],
    visual: "protocol",
  },
  {
    icon: Radar,
    eyebrow: "风控",
    title: "风控规则可在控制台编辑并模拟验证",
    body: "规则采用表达式语法，具备类型化环境与编译缓存，变量名错误在保存时即被拦截。模拟器支持以草稿规则回放真实流量，确认效果后再发布。",
    points: ["设备指纹与 IP 风险库", "GCRA 限流与地理风控", "命中记录可回放、可人工复核"],
    visual: "risk",
  },
  {
    icon: Wallet,
    eyebrow: "资产与交易",
    title: "资金与权益记录在同一套账本",
    body: "积分、钱包、订单、退款、会员与试用共用一套账本，金额全程以定点小数流转。支付凭证 PDF 支持 10 种语言排版。",
    points: ["订单与钱包流水分别出具凭证", "试用资格一人一次，由唯一约束保证", "管理员调账全程留痕"],
    visual: "ledger",
  },
  {
    icon: Braces,
    eyebrow: "远程函数",
    title: "自定义业务逻辑运行于服务端沙箱",
    body: "接入方可将业务分支编写为 JavaScript 并托管于服务端沙箱。试运行读取真实数据但不产生副作用，历史版本正文可随时取回，能力与运行闸门支持在控制台调整。",
    points: ["按已声明能力生成编辑器类型", "调用记录逐条可查", "KV 存储为脚本独占状态"],
    visual: "function",
    wide: true,
  },
];

/* ─────────────────────────── 能力全景 ─────────────────────────── */

export type HomeCapability = {
  /** 逐项各配一个图标：六项共用分组图标等于没有图标，只是把同一个形状重复六遍 */
  icon: LucideIcon;
  title: string;
  description: string;
};

export type HomeCapabilityGroup = {
  key: string;
  label: string;
  icon: LucideIcon;
  summary: string;
  items: HomeCapability[];
};

export const capabilityGroups: HomeCapabilityGroup[] = [
  {
    key: "identity",
    label: "认证与账号",
    icon: Fingerprint,
    summary: "覆盖账号生命周期内的全部环节，从首次注册到多设备登录。",
    items: [
      { icon: LogIn, title: "三类登录入口", description: "密码、短信验证码与 OAuth2，登录与注册的可选集合独立配置" },
      { icon: KeyRound, title: "多因子与无密码", description: "TOTP、一次性恢复码与 Passkey（WebAuthn 生物认证）" },
      { icon: MonitorSmartphone, title: "会话与设备", description: "在线会话列表、强制下线、登录基线与异地提醒" },
      { icon: CircleUserRound, title: "头像服务", description: "地址永久有效，支持 EXIF 纠正、多尺寸与 blurhash 占位" },
      { icon: Gauge, title: "密码强度", description: "基于 zxcvbn 猜测次数估算，替代字符类组合规则" },
      { icon: ScanFace, title: "动态验证码", description: "支持动画 GIF、算术与音频三类，外观按应用配置" },
    ],
  },
  {
    key: "organization",
    label: "组织与权限",
    icon: Building2,
    summary: "面向企业客户的组织结构、成员归属与授权管理。",
    items: [
      { icon: Network, title: "组织与部门树", description: "部门支持拖拽调整层级，实体对外统一使用 UUID 标识" },
      { icon: Users, title: "岗位与成员", description: "成员邀请与转让，权限按部门范围限定" },
      { icon: UserCog, title: "组织角色", description: "支持继承内置角色，也可仅覆盖其中单项" },
      { icon: ListChecks, title: "审批链", description: "审批场景与节点由服务端目录下发" },
      { icon: LayoutTemplate, title: "权限模板与协作组", description: "配置一次，批量套用至成员" },
      { icon: ScrollText, title: "操作日志", description: "组织内变更全量留痕，支持导出 Excel" },
    ],
  },
  {
    key: "commerce",
    label: "资产与交易",
    icon: Coins,
    summary: "用户持有的积分、余额与权益，以及全部变更记录。",
    items: [
      { icon: Trophy, title: "积分与等级", description: "自动签到、经验曲线与排行榜" },
      { icon: Wallet, title: "钱包与流水", description: "余额、消费与管理员调账，每笔流水记录方向与事由" },
      { icon: CreditCard, title: "支付渠道", description: "16 个内置渠道，含内部钱包、聚合支付与加密货币" },
      { icon: ReceiptText, title: "订单与退款", description: "退款作为反向资金流单独记录，并参与实收净额计算" },
      { icon: BadgeCheck, title: "会员与功能标识", description: "套餐关联功能标识，服务端可按标识粒度校验权益" },
      { icon: FileText, title: "凭证 PDF", description: "支持 10 种语言排版，可下载或直接寄送至用户邮箱" },
    ],
  },
  {
    key: "operations",
    label: "运营与内容",
    icon: Megaphone,
    summary: "无需发版即可调整的运营配置与内容投放。",
    items: [
      { icon: Images, title: "Banner", description: "按展示位分组并支持拖拽排序，直接呈现当前投放状态" },
      { icon: Megaphone, title: "公告", description: "应用公告支持富文本并在写入时净化，平台公告直达控制台" },
      { icon: Rocket, title: "版本发布", description: "多渠道灰度与强制更新检查" },
      { icon: LifeBuoy, title: "工单中心", description: "分类、处理组、SLA 与快捷回复，可见范围由服务端推导" },
      { icon: Send, title: "通知出口", description: "渠道、事件订阅、模板与投递记录，渠道字段由服务端元数据驱动" },
      { icon: Bell, title: "实时推送", description: "站内信与 WebSocket 推送，未读角标由服务端事件驱动" },
    ],
  },
  {
    key: "security",
    label: "安全与治理",
    icon: Siren,
    summary: "覆盖事前拦截、事中记录与事后处置的完整链路。",
    items: [
      { icon: ShieldCheck, title: "WAF", description: "Coraza v3 与 OWASP CRS v4 规则集，拦截日志逐条可查" },
      { icon: Globe, title: "真实客户端 IP", description: "基于受信代理网段与转发链判定，限流、封禁与审计均以此为准" },
      { icon: Ban, title: "封禁与申诉", description: "支持 IP、地域与账号三类封禁，记录起止时间与操作人，可撤销" },
      { icon: MapPin, title: "地理风控", description: "地理围栏、热力图与轨迹回放，内网地址统一归并处理" },
      { icon: FileSearch, title: "审计", description: "登录、会话与管理动作全量留痕" },
      { icon: Gavel, title: "平台治理", description: "六档治理状态与七项限制，每项均有明确执行点" },
    ],
  },
  {
    key: "developer",
    label: "开发者",
    icon: Terminal,
    summary: "接入、调试与排障所需的工具、客户端与文档。",
    items: [
      { icon: FileJson, title: "OpenAPI", description: "由运行中的路由树实时生成，可在开发者门户中直接调试" },
      { icon: Package, title: "官方客户端", description: "Kotlin/JVM 实现，Android 与服务端共用同一产物" },
      { icon: Braces, title: "远程函数", description: "服务端 JavaScript 沙箱、版本管理与调用密钥" },
      { icon: Workflow, title: "工作流", description: "可视化编排，基于 Temporal 引擎" },
      { icon: Route, title: "出海代理网关", description: "按域名后缀路由，支持六种协议端点，出网调用共用同一张路由表" },
      { icon: ListTree, title: "路由清单", description: "支持导出为表格、树形、Markdown、CSV 与 JSON，无需连接数据库" },
    ],
  },
];

/* ─────────────────────────── 架构 ─────────────────────────── */

export type HomeArchLayer = {
  name: string;
  role: string;
  stack: string;
};

export const archLayers: HomeArchLayer[] = [
  {
    name: "接入网关",
    role: "接入方对接的唯一命名空间，安全等级在此层完成解包",
    stack: "/api/v1/apps/{appKey}/* · WAF · 限流 · 重放防护",
  },
  {
    name: "传输层",
    role: "路由、参数绑定与响应封装，OpenAPI 规范由此层生成",
    stack: "Gin · DTO · pkg/response",
  },
  {
    name: "业务层",
    role: "业务判定的统一归属层，权限与作用域校验均下沉至此",
    stack: "认证 · 授权 · 组织 · 资产 · 风控 · 内容 · 远程函数",
  },
  {
    name: "数据层",
    role: "数据读写与缓存，新增查询由 sqlc 依据迁移文件生成类型安全代码",
    stack: "PostgreSQL + PostGIS · Redis · pgx/v5 · sqlc",
  },
  {
    name: "基础设施",
    role: "跨进程事件、长流程编排与可观测性",
    stack: "NATS JetStream · Temporal · GeoIP2 · OpenTelemetry",
  },
];

/* ─────────────────────────── 接入 ─────────────────────────── */

export type HomeSecurityLevel = {
  key: string;
  label: string;
  tagline: string;
  description: string;
  requirement: string;
  code: string;
};

export const securityLevels: HomeSecurityLevel[] = [
  {
    key: "standard",
    label: "standard",
    tagline: "默认档",
    description:
      "仅需 HTTPS 与 JSON。不涉及密钥交换与握手流程，客户端无需引入密码学依赖，适用于功能联调阶段。",
    requirement: "客户端无需额外处理",
    code: `curl -X POST https://your-host/api/v1/apps/\${APP_KEY}/auth/login \\
  -H 'Content-Type: application/json' \\
  -d '{"method":"password","identifier":"demo","password":"******"}'

# → { "code": 0, "data": { "accessToken": "…", "user": { … } } }`,
  },
  {
    key: "signed",
    label: "signed",
    tagline: "验证调用方身份",
    description:
      "在标准档基础上增加 HMAC-SHA256 请求签名，用于验证调用方持有 appSecret。签名 v2 覆盖 query string，避免请求参数在传输过程中被篡改。",
    requirement: "客户端需拼接 canonical 串并计算一次签名",
    code: `val canonical = listOf(method, path, query, timestamp, nonce, sha256(body))
  .joinToString("\\n")
val signature = hmacSha256(appSecret, canonical).toHex()

request.header("X-Aegis-Timestamp", timestamp)
request.header("X-Aegis-Nonce", nonce)
request.header("X-Aegis-Signature", "v2=\${signature}")`,
  },
  {
    key: "sealed",
    label: "sealed",
    tagline: "请求体端到端加密",
    description:
      "在签名档基础上叠加端到端加密载荷。AEAD 保证密文完整性，签名证明调用方身份，二者共同生效，因此 sealed 是 signed 的叠加而非替代。",
    requirement: "客户端需先完成签名，再加密载荷",
    code: `// 有请求体：密文走 body
POST /api/v1/apps/{appKey}/me/profile
{ "payload": "<base64 ciphertext>" }

// 无请求体：密文走 query，GET 不能带 body
GET /api/v1/apps/{appKey}/me?_payload=<base64 ciphertext>

// 上传：整个 multipart 体加密
X-Aegis-Plain-Content-Type: multipart/form-data; boundary=…`,
  },
];

/* ─────────────────────────── 常见问题 ─────────────────────────── */

export type HomeFaq = {
  question: string;
  answer: string;
};

export const faqs: HomeFaq[] = [
  {
    question: "每接入一个应用是否需要单独部署？",
    answer:
      "不需要。单套部署可承载任意数量的应用，每个应用拥有独立的用户库、认证策略与配置空间。隔离边界由请求路径中的 appKey 确定，与部署边界无关，因此新增应用是一次控制台操作，而非运维动作。",
  },
  {
    question: "如何选择接入的安全等级？",
    answer:
      "建议先使用 standard 完成功能联调，该档仅要求 HTTPS 与 JSON。当服务端需要验证请求来自持有 appSecret 的调用方时，升级至 signed。当请求体不允许以明文出现在传输链路中时，升级至 sealed。三档共用同一批路径与同一份 JSON 结构，升级仅需替换传输层适配器，业务代码无需改动。",
  },
  {
    question: "已上线的旧系统能否迁移接入？",
    answer:
      "支持。既可连接旧 MySQL 实例执行单条或批量同步，也可直接导入 mysqldump 文件，无需额外部署 MySQL 实例。旧版明文认证命名空间由各应用独立开关控制，历史客户端可继续运行，新客户端使用新协议，无需一次性切换。",
  },
  {
    question: "部署需要准备哪些中间件？",
    answer:
      "必需组件为 PostgreSQL（含 PostGIS 扩展）、Redis 与 NATS。Temporal 仅在使用工作流能力时需要。对象存储、邮件服务商与支付渠道均按需在控制台配置，未配置则不启用。",
  },
  {
    question: "配置项应归属应用级还是平台级？",
    answer:
      "配置仅有两档作用域，且各有唯一归属：随应用不同而变化的归应用级，对所有应用一致的归平台级。同一配置项不会同时提供两个入口，以确保生效来源明确。",
  },
  {
    question: "是否必须部署本管理控制台？",
    answer:
      "非必须。控制台是该套 API 的客户端之一，全部管理能力均有对应接口，OpenAPI 规范由服务端实时生成。可仅部署后端服务，并将管理界面集成至现有运营系统。",
  },
];

/* ─────────────────────────── 赞助商 ─────────────────────────── */

export type HomeSponsor = {
  /** 品牌标识键，取值受 brand-logos.generated.ts 约束，拼错过不了类型检查 */
  slug: BrandLogoSlug;
  name: string;
  /** 领域分类。这一排跨云资源、模型与开发工具，只写它是什么，不写夸词 */
  category: string;
  /** 已内置官方适配的，写清它在 Aegis 里承担什么；其余不写，不硬编故事 */
  role?: string;
  href: string;
};

/**
 * 排列顺序即三行跑马灯的分行依据（按下标均分），因此是按**字标宽度**穿插的：
 * Cloudflare 的字标有 10 个字高宽，Kimi 只有 2.7，同宽的排在一起会让某一行
 * 明显比另外两行长，滚起来三行的节奏对不齐。
 */
export const sponsors: HomeSponsor[] = [
  {
    slug: "zeabur",
    name: "Zeabur",
    category: "应用托管",
    role: "部署平台，同时提供内置的 REST 邮件出口",
    href: "https://zeabur.com",
  },
  {
    slug: "huggingface",
    name: "Hugging Face",
    category: "模型社区",
    href: "https://huggingface.co",
  },
  {
    slug: "anthropic",
    name: "Anthropic",
    category: "大模型",
    href: "https://www.anthropic.com",
  },
  {
    slug: "tencentcloud",
    name: "腾讯云",
    category: "云计算",
    role: "COS 对象存储与 SES 邮件出口",
    href: "https://cloud.tencent.com",
  },
  {
    slug: "aws",
    name: "Amazon Web Services",
    category: "云计算",
    role: "S3 对象存储与 SES 邮件出口",
    href: "https://aws.amazon.com",
  },
  {
    slug: "gemini",
    name: "Gemini",
    category: "大模型",
    href: "https://gemini.google.com",
  },
  {
    slug: "huaweicloud",
    name: "华为云",
    category: "云计算",
    href: "https://www.huaweicloud.com",
  },
  {
    slug: "qiniu",
    name: "七牛云",
    category: "对象存储",
    role: "对象存储",
    href: "https://www.qiniu.com",
  },
  {
    slug: "alibabacloud",
    name: "阿里云",
    category: "云计算",
    role: "OSS 对象存储与邮件推送出口",
    href: "https://www.aliyun.com",
  },
  {
    slug: "openai",
    name: "OpenAI",
    category: "大模型",
    href: "https://openai.com",
  },
  {
    slug: "azure",
    name: "Microsoft Azure",
    category: "云计算",
    role: "Blob 对象存储",
    href: "https://azure.microsoft.com",
  },
  {
    slug: "qwen",
    name: "通义千问",
    category: "大模型",
    href: "https://tongyi.aliyun.com",
  },
  {
    slug: "deepseek",
    name: "DeepSeek",
    category: "大模型",
    href: "https://www.deepseek.com",
  },
  {
    slug: "nvidia",
    name: "NVIDIA",
    category: "加速计算",
    href: "https://www.nvidia.com",
  },
  {
    slug: "figma",
    name: "Figma",
    category: "设计协作",
    href: "https://www.figma.com",
  },
  {
    slug: "mistral",
    name: "Mistral AI",
    category: "大模型",
    href: "https://mistral.ai",
  },
  {
    slug: "vercel",
    name: "Vercel",
    category: "前端云平台",
    role: "管理控制台所用的 Next.js 由其维护",
    href: "https://vercel.com",
  },
  {
    slug: "github",
    name: "GitHub",
    category: "代码托管",
    role: "源码托管与 OAuth 登录渠道",
    href: "https://github.com",
  },
  {
    slug: "google",
    name: "Google",
    category: "云计算",
    role: "OAuth 登录渠道",
    href: "https://cloud.google.com",
  },
  {
    slug: "kimi",
    name: "Kimi",
    category: "大模型",
    href: "https://kimi.moonshot.cn",
  },
  {
    slug: "cloudflare",
    name: "Cloudflare",
    category: "边缘网络",
    role: "其网段清单参与真实客户端 IP 判定",
    href: "https://www.cloudflare.com",
  },
  {
    slug: "notion",
    name: "Notion",
    category: "协作文档",
    href: "https://www.notion.so",
  },
  {
    slug: "cursor",
    name: "Cursor",
    category: "AI 代码编辑器",
    href: "https://cursor.com",
  },
  {
    slug: "volcengine",
    name: "火山引擎",
    category: "云计算",
    href: "https://www.volcengine.com",
  },
];

export const sponsorsSection = {
  eyebrow: "赞助商",
  title: "由这些厂商提供支持",
  description:
    "从云资源、模型能力到开发工具。其中存储、邮件出口与登录渠道已内置官方适配，在控制台配置后即可启用。",
  disclaimer: "商标与图形归各自所有者所有，出现于此不代表其对本项目的背书。",
} as const;

/* ─────────────────────────── 收尾 ─────────────────────────── */

export const closing = {
  title: "开始接入 Aegis",
  description:
    "管理控制台提供全部可配置项，对应能力均有开放接口。登录后即可查看完整的功能范围与配置入口。",
  primary: { authed: "进入控制台", guest: "登录控制台" },
  secondary: { label: "阅读接入文档", href: "/developers" },
} as const;

/* ─────────────────────────── 页脚 ─────────────────────────── */

export type FooterColumn = {
  title: string;
  links: { label: string; href: string; external?: boolean }[];
};

export const footerColumns: FooterColumn[] = [
  {
    title: "平台",
    links: [
      { label: "首页", href: "/" },
      { label: "服务状态", href: "/status" },
      { label: "控制台", href: "/overview" },
    ],
  },
  {
    title: "开发者",
    links: [
      { label: "快速接入", href: "/developers" },
      { label: "接口文档", href: "/developers/api" },
    ],
  },
  {
    title: "法律",
    links: [
      { label: "用户协议", href: "/legal/terms" },
      { label: "隐私政策", href: "/legal/privacy" },
    ],
  },
];
