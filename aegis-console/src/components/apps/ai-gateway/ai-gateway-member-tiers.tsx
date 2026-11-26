"use client";

import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Crown, Gem, GripVertical, Plus, Trash2, UserRound, Users } from "lucide-react";
import { discountLabel, formatCompactPoints, formatRate } from "@/components/apps/ai-gateway/ai-gateway-billing-kit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

/** 草稿里数字一律是字符串（输入过程中的空串不被 Number() 变成 0）。 */
export type MemberTierDraft = {
  uid: string;
  tierId: number;
  label: string;
  multiplier: string;
  daily: string;
  monthly: string;
  packDiscount: string;
  packBonusRate: string;
  rpm: string;
  maxConcurrent: string;
};

export type GeneralVipDraft = {
  vipMultiplier: string;
  vipDaily: string;
  vipMonthly: string;
  vipPackDiscount: string;
  vipPackBonusRate: string;
};

export type TierOption = { id: number; name: string };

let tierSeed = 0;

/** 会员档位优惠：上面一张对比表看清各档位差在哪，下面逐档编辑（拖动排优先级）。 */
export function MemberTiersSection({
  tiers,
  value,
  onChange,
  general,
  onGeneralChange,
  appRpm,
  appConcurrent
}: {
  tiers: TierOption[];
  value: MemberTierDraft[];
  onChange: (next: MemberTierDraft[]) => void;
  general: GeneralVipDraft;
  onGeneralChange: (next: GeneralVipDraft) => void;
  appRpm: number;
  appConcurrent: number;
}) {
  const used = new Set(value.map((item) => item.tierId));
  const available = tiers.filter((tier) => !used.has(tier.id));
  const tierName = (id: number) => tiers.find((tier) => tier.id === id)?.name;

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    const ids = value.map((item) => item.uid);
    onChange(arrayMove(value, ids.indexOf(String(active.id)), ids.indexOf(String(over.id))));
  };
  const add = () => {
    const tier = available[0];
    if (!tier) return;
    onChange([
      ...value,
      {
        uid: `tier-new-${++tierSeed}`,
        tierId: tier.id,
        label: "",
        multiplier: general.vipMultiplier || "1",
        daily: general.vipDaily || "0",
        monthly: general.vipMonthly || "0",
        packDiscount: general.vipPackDiscount || "1",
        packBonusRate: general.vipPackBonusRate || "0",
        rpm: "0",
        maxConcurrent: "0"
      }
    ]);
  };
  const patch = (uid: string, changes: Partial<MemberTierDraft>) =>
    onChange(value.map((item) => (item.uid === uid ? { ...item, ...changes } : item)));

  const rows = [
    ...value.map((item) => ({
      key: item.uid,
      icon: Gem,
      name: item.label || tierName(item.tierId) || `档位 #${item.tierId}`,
      multiplier: item.multiplier,
      daily: item.daily,
      monthly: item.monthly,
      packDiscount: item.packDiscount,
      packBonusRate: item.packBonusRate,
      rpm: Number(item.rpm) || 0,
      maxConcurrent: Number(item.maxConcurrent) || 0
    })),
    {
      key: "vip",
      icon: Crown,
      name: "通用会员",
      multiplier: general.vipMultiplier,
      daily: general.vipDaily,
      monthly: general.vipMonthly,
      packDiscount: general.vipPackDiscount,
      packBonusRate: general.vipPackBonusRate,
      rpm: 0,
      maxConcurrent: 0
    },
    { key: "none", icon: UserRound, name: "非会员", multiplier: "1", daily: "0", monthly: "0", packDiscount: "1", packBonusRate: "0", rpm: 0, maxConcurrent: 0 }
  ];
  const maxMultiplier = Math.max(1, ...rows.map((row) => Number(row.multiplier) || 0));

  return (
    <div className="space-y-4">
      <section className="overflow-hidden rounded-2xl border border-border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
        <header className="flex items-center gap-2 border-b border-border px-4 py-3 sm:px-5">
          <span className="grid size-8 place-items-center rounded-xl bg-amber-500/10 text-amber-600 ring-1 ring-inset ring-amber-500/20 dark:text-amber-400">
            <Users className="size-4" />
          </span>
          <div>
            <h3 className="text-sm font-semibold">优惠对比</h3>
            <p className="text-[11px] text-muted-foreground">用户持有多个档位时取列表里最靠前的；挂了档位但没配置的会员按通用会员算</p>
          </div>
        </header>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[720px] text-xs">
            <thead>
              <tr className="border-b text-[11px] text-muted-foreground">
                <th className="px-4 py-2 text-left font-medium">身份</th>
                <th className="w-[34%] px-3 py-2 text-left font-medium">调用价</th>
                <th className="px-3 py-2 text-right font-medium">每日额度</th>
                <th className="px-3 py-2 text-right font-medium">每月额度</th>
                <th className="px-3 py-2 text-left font-medium">买额度包</th>
                <th className="px-4 py-2 text-left font-medium">限速</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row, index) => {
                const multiplier = Number(row.multiplier) || 0;
                const discount = Number(row.packDiscount) || 1;
                const bonus = Number(row.packBonusRate) || 0;
                return (
                  <tr key={row.key} className={cn("border-b last:border-0", row.key === "none" && "text-muted-foreground")}>
                    <td className="px-4 py-2.5">
                      <span className="flex items-center gap-2">
                        <span className="w-4 font-mono text-[10px] text-muted-foreground">{index < value.length ? index + 1 : ""}</span>
                        <row.icon className={cn("size-3.5", row.key === "none" ? "text-muted-foreground" : "text-amber-500")} />
                        <span className="font-medium">{row.name}</span>
                      </span>
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="flex items-center gap-2">
                        <div className="relative h-2 flex-1 overflow-hidden rounded-full bg-muted">
                          <div
                            className={cn("absolute inset-y-0 left-0 rounded-full", multiplier < 1 ? "bg-amber-500" : multiplier > 1 ? "bg-rose-500" : "bg-muted-foreground/40")}
                            style={{ width: `${Math.min(100, (multiplier / maxMultiplier) * 100)}%` }}
                          />
                          <div className="absolute inset-y-0 w-px bg-foreground/40" style={{ left: `${(1 / maxMultiplier) * 100}%` }} />
                        </div>
                        <span className="w-16 text-right font-mono tabular-nums">{discountLabel(row.multiplier)}</span>
                      </div>
                    </td>
                    <td className="px-3 py-2.5 text-right font-mono tabular-nums">{Number(row.daily) > 0 ? formatCompactPoints(row.daily) : "—"}</td>
                    <td className="px-3 py-2.5 text-right font-mono tabular-nums">{Number(row.monthly) > 0 ? formatCompactPoints(row.monthly) : "—"}</td>
                    <td className="px-3 py-2.5">
                      {discount < 1 || bonus > 0 ? (
                        <span className="flex flex-wrap gap-1">
                          {discount < 1 ? <span className="rounded bg-amber-500/10 px-1.5 text-amber-700 dark:text-amber-300">{discountLabel(discount)}</span> : null}
                          {bonus > 0 ? <span className="rounded bg-emerald-500/10 px-1.5 text-emerald-700 dark:text-emerald-300">多送 {formatRate(bonus)}</span> : null}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">原价</span>
                      )}
                    </td>
                    <td className="px-4 py-2.5 font-mono text-[11px] tabular-nums text-muted-foreground">
                      {row.rpm || row.maxConcurrent
                        ? `${row.rpm || appRpm} 次/分 · ${row.maxConcurrent || appConcurrent} 并发`
                        : `沿用 ${appRpm} · ${appConcurrent}`}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>

      <section className="overflow-hidden rounded-2xl border border-border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
        <header className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3 sm:px-5">
          <span className="grid size-8 place-items-center rounded-xl bg-amber-500/10 text-amber-600 ring-1 ring-inset ring-amber-500/20 dark:text-amber-400">
            <Gem className="size-4" />
          </span>
          <div className="min-w-0">
            <h3 className="text-sm font-semibold">档位优惠</h3>
            <p className="text-[11px] text-muted-foreground">拖动调整优先级；限速填 0 沿用应用设置</p>
          </div>
          <Button size="sm" variant="outline" className="ml-auto" disabled={available.length === 0} onClick={add}>
            <Plus className="size-3.5" />
            {tiers.length === 0 ? "应用还没有会员档位" : available.length === 0 ? "档位都已配置" : "添加档位"}
          </Button>
        </header>
        <div className="space-y-2 p-4 sm:p-5">
          {value.length === 0 ? (
            <div className="rounded-xl border border-dashed px-4 py-6 text-center text-xs text-muted-foreground">
              没有按档位区分：所有会员都按下面的「通用会员」计。
            </div>
          ) : (
            <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
              <SortableContext items={value.map((item) => item.uid)} strategy={verticalListSortingStrategy}>
                {value.map((item, index) => (
                  <TierCard
                    key={item.uid}
                    item={item}
                    index={index}
                    tiers={tiers}
                    used={used}
                    onChange={(changes) => patch(item.uid, changes)}
                    onRemove={() => onChange(value.filter((entry) => entry.uid !== item.uid))}
                  />
                ))}
              </SortableContext>
            </DndContext>
          )}
        </div>
      </section>

      <section className="overflow-hidden rounded-2xl border border-border bg-card" style={{ boxShadow: "var(--shadow-soft)" }}>
        <header className="flex items-center gap-2 border-b border-border px-4 py-3 sm:px-5">
          <span className="grid size-8 place-items-center rounded-xl bg-amber-500/10 text-amber-600 ring-1 ring-inset ring-amber-500/20 dark:text-amber-400">
            <Crown className="size-4" />
          </span>
          <div>
            <h3 className="text-sm font-semibold">通用会员</h3>
            <p className="text-[11px] text-muted-foreground">不挂档位的老会员，或持有的档位都没在上面配置时用这一份</p>
          </div>
        </header>
        <div className="grid gap-3 p-4 sm:grid-cols-2 sm:p-5 lg:grid-cols-5">
          <DecimalBox label="调用倍率" value={general.vipMultiplier} caption={discountLabel(general.vipMultiplier)}
            onChange={(vipMultiplier) => onGeneralChange({ ...general, vipMultiplier })} />
          <DecimalBox label="每日额度" value={general.vipDaily} caption="额度 / 天"
            onChange={(vipDaily) => onGeneralChange({ ...general, vipDaily })} />
          <DecimalBox label="每月额度" value={general.vipMonthly} caption="额度 / 月"
            onChange={(vipMonthly) => onGeneralChange({ ...general, vipMonthly })} />
          <DecimalBox label="额度包折扣" value={general.vipPackDiscount} caption={discountLabel(general.vipPackDiscount)}
            onChange={(vipPackDiscount) => onGeneralChange({ ...general, vipPackDiscount })} />
          <DecimalBox label="额度包多送" value={general.vipPackBonusRate} caption={`多送 ${formatRate(general.vipPackBonusRate)}`}
            onChange={(vipPackBonusRate) => onGeneralChange({ ...general, vipPackBonusRate })} />
        </div>
      </section>
    </div>
  );
}

function TierCard({
  item,
  index,
  tiers,
  used,
  onChange,
  onRemove
}: {
  item: MemberTierDraft;
  index: number;
  tiers: TierOption[];
  used: Set<number>;
  onChange: (changes: Partial<MemberTierDraft>) => void;
  onRemove: () => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: item.uid });
  const known = tiers.some((tier) => tier.id === item.tierId);
  const tierName = tiers.find((tier) => tier.id === item.tierId)?.name;
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn("rounded-xl border bg-background", isDragging && "relative z-10 shadow-lg")}
    >
      <div className="flex flex-wrap items-center gap-2 border-b border-dashed px-2 py-2 sm:px-3">
        <button
          type="button"
          className="grid size-7 shrink-0 cursor-grab place-items-center rounded-md text-muted-foreground hover:bg-muted active:cursor-grabbing"
          aria-label="拖动排序"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-4" />
        </button>
        <span className="grid size-6 shrink-0 place-items-center rounded-full bg-amber-500/15 font-mono text-[10px] font-semibold text-amber-700 dark:text-amber-300">
          {index + 1}
        </span>
        <Select value={String(item.tierId)} onValueChange={(next) => onChange({ tierId: Number(next) })}>
          <SelectTrigger className="h-8 w-44 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {!known ? <SelectItem value={String(item.tierId)}>档位 #{item.tierId}（已不存在）</SelectItem> : null}
            {tiers.map((tier) => (
              <SelectItem key={tier.id} value={String(tier.id)} disabled={tier.id !== item.tierId && used.has(tier.id)}>
                {tier.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          className="h-8 w-40 text-xs"
          maxLength={32}
          placeholder={tierName ? `展示名（默认 ${tierName}）` : "展示名"}
          value={item.label}
          onChange={(event) => onChange({ label: event.target.value })}
        />
        <span className="ml-auto hidden text-[11px] text-muted-foreground sm:inline">
          调用 {discountLabel(item.multiplier)} · 额度包 {discountLabel(item.packDiscount)}
          {Number(item.packBonusRate) > 0 ? ` + 多送 ${formatRate(item.packBonusRate)}` : ""}
        </span>
        <Button size="icon" variant="ghost" className="size-7 text-muted-foreground hover:text-destructive" aria-label="删除" onClick={onRemove}>
          <Trash2 className="size-3.5" />
        </Button>
      </div>
      <div className="grid gap-3 p-3 sm:grid-cols-3 lg:grid-cols-7">
        <DecimalBox label="调用倍率" value={item.multiplier} caption={discountLabel(item.multiplier)} onChange={(multiplier) => onChange({ multiplier })} />
        <DecimalBox label="每日额度" value={item.daily} caption="额度 / 天" onChange={(daily) => onChange({ daily })} />
        <DecimalBox label="每月额度" value={item.monthly} caption="额度 / 月" onChange={(monthly) => onChange({ monthly })} />
        <DecimalBox label="额度包折扣" value={item.packDiscount} caption={discountLabel(item.packDiscount)} onChange={(packDiscount) => onChange({ packDiscount })} />
        <DecimalBox label="额度包多送" value={item.packBonusRate} caption={`多送 ${formatRate(item.packBonusRate)}`} onChange={(packBonusRate) => onChange({ packBonusRate })} />
        <DecimalBox label="每分钟请求" value={item.rpm} caption={Number(item.rpm) > 0 ? "次 / 分" : "沿用应用"} integer onChange={(rpm) => onChange({ rpm })} />
        <DecimalBox label="同时进行" value={item.maxConcurrent} caption={Number(item.maxConcurrent) > 0 ? "个" : "沿用应用"} integer onChange={(maxConcurrent) => onChange({ maxConcurrent })} />
      </div>
    </div>
  );
}

function DecimalBox({
  label,
  value,
  caption,
  integer,
  onChange
}: {
  label: string;
  value: string;
  caption?: string;
  integer?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <div className="min-w-0 space-y-1">
      <Label className="text-[11px] font-medium text-muted-foreground">{label}</Label>
      <Input
        className="h-8 font-mono text-xs tabular-nums"
        inputMode={integer ? "numeric" : "decimal"}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
      {caption ? <div className="truncate text-[10px] font-medium text-foreground/70">{caption}</div> : null}
    </div>
  );
}
