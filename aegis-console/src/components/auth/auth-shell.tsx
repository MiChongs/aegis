import type { ReactNode } from "react";
import { connection } from "next/server";
import { copyrightNotice } from "@/components/brand/home/home-content";
import { AuthMotionProvider } from "@/components/auth/auth-motion";
import { AuthStage } from "@/components/auth/auth-stage";
import { LoginRedirectGuard } from "@/components/auth/login-redirect-guard";
import { getBingWallpaper } from "@/lib/bing-wallpaper";

/**
 * 登录 / 注册共用外壳：必应每日一图全屏铺底，一张玻璃卡片只放表单。
 *
 * 壁纸元数据在服务端取（进程内缓存半小时，2.5 秒超时即按无壁纸处理），首屏就带上图片地址。
 * `connection()` 让两页按请求渲染，不在构建时定格 —— 理由见 `lib/bing-wallpaper.ts`。
 * 加载动画、背景与卡片的入场见 `AuthStage`。两页互为跳转目标，外壳完全一致，切换时只有表单在变。
 */
export async function AuthShell({ children }: { children: ReactNode }) {
  await connection();
  const wallpaper = await getBingWallpaper();
  return (
    <AuthMotionProvider>
      <LoginRedirectGuard />
      <AuthStage wallpaper={wallpaper} footnote={`© ${new Date().getFullYear()} ${copyrightNotice}`}>
        {children}
      </AuthStage>
    </AuthMotionProvider>
  );
}
