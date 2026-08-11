"use client";

import { ThemeProvider } from "next-themes";
import { QueryClientProvider } from "@tanstack/react-query";
import { ReactNode, useState } from "react";
import { createQueryClient } from "@/lib/query-client";
import { RealtimeProvider } from "@/lib/realtime-provider";
import { BrandingProvider } from "@/lib/branding-provider";
import { GlobalEnhancers } from "@/components/global";
import { PageBoundary } from "@/components/ui/error-boundary";
import { ServiceWorkerRegistrar } from "@/lib/service-worker-registrar";

export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(createQueryClient);

  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="system"
      enableSystem
      disableTransitionOnChange
      // next-themes 在 body 注入一段内联脚本避免主题闪烁；React 19 会把这段
      // <script> 视为"组件内 script tag"并告警。加 data-ntp 标注 + suppressHydrationWarning
      // 阻止 React 19 告警，功能不受影响。
      scriptProps={{ "data-ntp": "1", suppressHydrationWarning: true }}
    >
      <QueryClientProvider client={queryClient}>
        <BrandingProvider>
          <RealtimeProvider>
            <GlobalEnhancers />
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
