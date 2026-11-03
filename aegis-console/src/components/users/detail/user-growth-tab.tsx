"use client";

/**
 * 奖惩：这个用户获取积分与经验的速度、等级的上下限，以及改动它们的全部入口（docs/growth-control.md）。
 *
 * 自上而下：
 *   两块倍率表盘（积分 / 经验，综合了个人与全应用规则）+ 一键处罚 / 奖励
 *   各来源实际倍率矩阵（规则可以只作用于部分来源，这里看清每个来源最终是多少）
 *   等级控制（封顶 / 降级 / 强制升级 / 锁定）
 *   个人规则与同时作用于他的全应用规则
 *   规则作用效果、收入来源、调控记录
 *
 * 总览接口在服务端并行跑九组查询；页面只发一次请求，规则变化后整体失效重取。
 */

import * as React from "react";
import { Coins, Gauge, Globe, ListFilter, Plus, Scale, Snowflake, Sparkles, TrendingDown, TrendingUp } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useNow } from "@/components/users/ban-form";
import type { GrowthKind, GrowthKindView, GrowthRule } from "@/lib/api/growth";
import { useGrowthCatalogQuery, useUserGrowthQuery } from "@/lib/growth-hooks";
import { cn } from "@/lib/utils";
import { Section, StateBlock, errorMessage } from "./kit";
import { Segmented } from "./user-assets-adjust";
import { GrowthSourceStats, GrowthTimeline, GrowthTrend } from "./growth/growth-insights";
import { CapMeter, EffectPill, FactorText, ROUNDING_LABEL, RateDial, effectOf } from "./growth/growth-kit";
import { GrowthLevelPanel } from "./growth/growth-level-panel";
import { GrowthRuleEditor, type GrowthRuleSeed } from "./growth/growth-rule-editor";
import { GrowthRuleList } from "./growth/growth-rule-list";

type EditorState = { rule: GrowthRule | null; kind: GrowthKind; scope: "user" | "app"; seed?: GrowthRuleSeed } | null;

const QUICK: Array<{ key: string; label: string; icon: React.ComponentType<{ className?: string }>; seed: GrowthRuleSeed; tone: string }> = [
  { key: "freeze", label: "冻结获取", icon: Snowflake, seed: { numerator: 0, denominator: 1, duration: "7d" }, tone: "text-red-600 dark:text-red-400" },
  { key: "half", label: "减半 7 天", icon: TrendingDown, seed: { numerator: 1, denominator: 2, duration: "7d" }, tone: "text-amber-600 dark:text-amber-400" },
  { key: "cap", label: "每日限量", icon: Scale, seed: { numerator: 1, denominator: 1, dailyCap: 100, duration: "30d" }, tone: "text-amber-600 dark:text-amber-400" },
  { key: "double", label: "双倍 7 天", icon: TrendingUp, seed: { numerator: 2, denominator: 1, duration: "7d" }, tone: "text-emerald-600 dark:text-emerald-400" }
];

export function UserGrowthTab({ appKey, userId }: { appKey: string; userId: number }) {
  const now = useNow(30_000);
  const catalogQuery = useGrowthCatalogQuery(appKey);
  const profileQuery = useUserGrowthQuery(appKey, userId);
  const profile = profileQuery.data;
  const catalog = catalogQuery.data;
  const [editor, setEditor] = React.useState<EditorState>(null);
  const [ruleFilter, setRuleFilter] = React.useState<"live" | "all">("live");

  const openNew = (kind: GrowthKind, scope: "user" | "app" = "user", seed?: GrowthRuleSeed) => setEditor({ rule: null, kind, scope, seed });

  if (profileQuery.isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <div className="grid gap-4 lg:grid-cols-2">
          <Skeleton className="h-64 rounded-xl" />
          <Skeleton className="h-64 rounded-xl" />
        </div>
        <Skeleton className="h-40 rounded-xl" />
        <Skeleton className="h-56 rounded-xl" />
      </div>
    );
  }
  if (!profile) {
    return (
      <Card className="py-0">
        <StateBlock kind="error" title="奖惩信息加载失败" description={errorMessage(profileQuery.error)} onRetry={() => profileQuery.refetch()} />
      </Card>
    );
  }

  const liveRules = profile.rules.filter((rule) => rule.status !== "revoked" && rule.status !== "expired");
  const shownRules = ruleFilter === "live" ? liveRules : profile.rules;

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-4 lg:grid-cols-2">
        <RateCard view={profile.integral} icon={Coins} title="积分获取" onCreate={(seed) => openNew("integral", "user", seed)} />
        <RateCard view={profile.experience} icon={Sparkles} title="经验获取" onCreate={(seed) => openNew("experience", "user", seed)} />
      </div>

      <SourceMatrix integral={profile.integral} experience={profile.experience} />

      <GrowthLevelPanel appKey={appKey} userId={userId} level={profile.level} />

      <Section
        title="个人规则"
        icon={ListFilter}
        tone="warning"
        description={`只作用于这个用户；同一类型最多 ${catalog?.limits.maxActiveRules ?? 20} 条未结束的规则`}
        actions={
          <>
            <Segmented
              value={ruleFilter}
              onChange={setRuleFilter}
              options={[
                { value: "live", label: `进行中 ${liveRules.length}` },
                { value: "all", label: `全部 ${profile.rules.length}` }
              ]}
            />
            <Button size="sm" onClick={() => openNew("integral")}>
              <Plus />
              新建规则
            </Button>
          </>
        }
        flush
        bodyClassName="px-4 sm:px-5"
      >
        <GrowthRuleList
          appKey={appKey}
          rules={shownRules}
          catalog={catalog}
          now={now}
          onEdit={(rule) => setEditor({ rule, kind: rule.kind, scope: rule.userId > 0 ? "user" : "app" })}
          empty={
            <StateBlock
              compact
              icon={Gauge}
              title={ruleFilter === "live" ? "没有进行中的个人规则" : "还没有个人规则"}
              description="这个用户按应用的默认速度获取积分与经验。可以在上方表盘用一键处罚 / 奖励快速建立。"
            />
          }
        />
      </Section>

      <Section
        title="全应用规则"
        icon={Globe}
        tone="info"
        description="同时作用于全部用户（也包括这个用户），与个人规则叠乘"
        actions={
          <Button size="sm" variant="outline" onClick={() => openNew("integral", "app")}>
            <Plus />
            新建全应用规则
          </Button>
        }
        flush
        bodyClassName="px-4 sm:px-5"
      >
        <GrowthRuleList
          appKey={appKey}
          rules={profile.appRules}
          catalog={catalog}
          now={now}
          onEdit={(rule) => setEditor({ rule, kind: rule.kind, scope: "app" })}
          empty={<StateBlock compact icon={Globe} title="没有全应用规则" description="例如「周末双倍积分」「新版本经验 ×1.5」都可以建成全应用规则。" />}
        />
      </Section>

      <div className="grid gap-4 xl:grid-cols-2">
        <GrowthTrend usage={profile.usage} />
        <GrowthSourceStats integral={profile.integral.stats} experience={profile.experience.stats} catalog={catalog} days={profile.statsDays} />
      </div>

      <GrowthTimeline events={profile.events} />

      <GrowthRuleEditor
        open={Boolean(editor)}
        onOpenChange={(open) => (!open ? setEditor(null) : undefined)}
        appKey={appKey}
        userId={userId}
        catalog={catalog}
        rule={editor?.rule}
        defaultKind={editor?.kind}
        defaultScope={editor?.scope}
        seed={editor?.seed}
      />
    </div>
  );
}

/** 一种数值的获取速度：表盘、综合倍率、取整、今日上限进度、一键处罚 / 奖励。 */
function RateCard({
  view,
  icon: Icon,
  title,
  onCreate
}: {
  view: GrowthKindView;
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  onCreate: (seed?: GrowthRuleSeed) => void;
}) {
  const effect = effectOf(view.factor.num, view.factor.den, view.dailyCap);
  const touched = view.liveRules > 0;
  return (
    <Card className="gap-0 overflow-hidden py-0">
      <div className="flex items-center justify-between gap-3 px-4 pt-4 sm:px-5">
        <div className="flex items-center gap-2">
          <Icon className="size-4 text-muted-foreground" />
          <h3 className="text-sm font-semibold tracking-tight">{title}</h3>
          {touched ? <EffectPill effect={effect} /> : <Badge variant="secondary" size="sm">默认速度</Badge>}
        </div>
        <span className="text-xs text-muted-foreground">{view.liveRules} 条规则生效</span>
      </div>
      <div className="flex flex-col items-center gap-1 px-4 pt-2 sm:flex-row sm:items-end sm:gap-5 sm:px-5">
        <RateDial num={view.factor.num} den={view.factor.den} dailyCap={view.dailyCap} />
        <div className="flex min-w-0 flex-1 flex-col gap-2 pb-3 text-center sm:text-left">
          <div className="flex flex-col">
            <span className="text-xs text-muted-foreground">综合倍率</span>
            <FactorText num={view.factor.num} den={view.factor.den} dailyCap={view.dailyCap} className="text-4xl leading-tight" />
          </div>
          <div className="text-xs text-muted-foreground">
            {touched ? `${ROUNDING_LABEL[view.rounding]}` : "未受任何规则作用"}
            {view.restricted && view.boosted ? "，部分来源加速、部分减速" : ""}
          </div>
          <CapMeter used={view.usedToday} cap={view.dailyCap} />
        </div>
      </div>
      <div className="grid grid-cols-2 border-t sm:grid-cols-4">
        {QUICK.map((item, index) => (
          <button
            key={item.key}
            type="button"
            onClick={() => onCreate(item.seed)}
            className={cn(
              "flex items-center justify-center gap-1.5 px-2 py-2.5 text-xs font-medium transition-colors hover:bg-accent/60",
              index > 0 && "sm:border-l",
              index % 2 === 1 && "border-l",
              index >= 2 && "border-t sm:border-t-0"
            )}
          >
            <item.icon className={cn("size-3.5", item.tone)} />
            {item.label}
          </button>
        ))}
      </div>
    </Card>
  );
}

/** 各来源最终倍率：规则可以只作用于部分来源，矩阵把「签到 ÷2、抽奖 ×1」这种差异摆平。 */
function SourceMatrix({ integral, experience }: { integral: GrowthKindView; experience: GrowthKindView }) {
  const keys = Array.from(new Set([...integral.bySource.map((s) => s.source), ...experience.bySource.map((s) => s.source)]));
  const lookup = (view: GrowthKindView, key: string) => view.bySource.find((item) => item.source === key);
  const label = (key: string) => lookup(integral, key)?.label ?? lookup(experience, key)?.label ?? key;
  return (
    <Section title="各来源实际倍率" icon={Gauge} tone="primary" description="个人与全应用规则叠乘之后，每个来源此刻的结果；未受管的收入（管理员调整、充值、会员赠送、退回）不在此列" flush>
      <div className="grid grid-cols-[minmax(0,1fr)_repeat(2,minmax(0,8rem))] text-sm">
        <div className="border-b bg-muted/30 px-4 py-2 text-xs text-muted-foreground sm:px-5">来源</div>
        <div className="border-b bg-muted/30 px-3 py-2 text-center text-xs text-muted-foreground">积分</div>
        <div className="border-b bg-muted/30 px-3 py-2 text-center text-xs text-muted-foreground">经验</div>
        {keys.map((key, index) => (
          <React.Fragment key={key}>
            <div className={cn("px-4 py-2.5 sm:px-5", index > 0 && "border-t")}>{label(key)}</div>
            <MatrixCell effect={lookup(integral, key)} first={index === 0} />
            <MatrixCell effect={lookup(experience, key)} first={index === 0} />
          </React.Fragment>
        ))}
      </div>
    </Section>
  );
}

function MatrixCell({ effect, first }: { effect?: GrowthKindView["bySource"][number]; first: boolean }) {
  return (
    <div className={cn("flex flex-col items-center justify-center px-3 py-2", !first && "border-t")}>
      {effect ? (
        <>
          {effect.ruleIds.length ? (
            <FactorText num={effect.factor.num} den={effect.factor.den} dailyCap={effect.dailyCap} className="text-sm" />
          ) : (
            <span className="text-sm text-muted-foreground tabular-nums">×1</span>
          )}
          {effect.dailyCap > 0 ? <span className="text-[10px] text-muted-foreground">每日 ≤ {effect.dailyCap.toLocaleString("zh-CN")}</span> : null}
        </>
      ) : (
        <span className="text-xs text-muted-foreground/60">不适用</span>
      )}
    </div>
  );
}
