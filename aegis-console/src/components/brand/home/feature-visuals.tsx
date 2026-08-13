"use client";

import { Area, AreaChart, Bar, BarChart, Cell } from "recharts";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { cn } from "@/lib/utils";

/**
 * 核心能力卡片里的小可视化。
 *
 * 六张卡各配一个不同形状的图形，而不是六个图标：一屏六张只有文字的卡片
 * 会退化成一段被切碎的说明书，读者的眼睛没有落点。每个图形都在示意这张卡
 * **真正在讲的那件事**（隔离 / 策略表 / 三档叠加 / 风险分布 / 账本 / 沙箱脚本），
 * 不是随便找个形状填空。
 *
 * 两条约束：颜色一律走 `--home-*` 令牌（深浅两套自动切换），
 * 动效只做 transform / opacity 且在 reduce 档下由 CSS 统一停掉。
 */

const FRAME = "relative h-32 overflow-hidden rounded-lg border bg-muted/40";

/* ── 多租户：三块应用瓦片各自独立，逐个亮起 ── */
export function TenancyVisual() {
  const apps = [
    { name: "app_web", users: "128,402" },
    { name: "app_ios", users: "64,915" },
    { name: "app_ops", users: "1,208" },
  ];

  return (
    <div className={cn(FRAME, "flex flex-col justify-center gap-1.5 p-3")}>
      {apps.map((app, index) => (
        <div
          key={app.name}
          className="home-tile-pulse flex items-center gap-2 rounded-md border bg-background/80 px-2.5 py-1.5"
          style={{ animationDelay: `${index * 0.9}s` }}
        >
          <span
            className="size-1.5 shrink-0 rounded-full"
            style={{ background: "var(--home-accent)" }}
          />
          <span className="font-mono text-[10px] text-foreground">{app.name}</span>
          <span className="ml-auto font-mono text-[10px] text-muted-foreground">
            {app.users}
          </span>
        </div>
      ))}
    </div>
  );
}

/* ── 授权：策略表点阵，绿色放行 / 红色显式拒绝 / 灰色未命中 ── */
export function PolicyVisual() {
  // 固定图样而不是随机：随机会让每次刷新看起来像数据在变，而这只是示意
  const cells = [
    2, 1, 0, 1, 1, 0, 1, 2, 1, 0, 1, 1, 1, 0, 0, 1, 1, 2, 0, 1, 0, 1, 1, 0, 1,
    1, 0, 2, 1, 1, 0, 1, 1, 0, 1, 0,
  ];

  return (
    <div className={cn(FRAME, "flex items-center justify-center p-3")}>
      <div className="grid grid-cols-12 gap-1">
        {cells.map((state, index) => (
          <span
            key={index}
            className="size-2 rounded-[2px]"
            style={{
              background:
                state === 1
                  ? "color-mix(in srgb, var(--home-accent-ok) 70%, transparent)"
                  : state === 2
                    ? "color-mix(in srgb, var(--destructive) 70%, transparent)"
                    : "color-mix(in srgb, var(--foreground) 12%, transparent)",
            }}
          />
        ))}
      </div>
    </div>
  );
}

/* ── 接入协议：三档累加，条形逐档变长并换色 ── */
export function ProtocolVisual() {
  const levels = [
    { label: "standard", width: "38%", color: "var(--home-accent)" },
    { label: "signed", width: "68%", color: "var(--home-accent-alt)" },
    { label: "sealed", width: "100%", color: "var(--home-accent-soft)" },
  ];

  return (
    <div className={cn(FRAME, "flex flex-col justify-center gap-2.5 p-3")}>
      {levels.map((level) => (
        <div key={level.label} className="flex items-center gap-2">
          <span className="w-14 shrink-0 font-mono text-[10px] text-muted-foreground">
            {level.label}
          </span>
          <span className="h-1.5 flex-1 overflow-hidden rounded-full bg-foreground/8">
            <span
              className="block h-full rounded-full"
              style={{ width: level.width, background: level.color }}
            />
          </span>
        </div>
      ))}
    </div>
  );
}

/* ── 风控：命中趋势面积图 ── */
const riskConfig = {
  hits: { label: "命中", color: "var(--home-accent)" },
} satisfies ChartConfig;

const riskData = [
  { t: "1", hits: 12 },
  { t: "2", hits: 19 },
  { t: "3", hits: 14 },
  { t: "4", hits: 31 },
  { t: "5", hits: 26 },
  { t: "6", hits: 48 },
  { t: "7", hits: 39 },
  { t: "8", hits: 62 },
  { t: "9", hits: 51 },
  { t: "10", hits: 44 },
];

export function RiskVisual() {
  return (
    <div className={FRAME}>
      <ChartContainer config={riskConfig} className="aspect-auto size-full">
        <AreaChart data={riskData} margin={{ top: 8, right: 0, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id="home-risk-fill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--color-hits)" stopOpacity={0.35} />
              <stop offset="100%" stopColor="var(--color-hits)" stopOpacity={0} />
            </linearGradient>
          </defs>
          <Area
            dataKey="hits"
            type="natural"
            stroke="var(--color-hits)"
            strokeWidth={2}
            fill="url(#home-risk-fill)"
            isAnimationActive={false}
          />
        </AreaChart>
      </ChartContainer>
    </div>
  );
}

/* ── 资产与交易：进项与出项分色的柱状图 ──
   进项与出项必须分色系，否则「收了 10 万」和「退了 10 万」在图上一模一样。 */
const ledgerConfig = {
  amount: { label: "金额", color: "var(--home-accent-ok)" },
} satisfies ChartConfig;

const ledgerData = [
  { d: "1", amount: 42, out: false },
  { d: "2", amount: 58, out: false },
  { d: "3", amount: -18, out: true },
  { d: "4", amount: 74, out: false },
  { d: "5", amount: 66, out: false },
  { d: "6", amount: -26, out: true },
  { d: "7", amount: 88, out: false },
  { d: "8", amount: 71, out: false },
];

export function LedgerVisual() {
  return (
    <div className={FRAME}>
      <ChartContainer config={ledgerConfig} className="aspect-auto size-full">
        <BarChart data={ledgerData} margin={{ top: 10, right: 8, bottom: 8, left: 8 }}>
          <Bar dataKey="amount" radius={2} isAnimationActive={false}>
            {ledgerData.map((entry) => (
              <Cell
                key={entry.d}
                fill={entry.out ? "var(--home-accent-warm)" : "var(--home-accent-ok)"}
              />
            ))}
          </Bar>
        </BarChart>
      </ChartContainer>
    </div>
  );
}

/* ── 远程函数：沙箱脚本片段 + 打字光标 ── */
export function FunctionVisual() {
  const lines: { text: string; tone?: "keyword" | "muted" }[] = [
    { text: "// 试跑读真数据，副作用不落库", tone: "muted" },
    { text: "export default async (ctx) => {" },
    { text: "  const u = await aegis.user.get(ctx.userId)" },
    { text: "  return { vip: u.vip.active }" },
  ];

  return (
    <div className={cn(FRAME, "flex flex-col justify-center gap-0.5 p-3 font-mono text-[10px]")}>
      {lines.map((line) => (
        <span
          key={line.text}
          className={cn(
            "truncate",
            line.tone === "muted" ? "text-muted-foreground/70" : "text-muted-foreground"
          )}
        >
          {line.text}
        </span>
      ))}
      <span className="flex items-center gap-1 text-muted-foreground">
        {"}"}
        <span
          className="home-caret inline-block h-3 w-[2px]"
          style={{ background: "var(--home-accent)" }}
        />
      </span>
    </div>
  );
}
