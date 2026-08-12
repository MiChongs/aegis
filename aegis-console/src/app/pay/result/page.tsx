import { Suspense } from "react";
import type { Metadata } from "next";
import { LoginBackground } from "@/components/auth/login-background";
import { PayResultView } from "./pay-result-view";

/**
 * 支付结果页 —— 各支付渠道「同步跳转地址」的落点。
 *
 * 面向的是**应用的终端用户**，不是控制台管理员，因此这里刻意不放 PublicHeader：
 * 那条导航会把刚付完钱的普通用户引向后台登录入口。这一页只回答一个问题：
 * 我这笔钱付成功了吗。
 *
 * 不索引：URL 上带着订单号与渠道签名，被搜索引擎收录既无意义也不合适。
 */
export const metadata: Metadata = {
  title: "支付结果",
  robots: { index: false, follow: false }
};

export default function PayResultPage() {
  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden px-5 py-10">
      <LoginBackground />
      <div
        className="pointer-events-none absolute inset-0"
        style={{
          background: [
            "radial-gradient(circle at 20% 20%, rgba(255,255,255,0.1), transparent 28%)",
            "linear-gradient(180deg, rgba(5,8,14,0.32), rgba(5,8,14,0.86))"
          ].join(", ")
        }}
      />
      {/* useSearchParams 需要 Suspense 边界，否则整页会被迫改成动态渲染 */}
      <Suspense fallback={null}>
        <PayResultView />
      </Suspense>
    </main>
  );
}
