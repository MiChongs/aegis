import { Badge } from "@/components/ui/badge";
import { SurfaceCard } from "@/components/ui/surface-card";

const timeline = [
  {
    title: "网关安全策略已启用",
    summary: "限流、WAF 与应用加密已接入统一入口。",
    time: "02 分钟前"
  },
  {
    title: "GeoIP 本地库已启用",
    summary: "定位优先走本地库，减少外部依赖。",
    time: "17 分钟前"
  },
  {
    title: "工作流队列运行正常",
    summary: "任务队列无明显积压。",
    time: "35 分钟前"
  }
];

export function ActivityFeed() {
  return (
    <SurfaceCard>
      <div className="space-y-5">
        <div className="space-y-2">
          <Badge variant="outline">Recent</Badge>
          <h2 className="text-lg font-semibold text-foreground">近期变更</h2>
        </div>
        <div className="space-y-4">
        {timeline.map((item) => (
          <article className="panel-muted grid gap-3 p-4 md:grid-cols-[12px_minmax(0,1fr)]" key={item.title}>
            <div className="mt-2 size-3 rounded-full bg-primary/85 shadow-[0_0_0_5px_rgba(37,99,235,0.12)]" />
            <div className="space-y-2">
              <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
                <h3 className="text-sm font-semibold text-foreground">{item.title}</h3>
                <span className="text-xs uppercase tracking-[0.14em] text-muted-foreground">{item.time}</span>
              </div>
              <p className="text-sm leading-6 text-muted-foreground">{item.summary}</p>
            </div>
          </article>
        ))}
        </div>
      </div>
    </SurfaceCard>
  );
}
