"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { AlertTriangle, BookOpen, CheckCircle2, Loader2, Lock, ShieldCheck, Zap } from "lucide-react";
import { CodeBlock, InlineCode } from "@/components/developers/code-block";
import { CodeSamples } from "@/components/developers/code-samples";
import {
  getAppIntegrationConfig,
  type AppIntegrationConfig,
  type CaptchaRequirement,
  type SecurityLevel
} from "@/lib/api/app-auth-protocol";
import { buildScenarios } from "@/lib/integration-snippets";
import { ApiError } from "@/lib/api/client";
import { appConfig } from "@/lib/env";
import { useOrigin } from "@/lib/use-client-value";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

const SECTIONS = [
  { id: "samples", label: "调用示例" },
  { id: "config", label: "应用配置" },
  { id: "levels", label: "安全等级" },
  { id: "errors", label: "错误码" },
  { id: "reference", label: "接口清单" }
] as const;

const PLACEHOLDER_APP_KEY = "your_app_key";

const LEVEL_CARDS: {
  level: SecurityLevel;
  name: string;
  title: string;
  work: string;
  icon: typeof Zap;
}[] = [
  {
    level: "standard",
    name: "standard",
    title: "标准",
    work: "HTTPS 上直接发 JSON，不涉及密钥和密码学库。",
    icon: Zap
  },
  {
    level: "signed",
    name: "signed",
    title: "签名",
    work: "每个请求附带一个 HMAC-SHA256 头，防篡改与重放。",
    icon: ShieldCheck
  },
  {
    level: "sealed",
    name: "sealed",
    title: "加密",
    work: "在签名基础上再用 X25519 + XChaCha20-Poly1305 加密载荷。",
    icon: Lock
  }
];

const ENDPOINTS: { method: string; path: string; note: string }[] = [
  { method: "GET", path: "/config", note: "应用能力与安全等级规格，免包装可读" },
  { method: "POST", path: "/captcha", note: "签发图形验证码" },
  { method: "POST", path: "/auth/sms/code", note: "申请短信验证码，purpose 取 login 或 register" },
  { method: "POST", path: "/auth/register", note: "注册，method 取 password 或 sms" },
  { method: "POST", path: "/auth/login", note: "登录，method 取 password 或 sms" },
  { method: "POST", path: "/auth/refresh", note: "刷新访问令牌" },
  { method: "POST", path: "/auth/logout", note: "注销当前会话，需 Bearer" },
  { method: "POST", path: "/auth/2fa/verify", note: "完成二次认证挑战" },
  { method: "POST", path: "/auth/oauth/url", note: "取第三方授权地址" },
  { method: "GET", path: "/auth/oauth/callback", note: "第三方授权回跳，免包装" },
  { method: "POST", path: "/auth/oauth/exchange", note: "原生 SDK 用 profile 换会话" },
  { method: "GET", path: "/me", note: "当前登录用户资料，需 Bearer" }
];

const ERROR_CODES: { code: string; layer: "gateway" | "business"; meaning: string; fix: string }[] = [
  {
    code: "40071",
    layer: "gateway",
    meaning: "时间戳无效或已过期",
    fix: "客户端时钟与服务端的偏差需在 5 分钟内"
  },
  {
    code: "40077",
    layer: "gateway",
    meaning: "加密载荷认证失败",
    fix: "检查 AAD 的七行拼接、HKDF 盐与 nonce"
  },
  {
    code: "40174 / 40175",
    layer: "gateway",
    meaning: "签名格式无效或校验失败",
    fix: "核对待签名字符串的字段顺序与换行，确认使用的是当前 appSecret"
  },
  {
    code: "40970",
    layer: "gateway",
    meaning: "nonce 已被使用",
    fix: "每个请求生成新的随机 nonce"
  },
  {
    code: "42670",
    layer: "gateway",
    meaning: "应用要求加密载荷",
    fix: "该应用处于加密档，不接受明文 JSON"
  },
  {
    code: "40370",
    layer: "business",
    meaning: "该认证方式未启用",
    fix: "由应用管理员在认证策略中勾选"
  },
  {
    code: "40393",
    layer: "business",
    meaning: "第三方账号未绑定，且渠道未开放自动注册",
    fix: "引导用户先用已有账号登录，再绑定第三方"
  },
  {
    code: "40394",
    layer: "business",
    meaning: "手机号未注册，且应用未开放短信注册",
    fix: "改用其他登录方式，或由管理员开启短信注册"
  },
  {
    code: "40470",
    layer: "business",
    meaning: "应用不存在或已停用",
    fix: "核对 appKey 与应用状态"
  }
];

/**
 * 逐入口说明验证码要求。
 *
 * 只说"需要验证码"是不够的：登录要而注册不要是很常见的配置，接入方看到一句
 * 笼统的"需要验证码"仍然不知道该在哪个表单里加那一栏。
 */
function describeCaptcha(captcha: CaptchaRequirement | undefined): string {
  const entries = [
    { label: "登录", required: captcha?.login },
    { label: "注册", required: captcha?.register },
    { label: "短信", required: captcha?.sms }
  ].filter((entry) => entry.required);
  return entries.length === 0
    ? "均无需验证码"
    : `${entries.map((entry) => entry.label).join(" / ")}需验证码`;
}

function Section({
  id,
  index,
  title,
  children
}: {
  id: string;
  index: number;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section id={id} className="scroll-mt-20 border-t pt-9 first:border-t-0 first:pt-0">
      <h2 className="flex items-baseline gap-2.5 text-xl font-semibold tracking-tight">
        <span className="text-sm font-normal tabular-nums text-muted-foreground">
          {String(index).padStart(2, "0")}
        </span>
        {title}
      </h2>
      <div className="mt-4 space-y-4 text-sm leading-relaxed">{children}</div>
    </section>
  );
}

function Callout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex gap-2.5 rounded-lg border border-amber-500/35 bg-amber-500/[0.07] p-3 text-[13px] leading-relaxed text-amber-900 dark:text-amber-200">
      <AlertTriangle className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0">{children}</div>
    </div>
  );
}

export default function DevelopersQuickStartPage() {
  const [appKeyInput, setAppKeyInput] = useState("");
  const [config, setConfig] = useState<AppIntegrationConfig | null>(null);
  const [probing, setProbing] = useState(false);
  const [probeError, setProbeError] = useState("");

  // 同源部署时 apiBaseUrl 为空串，回落到当前页面 origin（服务端渲染阶段为空）
  const pageOrigin = useOrigin();
  const appKey = appKeyInput.trim() || PLACEHOLDER_APP_KEY;
  const base = appConfig.apiBaseUrl || pageOrigin;

  // 探测到真实应用就按它的等级展示，否则按标准档演示
  const activeLevel: SecurityLevel = config?.security.level ?? "standard";
  const scenarios = useMemo(
    () => buildScenarios({ baseUrl: base, appKey, level: activeLevel }),
    [base, appKey, activeLevel]
  );

  async function probe() {
    const key = appKeyInput.trim();
    if (!key) return;
    setProbing(true);
    setProbeError("");
    setConfig(null);
    try {
      setConfig(await getAppIntegrationConfig(key));
    } catch (error) {
      setProbeError(
        error instanceof ApiError ? error.message : error instanceof Error ? error.message : "请求失败"
      );
    } finally {
      setProbing(false);
    }
  }

  return (
    <div className="mx-auto w-full max-w-[1400px] px-4 py-10 md:px-6">
      <header className="max-w-3xl">
        <p className="text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground">
          Aegis Open API
        </p>
        <h1 className="mt-2 text-3xl font-semibold tracking-tight md:text-4xl">应用接入</h1>
        <p className="mt-3 text-[15px] leading-relaxed text-muted-foreground">
          接入方的全部接口都在{" "}
          <InlineCode copyable={false}>{"/api/v1/apps/{appKey}"}</InlineCode> 下，
          覆盖注册、登录、令牌刷新与用户资料。标准档下一次{" "}
          <InlineCode copyable={false}>fetch</InlineCode> 即可完成登录；
          提高安全等级后路径与 JSON 结构不变，只是请求多一层包装。
        </p>
      </header>

      {/* 填入真实 appKey 后，全页示例随之替换 */}
      <div className="mt-6 rounded-xl border bg-muted/20 p-4">
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-56 flex-1">
            <label className="text-xs font-medium" htmlFor="appkey-probe">
              App Key
            </label>
            <p className="mt-0.5 text-xs text-muted-foreground">
              向应用管理员索取。填入后本页示例会替换成你的值，并按该应用的实际配置展示。
            </p>
            <Input
              id="appkey-probe"
              value={appKeyInput}
              placeholder={PLACEHOLDER_APP_KEY}
              className="mt-2 font-mono text-sm"
              onChange={(event) => setAppKeyInput(event.target.value)}
            />
          </div>
          <div className="flex gap-2">
            <Button size="sm" disabled={probing || !appKeyInput.trim()} onClick={() => void probe()}>
              {probing ? <Loader2 className="size-4 animate-spin" /> : null}
              读取配置
            </Button>
            <Button asChild size="sm" variant="outline">
              <Link href="/developers/api">
                <BookOpen className="size-4" /> 接口文档
              </Link>
            </Button>
          </div>
        </div>

        {probeError ? (
          <p className="mt-3 text-[13px] text-destructive">读取失败：{probeError}</p>
        ) : null}

        {config ? (
          <div className="mt-3 border-t pt-3 text-[13px]">
            <div className="flex flex-wrap items-center gap-2">
              <CheckCircle2 className="size-4 text-emerald-600 dark:text-emerald-400" />
              <span className="font-medium">{config.app.name}</span>
              <Badge variant={config.app.status ? "success" : "danger"} size="sm">
                {config.app.status ? "运行中" : "已停用"}
              </Badge>
              <Badge variant="info" size="sm">
                {config.security.level}
              </Badge>
            </div>
            <p className="mt-2 text-muted-foreground">
              登录方式 {config.auth.loginMethods.join(" / ") || "无"} · 账号标识{" "}
              {config.auth.identifiers.join(" / ")} ·{" "}
              {describeCaptcha(config.auth.captcha)} ·{" "}
              {config.auth.registerEnabled ? "开放注册" : "关闭注册"}
            </p>
          </div>
        ) : null}
      </div>

      <div className="mt-10 gap-10 lg:grid lg:grid-cols-[168px_minmax(0,1fr)]">
        <nav className="hidden lg:block" aria-label="页面目录">
          <ul className="sticky top-20 space-y-0.5 text-sm">
            {SECTIONS.map((section, index) => (
              <li key={section.id}>
                <a
                  href={`#${section.id}`}
                  className="flex items-baseline gap-2 rounded-md px-2.5 py-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                >
                  <span className="text-xs tabular-nums opacity-60">
                    {String(index + 1).padStart(2, "0")}
                  </span>
                  {section.label}
                </a>
              </li>
            ))}
          </ul>
        </nav>

        <div className="min-w-0 space-y-9">
          <Section id="samples" index={1} title="调用示例">
            <p>
              响应统一是{" "}
              <InlineCode copyable={false}>{"{ code, message, data }"}</InlineCode> 信封，
              <InlineCode copyable={false}>code</InlineCode> 为 200 表示成功。
              登录成功后 <InlineCode copyable={false}>data</InlineCode> 中包含{" "}
              <InlineCode copyable={false}>accessToken</InlineCode>、
              <InlineCode copyable={false}>refreshToken</InlineCode> 与{" "}
              <InlineCode copyable={false}>expiresAt</InlineCode>。
            </p>
            <p>
              示例中只有登录会随安全等级变化。注册、短信、第三方和会话共用同一套包装，
              因此统一按标准档展示，换档时复用登录示例里的包装函数。
            </p>
            <CodeSamples scenarios={scenarios} maxHeight={620} />
          </Section>

          <Section id="config" index={2} title="应用配置">
            <p>
              <InlineCode copyable={false}>/config</InlineCode> 是客户端唯一需要预先拉取的接口，
              在任何安全等级下都可以明文读取，响应可缓存 60 秒。
              它描述了这个应用的登录方式、验证码要求、注册字段、可用的第三方渠道，
              以及当前等级所需的全部参数。
            </p>
            <CodeBlock
              language="json"
              title="GET /config 响应片段"
              maxHeight={340}
              code={`{
  "protocolVersion": "aegis-app-v1",
  "app": { "key": "${appKey}", "name": "示例应用", "status": true },
  "auth": {
    "identifiers": ["username", "email", "phone"],
    "loginMethods": ["password", "sms", "oauth"],
    "registerMethods": ["password", "sms"],
    "registrationSchema": [
      { "name": "account",  "type": "text",     "required": true,  "mutable": false, "label": "账号" },
      { "name": "password", "type": "password", "required": true,  "mutable": true,  "label": "密码" },
      { "name": "nickname", "type": "text",     "required": false, "mutable": true,  "label": "昵称" }
    ],
    "captcha": { "login": true, "register": false, "sms": true },
    "autoLoginAfterRegister": true,
    "registerEnabled": true,
    "loginEnabled": true,
    "oauthProviders": [
      { "provider": "wechat", "displayName": "微信", "allowLogin": true, "sortOrder": 1 }
    ]
  },
  "security": { "level": "standard", "appKeyHeader": "X-Aegis-App-Key" },
  "endpoints": {
    "login": "/api/v1/apps/${appKey}/auth/login",
    "smsCode": "/api/v1/apps/${appKey}/auth/sms/code",
    "oauthUrl": "/api/v1/apps/${appKey}/auth/oauth/url",
    "me": "/api/v1/apps/${appKey}/me"
  }
}`}
            />
            <p>
              注册表单按 <InlineCode copyable={false}>registrationSchema</InlineCode> 渲染，
              第三方登录按钮按 <InlineCode copyable={false}>oauthProviders</InlineCode> 渲染。
              管理员调整配置后客户端无需发版。
              <InlineCode copyable={false}>endpoints</InlineCode> 给出完整相对路径，
              客户端不需要自行拼接 appKey。
            </p>
          </Section>

          <Section id="levels" index={3} title="安全等级">
            <p>
              等级由应用管理员设定，客户端从 <InlineCode copyable={false}>/config</InlineCode> 读取。
              三档共用同一批路径和同一套请求响应结构，升档时只替换发送请求的那一层。
            </p>
            <div className="grid gap-3 md:grid-cols-3">
              {LEVEL_CARDS.map((item) => {
                const Icon = item.icon;
                const active = item.level === activeLevel;
                return (
                  <div
                    key={item.level}
                    className={cn(
                      "rounded-lg border p-3",
                      active && "border-primary bg-primary/5"
                    )}
                  >
                    <div className="flex items-center gap-2 text-sm font-medium">
                      <Icon className="size-4" />
                      {item.title}
                      <code className="font-mono text-xs font-normal text-muted-foreground">
                        {item.name}
                      </code>
                    </div>
                    <p className="mt-1.5 text-[13px] text-muted-foreground">{item.work}</p>
                  </div>
                );
              })}
            </div>
            <p>
              签名档的待签名字符串按下列顺序拼接，字段之间用换行分隔，末尾不带换行。
              加密档在签名之外再加密载荷，两层同时生效：AEAD 保证密文未被篡改，
              而服务端公钥是公开的，任何人都能构造出合法密文，签名用来证明调用方持有 appSecret。
            </p>
            <CodeBlock
              language="text"
              title="待签名字符串"
              code={`aegis-hmac-sha256
{appKey}
{大写 HTTP 方法}
{请求路径，不含 query}
{Unix 秒级时间戳}
{随机 nonce，8-128 字符}
{sha256Hex(请求体原始字节)}`}
            />
            <p>
              完整实现见上方「调用示例 → 登录」中对应等级的代码。
            </p>
            <Callout>
              <InlineCode copyable={false}>appSecret</InlineCode> 只能保存在你自己的服务端。
              移动端与前端场景请使用标准档，安全性由 HTTPS 和服务端风控保证。
              另外第三方回跳{" "}
              <InlineCode copyable={false}>/auth/oauth/callback</InlineCode>{" "}
              由第三方平台重定向浏览器发起，客户端无法为它签名或加密，
              加密档下这一跳仍是明文。要求全链路加密时请改用原生 SDK 的{" "}
              <InlineCode copyable={false}>/auth/oauth/exchange</InlineCode>。
            </Callout>
          </Section>

          <Section id="errors" index={4} title="错误码">
            <p>
              错误共用同一个响应信封，<InlineCode copyable={false}>code</InlineCode> 是业务码，
              HTTP 状态码只表达大类。标注为网关层的错误表示请求在进入业务逻辑之前就被拦下，
              原因在请求包装，与账号密码无关。
            </p>
            <div className="overflow-x-auto rounded-lg border">
              <table className="w-full text-[13px]">
                <thead className="bg-muted/50">
                  <tr>
                    <th className="px-3 py-2 text-left font-medium">业务码</th>
                    <th className="px-3 py-2 text-left font-medium">层</th>
                    <th className="px-3 py-2 text-left font-medium">含义</th>
                    <th className="px-3 py-2 text-left font-medium">处理</th>
                  </tr>
                </thead>
                <tbody>
                  {ERROR_CODES.map((row) => (
                    <tr key={row.code} className="border-t">
                      <td className="whitespace-nowrap px-3 py-2 font-mono text-xs">{row.code}</td>
                      <td className="whitespace-nowrap px-3 py-2">
                        <Badge variant={row.layer === "gateway" ? "warning" : "outline"} size="sm">
                          {row.layer === "gateway" ? "网关" : "业务"}
                        </Badge>
                      </td>
                      <td className="px-3 py-2">{row.meaning}</td>
                      <td className="px-3 py-2 text-muted-foreground">{row.fix}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="text-muted-foreground">
              排查包装问题时，可以请应用管理员在控制台运行接入自检。
              服务端会按同一套规格实跑一遍，指出是签名、时间戳还是 AAD 出错。
            </p>
          </Section>

          <Section id="reference" index={5} title="接口清单">
            <p>
              下列路径都位于{" "}
              <InlineCode copyable={false}>{`${base}/api/v1/apps/${appKey}`}</InlineCode> 之下。
            </p>
            <div className="overflow-x-auto rounded-lg border">
              <table className="w-full text-[13px]">
                <tbody>
                  {ENDPOINTS.map((row) => (
                    <tr key={row.path} className="border-b last:border-b-0">
                      <td className="w-16 px-3 py-2 font-mono text-xs text-muted-foreground">
                        {row.method}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 font-mono text-xs">{row.path}</td>
                      <td className="px-3 py-2 text-muted-foreground">{row.note}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="text-muted-foreground">
              用户资料、存储、积分等业务接口见{" "}
              <Link href="/developers/api" className="underline underline-offset-4">
                完整接口文档
              </Link>
              。
            </p>
          </Section>
        </div>
      </div>
    </div>
  );
}
