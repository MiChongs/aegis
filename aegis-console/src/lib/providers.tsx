"use client";

import { ThemeProvider } from "next-themes";
import { QueryClientProvider } from "@tanstack/react-query";
import { ReactNode, useState } from "react";
import { createQueryClient } from "@/lib/query-client";
import { RealtimeProvider } from "@/lib/realtime-provider";
import { BrandingProvider } from "@/lib/branding-provider";
import { GlobalEnhancers } from "@/components/global";
import { SystemThemeWatcher } from "@/lib/theme-transition";
import { PageBoundary } from "@/components/ui/error-boundary";
import { ServiceWorkerRegistrar } from "@/lib/service-worker-registrar";

export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(createQueryClient);

  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="system"
      enableSystem
      // 刻意**不开** disableTransitionOnChange：它会在换主题的那一帧给全局插一条
      // `* { transition: none }`，正好把 globals.css 里那层 280ms 颜色过渡废掉。
      // 手动切换有 View Transitions 的快照挡着，看不见这层过渡；
      // 但跟随系统时是操作系统自己翻昼夜模式，没有快照也没有起点，
      // 全靠这层过渡把硬切变成淡入（详见 lib/theme-transition.tsx）。
      // next-themes 在 body 注入一段内联脚本避免主题闪烁；React 19 会把这段
      // <script> 视为"组件内 script tag"并告警。加 data-ntp 标注 + suppressHydrationWarning
      // 阻止 React 19 告警，功能不受影响。
      scriptProps={{ "data-ntp": "1", suppressHydrationWarning: true }}
    >
      <QueryClientProvider client={queryClient}>
        <BrandingProvider>
          <RealtimeProvider>
            <GlobalEnhancers />
            {/* 跟随系统时给操作系统发起的那次切换放慢过渡（渲染 null） */}
            <SystemThemeWatcher />
            {/* Service Worker（public/sw.js，零依赖）：开发与生产都注册，让所有 <img> +
                /api/storage/proxy 请求走 Cache Storage API 的 CacheFirst 策略 */}
            <ServiceWorkerRegistrar />
            {/* 全局根兜底：任意路由内部抛出的渲染错误都会落到这里，
                不影响 Shell（侧边栏 / 顶栏 / 主题 / 实时等上层能力）。
                子树内可进一步嵌套 WidgetBoundary 做更小范围的局部兜底。 */}
            <PageBoundary>{children}</PageBoundary>
          </RealtimeProvider>
        </BrandingProvider>
      </QueryClientProvider>
    </ThemeProvider>
  );
}
