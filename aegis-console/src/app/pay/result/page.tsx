import { Suspense } from "react";
import type { Metadata } from "next";
import { AegisMark } from "@/components/brand/aegis-mark";
import { PayResultNote, PayResultSkeleton, PayResultView } from "./pay-result-view";

/**
 * 支付结果页 —— 各支付渠道「同步跳转地址」的落点。
 *
 * 面向的是**应用的终端用户**，不是控制台管理员，因此这里刻意不放 PublicHeader：
 * 那条导航会把刚付完钱的普通用户引向后台登录入口。这一页只回答一个问题：
 * 我这笔钱付成功了吗。
 *
 * **背景不复用登录页那套滚动条纹**（`LoginBackground`）。那是给品牌门面用的
 * 高饱和动效，压在一张需要被逐字读完的收银小票上只会抢注意力；它还固定按深色
 * 明度设计，逼得整页只能用写死的白字与 rgba 玻璃拟态，跟随系统开浅色的手机
 * 浏览器上对比度直接塌掉。现在整页只用主题令牌上色，深浅两套都成立。
 *
 * 不索引：URL 上带着订单号与渠道签名，被搜索引擎收录既无意义也不合适。
 */
export const metadata: Metadata = {
  title: "支付结果",
  robots: { index: false, follow: false }
};

export default function PayResultPage() {
  return (
    <main className="relative flex min-h-svh flex-col items-center justify-center px-4 py-10">
      {/* 顶部一层极浅的令牌渐变，给纯色页面一点纵深；不含任何写死的色值 */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-x-0 top-0 -z-10 h-72 bg-gradient-to-b from-muted to-transparent"
      />

      <div className="w-full max-w-md space-y-5">
        <header className="flex items-center justify-center gap-2 text-muted-foreground">
          <AegisMark className="size-5" />
          <span className="text-sm font-medium tracking-wide">Aegis</span>
        </header>

        {/* useSearchParams 需要 Suspense 边界，否则整页会被迫改成动态渲染。
            fallback 用与真实内容同形的骨架：这一页承载的是「我的钱怎么样了」，
            布局跳变会被读成状态跳变。 */}
        <Suspense fallback={<PayResultSkeleton />}>
          <PayResultView />
        </Suspense>

        <PayResultNote />
      </div>
    </main>
  );
}
