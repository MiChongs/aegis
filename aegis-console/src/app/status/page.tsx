import { PublicEntryActions } from "@/components/brand/public-entry-actions";
import { PublicHeader } from "@/components/brand/public-header";
import { SiteFooter } from "@/components/brand/site-footer";
import { AvailabilityDashboard } from "@/components/monitor/availability-dashboard";

/**
 * 公开状态页。
 *
 * 与首页共用顶栏与页脚，因此配色也共用同一套语义令牌 —— 旧版这里是一张
 * 写死的深色玻璃画布，浅色模式下从控制台点过来会毫无预告地黑掉一屏。
 */
export default function PublicStatusPage() {
  return (
    // PublicHeader 是整宽 sticky 栏，必须挂在没有左右内边距的容器上，
    // 内容的 px 因此下沉到内层 —— 否则栏会跟着缩进，滚动时和内容错位。
    <div className="flex min-h-screen flex-col bg-background text-foreground">
      <PublicHeader current="status" navLabel="状态页导航" />

      <main className="flex-1">
        <section className="border-b">
          <div className="mx-auto w-full max-w-7xl px-5 py-12 md:px-8 md:py-16">
            <h1 className="text-3xl font-semibold tracking-tight md:text-5xl">服务状态</h1>
            <p className="mt-3 max-w-xl text-sm text-muted-foreground md:text-base">
              系统组件与各应用的实时可用性。数据取自运行中的实例，不经缓存。
            </p>
            <div className="mt-8">
              <PublicEntryActions secondaryHref="/" secondaryLabel="返回首页" />
            </div>
          </div>
        </section>

        <section className="mx-auto w-full max-w-7xl px-5 py-10 md:px-8">
          <AvailabilityDashboard mode="public" showPublicLinks={false} />
        </section>
      </main>

      <SiteFooter />
    </div>
  );
}
