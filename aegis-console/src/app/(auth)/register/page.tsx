import type { Metadata } from "next";
import { AuthMotionProvider } from "@/components/auth/auth-motion";
import { LoginRedirectGuard } from "@/components/auth/login-redirect-guard";
import { LoginThemeToggle } from "@/components/auth/login-theme-toggle";
import { RegisterForm } from "@/components/auth/register-form";

export const metadata: Metadata = {
  title: "注册 · Aegis Console",
  description: "创建 Aegis 管理控制台账号"
};

/**
 * 注册页 —— 与登录页共用同一个骨架：单列居中，一屏之内只有这一件事。
 *
 * 两页互为跳转目标（「还没有账号 / 已有账号」），换页时除了卡片里的内容，
 * 其余一切都不该变位置 —— 版式跳一下会让人以为自己点错了。
 *
 * 同样没有背景：此前的全屏滚动斜线已移除（组件本身保留给状态页与品牌首页），
 * 也没有品牌大字与能力介绍。注册页要做的只有把这张表填完。
 */
export default function RegisterPage() {
  return (
    <AuthMotionProvider>
      <main className="relative flex min-h-svh flex-col items-center justify-center px-4 py-10">
        <LoginRedirectGuard />

        <div className="absolute top-4 right-4">
          <LoginThemeToggle />
        </div>

        <div className="flex w-full max-w-sm flex-col gap-6">
          <RegisterForm />

          <p className="text-center text-[11px] leading-5 text-muted-foreground/60">
            &copy; {new Date().getFullYear()} Aegis Identity Fabric
          </p>
        </div>
      </main>
    </AuthMotionProvider>
  );
}
