import { LoginBackground } from "@/components/auth/login-background";
import { LoginBrandPanel } from "@/components/auth/login-brand-panel";
import { LoginRedirectGuard } from "@/components/auth/login-redirect-guard";
import { LoginForm } from "@/components/auth/login-form";

/**
 * 登录页
 *
 * 布局参考（基于项目 Radix / shadcn 设计语言，Zinc 色阶，深浅色自适应）：
 *   - 单张全屏流动渐变背景（Sky / Cyan / Indigo 协调色 + 多层模糊色块循环位移）
 *   - 内容居中在两列网格里：**左列表单 + 右列品牌文案**
 *   - 表单卡片（`<LoginForm>`）保留现有业务逻辑不动，视觉上以 `bg-card` + 轻投影贴近参考图
 *   - 右列是大字号 Aegis 品牌 + 辅助描述，不再堆砌能力徽章
 *   - 移动端（<lg）自动堆叠为上下两段，保证表单在首屏可见
 */
export default function LoginPage() {
  return (
    <main className="relative min-h-screen w-full overflow-hidden bg-background">
      <LoginRedirectGuard />

      {/* 流动渐变背景（全屏铺底） */}
      <LoginBackground />

      {/* 内容网格：lg+ 两列；移动端自然堆叠 */}
      <div className="relative z-10 mx-auto flex min-h-screen max-w-7xl flex-col items-center justify-center gap-10 px-6 py-14 lg:grid lg:grid-cols-2 lg:items-center lg:gap-20 lg:px-12 xl:gap-28 xl:px-16">
        {/* 左列：登录表单 */}
        <div className="flex w-full justify-center lg:justify-end">
          <LoginForm />
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
