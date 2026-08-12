import type { Metadata } from "next";
import { LoginForm } from "@/components/auth/login-form";
import { LoginMotionProvider } from "@/components/auth/login-motion";
import { LoginRedirectGuard } from "@/components/auth/login-redirect-guard";
import { LoginThemeToggle } from "@/components/auth/login-theme-toggle";

export const metadata: Metadata = {
  title: "登录 · Aegis Console",
  description: "Aegis 管理控制台登录入口"
};

/**
 * 登录页 —— 单列居中，一屏之内只有登录这一件事。
 *
 * ── 刻意没有的东西 ──
 * 没有背景（此前的全屏滚动斜线已移除，组件本身保留给注册页 / 状态页 / 品牌首页）、
 * 没有品牌大字、没有能力介绍、没有双栏。会到这个页面的人只有一个目的，
 * 任何"顺便介绍一下"的内容都是在和输入框抢注意力。
 * 层次全部由表面色、边框与字号级差承担，走主题变量，深浅色不需要额外补丁。
 *
 * ── 布局 ──
 * `min-h-svh` 而不是 `min-h-screen`：移动端地址栏收起时 `100vh` 比可视区高一截，
 * 登录按钮会被顶到折叠线以下 —— 这是最不该发生在登录页的事。
 */
export default function LoginPage() {
  return (
    <LoginMotionProvider>
      <main className="relative flex min-h-svh flex-col items-center justify-center px-4 py-10">
        <LoginRedirectGuard />

        {/* 主题开关贴在角上：它是工具，不参与页面的视觉重心 */}
        <div className="absolute top-4 right-4">
          <LoginThemeToggle />
        </div>

        <div className="flex w-full max-w-sm flex-col gap-6">
          <LoginForm />

          <p className="text-center text-[11px] leading-5 text-muted-foreground/60">
            &copy; {new Date().getFullYear()} Aegis Identity Fabric
          </p>
        </div>
      </main>
    </LoginMotionProvider>
  );
}
