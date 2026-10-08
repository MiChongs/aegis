import Link from "next/link";
import type { ReactNode } from "react";
import { BadgeCheck, Fingerprint, Layers, ShieldCheck, ShieldX } from "lucide-react";
import { AegisMark } from "@/components/brand/aegis-mark";
import { copyrightNotice } from "@/components/brand/home/home-content";
import { AuthMotionProvider } from "@/components/auth/auth-motion";
import { LoginRedirectGuard } from "@/components/auth/login-redirect-guard";
import { LoginThemeToggle } from "@/components/auth/login-theme-toggle";

const FEATURES = [
  { icon: Layers, title: "多应用隔离", desc: "每个应用独立的用户库、认证策略与配额" },
  { icon: Fingerprint, title: "多因子认证", desc: "两步验证、通行密钥与一次性恢复码" },
  { icon: ShieldCheck, title: "审计与风控", desc: "全量操作留痕，异常登录实时拦截" }
] as const;

/** 品牌面板上的示意卡片：只展示状态形态，不放任何数字，避免被当成真实数据 */
const PREVIEW_EVENTS = [
  { icon: BadgeCheck, label: "密码登录", state: "已通过", tone: "text-emerald-400" },
  { icon: Fingerprint, label: "通行密钥", state: "已通过", tone: "text-emerald-400" },
  { icon: ShieldX, label: "异地登录", state: "已拦截", tone: "text-red-400" }
] as const;

function BrandPanel() {
  return (
    <aside className="relative hidden overflow-hidden rounded-3xl bg-zinc-950 text-zinc-50 lg:flex lg:flex-col dark:bg-zinc-900 dark:ring-1 dark:ring-white/10">
      {/* 深色主题的页面底色同为 zinc-950，面板提一级并描一圈细边，否则边界会消失 */}
      {/* 点阵底纹：纯 SVG 图案，不用渐变 */}
      <svg aria-hidden className="pointer-events-none absolute inset-0 size-full text-white/[0.06]">
        <defs>
          <pattern id="auth-dots" width="22" height="22" patternUnits="userSpaceOnUse">
            <circle cx="1.5" cy="1.5" r="1.1" fill="currentColor" />
          </pattern>
        </defs>
        <rect width="100%" height="100%" fill="url(#auth-dots)" />
      </svg>

      <div className="relative flex flex-1 flex-col p-10 xl:p-12">
        <div className="flex items-center gap-2.5">
          <span className="flex size-9 items-center justify-center rounded-xl bg-white/10 ring-1 ring-white/15">
            <AegisMark static className="size-5 text-white" />
          </span>
          <span className="text-[15px] font-semibold tracking-tight">Aegis 控制台</span>
        </div>

        <div className="mt-auto max-w-md">
          <h2 className="text-[34px] leading-[1.15] font-semibold tracking-tight xl:text-[40px]">
            统一身份底座
          </h2>
          <p className="mt-4 text-[15px] leading-7 text-zinc-400">
            为每个应用建立清晰、稳定、可治理的用户与权限边界。
          </p>

          <ul className="mt-9 space-y-5">
            {FEATURES.map((feature) => (
              <li key={feature.title} className="flex gap-3.5">
                <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-white/[0.07] ring-1 ring-white/10">
                  <feature.icon className="size-[18px] text-zinc-200" />
                </span>
                <span>
                  <span className="block text-sm font-medium text-zinc-100">{feature.title}</span>
                  <span className="mt-0.5 block text-[13px] text-zinc-400">{feature.desc}</span>
                </span>
              </li>
            ))}
          </ul>
        </div>

        <div className="mt-10 max-w-sm rounded-2xl bg-zinc-900 p-4 ring-1 ring-white/10 dark:bg-zinc-950" aria-hidden>
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-zinc-300">认证事件</span>
            <span className="flex items-center gap-1.5 text-[11px] text-zinc-500">
              <span className="size-1.5 rounded-full bg-emerald-400" />
              实时
            </span>
          </div>
          <ul className="mt-3 space-y-2">
            {PREVIEW_EVENTS.map((event) => (
              <li key={event.label} className="flex items-center gap-2.5 rounded-lg bg-white/[0.04] px-3 py-2">
                <event.icon className={`size-4 shrink-0 ${event.tone}`} />
                <span className="flex-1 text-[13px] text-zinc-300">{event.label}</span>
                <span className={`text-[12px] font-medium ${event.tone}`}>{event.state}</span>
              </li>
            ))}
          </ul>
        </div>

        <div className="mt-10 flex items-center justify-between text-[12px] text-zinc-500">
          <span>&copy; {new Date().getFullYear()} {copyrightNotice}</span>
          <Link href="/status" className="transition-colors hover:text-zinc-300">
            服务状态
          </Link>
        </div>
      </div>
    </aside>
  );
}

/**
 * 登录 / 注册共用外壳。
 *
 * 桌面端左右分栏：左侧品牌面板交代「这是什么」，右侧开阔的表单区只做一件事。
 * 两页互为跳转目标，外壳完全一致，切换时只有表单内容在变。
 *
 * 移动端只剩表单一栏：顶部压缩成一行品牌条，输入框与按钮按 44px 触控高度，
 * 用 `min-h-svh` 而不是 `100vh`（地址栏收起时 100vh 会把按钮顶到折叠线以下），
 * 并给刘海与底部手势区留出安全距离。
 */
export function AuthShell({ children }: { children: ReactNode }) {
  return (
    <AuthMotionProvider>
      <div className="min-h-svh bg-background lg:grid lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)] lg:gap-3 lg:p-3 xl:grid-cols-[minmax(0,1fr)_minmax(0,1.25fr)]">
        <LoginRedirectGuard />
        <BrandPanel />

        <main className="flex min-h-svh flex-col px-5 pt-[max(1rem,env(safe-area-inset-top))] pb-[max(1.25rem,env(safe-area-inset-bottom))] sm:px-8 lg:min-h-0 lg:px-10">
          <header className="flex h-12 items-center justify-between">
            <Link href="/" className="flex items-center gap-2 lg:invisible" aria-label="Aegis 首页">
              <span className="flex size-8 items-center justify-center rounded-lg bg-foreground text-background">
                <AegisMark static className="size-[18px]" />
              </span>
              <span className="text-sm font-semibold tracking-tight">Aegis 控制台</span>
            </Link>
            <LoginThemeToggle />
          </header>

          <div className="flex flex-1 items-center justify-center py-8 sm:py-12">
            <div className="w-full max-w-[400px]">{children}</div>
          </div>

          <p className="text-center text-[11px] leading-5 text-muted-foreground/70 lg:hidden">
            &copy; {new Date().getFullYear()} {copyrightNotice}
          </p>
        </main>
      </div>
    </AuthMotionProvider>
  );
}
