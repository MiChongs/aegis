import { LoginBackground } from "@/components/auth/login-background";
import { LoginBrandPanel } from "@/components/auth/login-brand-panel";
import { LoginRedirectGuard } from "@/components/auth/login-redirect-guard";
import { RegisterForm } from "@/components/auth/register-form";

/**
 * 注册页
 *
 * 布局与登录页 1:1 对齐：
 *   - 单张全屏 Three.js Waves 背景（LoginBackground）
 *   - 内容居中在两列网格里：**左列表单（RegisterForm）+ 右列品牌文案（LoginBrandPanel）**
 *   - 表单卡片不变（保留 RegisterForm 全部业务逻辑：注册、验证码、协议勾选等）
 *   - 移动端（<lg）自动堆叠为上下两段
 *   - 底部版权
 */
export default function RegisterPage() {
  return (
    <main className="relative min-h-screen w-full overflow-hidden bg-background">
      <LoginRedirectGuard />

      {/* 流动渐变背景（全屏铺底） */}
      <LoginBackground />

      {/* 内容网格：lg+ 两列；移动端自然堆叠 */}
      <div className="relative z-10 mx-auto flex min-h-screen max-w-7xl flex-col items-center justify-center gap-10 px-6 py-14 lg:grid lg:grid-cols-2 lg:items-center lg:gap-20 lg:px-12 xl:gap-28 xl:px-16">
        {/* 左列：注册表单 */}
        <div className="flex w-full justify-center lg:justify-end">
          <RegisterForm />
        </div>

        {/* 右列：品牌文案 */}
        <div className="w-full max-w-xl lg:max-w-2xl">
          <LoginBrandPanel />
        </div>
      </div>

      {/* 底部版权 —— 压到页面最底、不打扰主体 */}
      <p className="pointer-events-none absolute inset-x-0 bottom-4 z-10 text-center text-[11px] text-muted-foreground/60 select-none">
        &copy; {new Date().getFullYear()} Aegis Identity Fabric
      </p>
    </main>
  );
}
