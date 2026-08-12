import { LoginBackground } from "@/components/auth/login-background";
import { PublicEntryActions } from "@/components/brand/public-entry-actions";
import { PublicHeader } from "@/components/brand/public-header";
import { AvailabilityDashboard } from "@/components/monitor/availability-dashboard";

export default function PublicStatusPage() {
  return (
    // PublicHeader 现在是整宽 sticky 栏，必须挂在没有左右内边距的容器上，
    // 内容的 px 因此下沉到内层 —— 否则栏会跟着缩进，滚动时和内容错位。
    <main className="relative min-h-screen text-white">
      <div className="pointer-events-none fixed inset-0 overflow-hidden">
        <LoginBackground />

        {/* 渐变遮罩 */}
        <div
          className="absolute inset-0"
          style={{
            background: [
              "radial-gradient(circle at 18% 18%, rgba(255,255,255,0.12), transparent 26%)",
              "radial-gradient(circle at 84% 22%, rgba(255,255,255,0.08), transparent 22%)",
              "linear-gradient(180deg, rgba(5,8,14,0.3), rgba(5,8,14,0.82))",
            ].join(", "),
          }}
        />
      </div>

      <PublicHeader current="status" navLabel="状态页导航" />

      <section className="relative z-10 mx-auto w-full max-w-[1440px] px-5 pt-8 pb-6 md:px-8 max-md:pt-6 xl:px-12">
        <div
          className="flex flex-col gap-6 rounded-[36px] p-6 md:p-8 max-md:rounded-[28px] max-md:p-5"
          style={{
            border: "1px solid rgba(255,255,255,0.1)",
            background:
              "linear-gradient(145deg, rgba(255,255,255,0.08), rgba(255,255,255,0.02)), rgba(10,14,21,0.34)",
            backdropFilter: "blur(18px)",
          }}
        >
          <div className="flex flex-col gap-3">
            <h1
              style={{
                fontFamily: "var(--font-data)",
                fontSize: "clamp(2.1rem, 6vw, 4.75rem)",
                fontWeight: 700,
                lineHeight: 0.92,
                letterSpacing: "0.12em",
                textTransform: "uppercase",
                color: "#ffffff",
              }}
            >
              服务状态
            </h1>
            <p
              className="text-sm font-medium md:text-base max-md:text-xs"
              style={{ letterSpacing: "0.14em", color: "rgba(255,255,255,0.68)" }}
            >
              系统总览与应用级可用性
            </p>
          </div>
          <PublicEntryActions secondaryHref="/" secondaryLabel="返回首页" />
        </div>
      </section>

      <section className="brand-status-page__dashboard relative z-10 mx-auto w-full max-w-[1440px] px-5 pb-10 md:px-8 xl:px-12">
        <AvailabilityDashboard mode="public" showPublicLinks={false} />
      </section>
    </main>
  );
}
