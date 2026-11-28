"use client";

import { useState } from "react";
import { Reorder, useDragControls } from "motion/react";
import { ArrowDown, ArrowUp, ChevronDown, GripVertical, Layers, Plus, Trash2 } from "lucide-react";
import { FieldGroup, NumberField, SectionCard, SwitchRow } from "@/components/apps/app-config-primitives";
import {
  CLOUD_SYNC_COLLECTIONS,
  CLOUD_SYNC_DOMAINS,
  COLLECTION_RE,
  MiB,
  collectionName,
  formatBytes
} from "@/components/apps/cloud-storage/cloud-storage-shared";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { CloudPlan, CloudStorageCaps } from "@/lib/api/cloud-storage";
import type { VipTier } from "@/lib/api/vip";
import { cn } from "@/lib/utils";

/**
 * 云服务档位编辑器（docs/cloud-service.md「档位」）。
 *
 * 列表顺序就是优先级：用户持有的会员档位里排在最前面的那一档生效，所以不另给优先级输入框
 * （与存储池成员同一条约定）。免费档（tierId 0）只能有一个，它覆盖配置本身的基础档。
 * 档位上限与后端 cloudstorage 的常量一致，超出时以后端的校验信息为准。
 */

const GiB = 1024 * MiB;
const SIZE_UNITS = ["MB", "GB"] as const;
type SizeUnit = (typeof SIZE_UNITS)[number];
const UNIT_BYTES: Record<SizeUnit, number> = { MB: MiB, GB: GiB };

/** 与 cloudstorage/plan.go 的常量一致。 */
export const PLAN_LIMITS = {
  maxPlans: 16,
  maxDevices: 50,
  maxSyncRecords: 1_000_000,
  maxHistoryRetentionDays: 3650,
  minPollSeconds: 15,
  maxPollSeconds: 86400,
  nameLength: 32,
  descriptionLength: 120
} as const;

export const FREE_TIER_ID = 0;

/** 数字项按字符串存，理由同配置面板：存 number 的话输入框清不空。 */
export type PlanDraft = {
  uid: string;
  tierId: number;
  name: string;
  description: string;
  quota: string;
  quotaUnit: SizeUnit;
  maxItem: string;
  maxItemUnit: SizeUnit;
  maxItems: string;
  maxRevisions: string;
  trashRetentionDays: string;
  syncEnabled: boolean;
  maxDevices: string;
  maxSyncRecords: string;
  historyRetentionDays: string;
  realtime: boolean;
  pollSeconds: string;
  handoff: boolean;
  /** 关闭时只能同步 collections 里的集合 */
  allCollections: boolean;
  collections: string[];
};

let uidSeed = 0;
const nextUid = () => `plan-${++uidSeed}`;

function splitPlanSize(bytes: number): { value: string; unit: SizeUnit } {
  const unit: SizeUnit = bytes >= GiB && bytes % (GiB / 1024) === 0 ? "GB" : "MB";
  const value = bytes / UNIT_BYTES[unit];
  return { value: String(Number(value.toFixed(3))), unit };
}

function joinPlanSize(value: string, unit: SizeUnit) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) return Number.NaN;
  return Math.round(parsed * UNIT_BYTES[unit]);
}

export function planToDraft(plan: CloudPlan, uid: string): PlanDraft {
  const quota = splitPlanSize(plan.quotaBytes);
  const maxItem = splitPlanSize(plan.maxItemBytes);
  const collections = plan.collections ?? [];
  return {
    uid,
    tierId: plan.tierId,
    name: plan.name ?? "",
    description: plan.description ?? "",
    quota: quota.value,
    quotaUnit: quota.unit,
    maxItem: maxItem.value,
    maxItemUnit: maxItem.unit,
    maxItems: String(plan.maxItems),
    maxRevisions: String(plan.maxRevisions),
    trashRetentionDays: String(plan.trashRetentionDays),
    syncEnabled: plan.syncEnabled,
    maxDevices: String(plan.maxDevices),
    maxSyncRecords: String(plan.maxSyncRecords),
    historyRetentionDays: String(plan.historyRetentionDays),
    realtime: plan.realtime,
    pollSeconds: String(plan.pollSeconds || 300),
    handoff: plan.handoff,
    allCollections: collections.length === 0,
    collections
  };
}

/** 从服务端数据播种：用下标作 uid，随机 uid 会让输入框在每次重渲染时被卸载重建。 */
export function seedPlanDrafts(plans?: CloudPlan[] | null): PlanDraft[] {
  return (plans ?? []).map((plan, index) => planToDraft(plan, `plan-seed-${index}`));
}

/** 草稿 → 提交的档位；第一个问题以错误文案返回。 */
export function planDraftsToPayload(
  drafts: PlanDraft[],
  label: (tierId: number) => string
): { plans: CloudPlan[] } | { error: string } {
  if (drafts.filter((draft) => draft.tierId === FREE_TIER_ID).length > 1) return { error: "免费档只能有一个" };
  const plans: CloudPlan[] = [];
  for (const draft of drafts) {
    const name = draft.name.trim() || label(draft.tierId);
    const quotaBytes = joinPlanSize(draft.quota, draft.quotaUnit);
    const maxItemBytes = joinPlanSize(draft.maxItem, draft.maxItemUnit);
    if (Number.isNaN(quotaBytes) || Number.isNaN(maxItemBytes)) return { error: `「${name}」的配额与单条目上限需为正数` };
    if (maxItemBytes > quotaBytes) return { error: `「${name}」的单条目上限不能超过配额` };
    if (!draft.allCollections && draft.collections.length === 0) return { error: `「${name}」至少选择一个集合` };
    plans.push({
      tierId: draft.tierId,
      name: draft.name.trim(),
      description: draft.description.trim() || undefined,
      quotaBytes,
      maxItemBytes,
      maxItems: Number(draft.maxItems) || 0,
      maxRevisions: Number(draft.maxRevisions) || 0,
      trashRetentionDays: Number(draft.trashRetentionDays) || 0,
      syncEnabled: draft.syncEnabled,
      maxDevices: Number(draft.maxDevices) || 0,
      maxSyncRecords: Number(draft.maxSyncRecords) || 0,
      historyRetentionDays: Number(draft.historyRetentionDays) || 0,
      realtime: draft.realtime,
      pollSeconds: Number(draft.pollSeconds) || 0,
      handoff: draft.handoff,
      collections: draft.allCollections ? [] : draft.collections
    });
  }
  return { plans };
}

/** 新档位的初值：空间沿用当前基础档，同步取后端默认（3 台设备、2 万条、实时、接力）。 */
export type PlanBase = Pick<CloudPlan, "quotaBytes" | "maxItemBytes" | "maxItems" | "maxRevisions" | "trashRetentionDays">;

function newPlanDraft(tierId: number, base: PlanBase): PlanDraft {
  return planToDraft(
    {
      tierId,
      name: "",
      quotaBytes: base.quotaBytes > 0 ? base.quotaBytes : 20 * MiB,
      maxItemBytes: base.maxItemBytes > 0 ? base.maxItemBytes : MiB,
      maxItems: base.maxItems || 500,
      maxRevisions: base.maxRevisions || 10,
      trashRetentionDays: base.trashRetentionDays,
      syncEnabled: true,
      maxDevices: 3,
      maxSyncRecords: 20000,
      historyRetentionDays: 0,
      realtime: true,
      pollSeconds: 300,
      handoff: true,
      collections: []
    },
    nextUid()
  );
}

export function CloudPlansSection({
  plans,
  onChange,
  tiers,
  tiersError,
  caps,
  base
}: {
  plans: PlanDraft[];
  onChange: (plans: PlanDraft[]) => void;
  tiers: VipTier[];
  /** 会员档位读不到时仍能编辑免费档 */
  tiersError?: boolean;
  caps?: CloudStorageCaps;
  base: PlanBase;
}) {
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const tierName = (tierId: number) =>
    tierId === FREE_TIER_ID ? "免费" : (tiers.find((tier) => tier.id === tierId)?.name ?? `档位 #${tierId}`);
  const used = new Set(plans.map((plan) => plan.tierId));
  const freeAvailable = !used.has(FREE_TIER_ID);
  const availableTiers = tiers.filter((tier) => !used.has(tier.id));
  const full = plans.length >= PLAN_LIMITS.maxPlans;
  const tiersLoaded = !tiersError && tiers.length > 0;
  // 每一行的档位下拉：自己当前的档位 + 还没被别的行占用的档位；已删除的会员档位保留原值。
  const tierOptionsFor = (current: number) => [
    ...(current === FREE_TIER_ID || freeAvailable ? [{ id: FREE_TIER_ID, name: "免费档" }] : []),
    ...tiers.filter((tier) => tier.id === current || !used.has(tier.id)).map((tier) => ({ id: tier.id, name: tier.name })),
    ...(current !== FREE_TIER_ID && !tiers.some((tier) => tier.id === current)
      ? [{ id: current, name: `档位 #${current}` }]
      : [])
  ];

  const patchPlan = (uid: string, changes: Partial<PlanDraft>) =>
    onChange(plans.map((plan) => (plan.uid === uid ? { ...plan, ...changes } : plan)));
  const move = (index: number, delta: number) => {
    const target = index + delta;
    if (target < 0 || target >= plans.length) return;
    const next = [...plans];
    const [item] = next.splice(index, 1);
    next.splice(target, 0, item);
    onChange(next);
  };
  const add = (tierId: number) => {
    const draft = newPlanDraft(tierId, base);
    // 免费档放在最后：它只在持有的档位都没配时生效，排在哪里都一样，放最后读起来顺。
    const paid = plans.filter((item) => item.tierId !== FREE_TIER_ID);
    const freePlans = plans.filter((item) => item.tierId === FREE_TIER_ID);
    onChange(tierId === FREE_TIER_ID ? [...plans, draft] : [...paid, draft, ...freePlans]);
    setExpanded((current) => new Set(current).add(draft.uid));
  };
  const toggle = (uid: string) =>
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(uid)) next.delete(uid);
      else next.add(uid);
      return next;
    });

  return (
    <SectionCard
      icon={<Layers className="size-4" />}
      title="档位"
      aside={
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="sm" variant="outline" disabled={full || (!freeAvailable && availableTiers.length === 0)}>
              <Plus className="size-3.5" />
              添加档位
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-52">
            <DropdownMenuItem disabled={!freeAvailable} onClick={() => add(FREE_TIER_ID)}>
              免费档
              {!freeAvailable ? <span className="ml-auto text-[11px] text-muted-foreground">已添加</span> : null}
            </DropdownMenuItem>
            {tiers.length ? <DropdownMenuSeparator /> : null}
            {tiers.map((tier) => (
              <DropdownMenuItem key={tier.id} disabled={used.has(tier.id)} onClick={() => add(tier.id)}>
                <span className="truncate">{tier.name}</span>
                {used.has(tier.id) ? <span className="ml-auto text-[11px] text-muted-foreground">已添加</span> : null}
              </DropdownMenuItem>
            ))}
            {tiersError ? (
              <DropdownMenuItem disabled>
                <span className="text-[11px] text-destructive">会员档位读取失败</span>
              </DropdownMenuItem>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      }
    >
      {plans.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border px-4 py-8 text-center text-xs text-muted-foreground">
          暂无档位，所有人使用基础档
        </p>
      ) : (
        <Reorder.Group axis="y" values={plans} onReorder={onChange} className="space-y-2">
          {plans.map((plan, index) => (
            <PlanRow
              key={plan.uid}
              plan={plan}
              index={index}
              count={plans.length}
              open={expanded.has(plan.uid)}
              tierLabel={tierName(plan.tierId)}
              tierMissing={tiersLoaded && plan.tierId !== FREE_TIER_ID && !tiers.some((tier) => tier.id === plan.tierId)}
              tierOptions={tierOptionsFor(plan.tierId)}
              caps={caps}
              onToggle={() => toggle(plan.uid)}
              onPatch={(changes) => patchPlan(plan.uid, changes)}
              onMove={(delta) => move(index, delta)}
              onRemove={() => onChange(plans.filter((item) => item.uid !== plan.uid))}
            />
          ))}
        </Reorder.Group>
      )}
    </SectionCard>
  );
}

function PlanRow({
  plan,
  index,
  count,
  open,
  tierLabel,
  tierMissing,
  tierOptions,
  caps,
  onToggle,
  onPatch,
  onMove,
  onRemove
}: {
  plan: PlanDraft;
  index: number;
  count: number;
  open: boolean;
  tierLabel: string;
  tierMissing: boolean;
  tierOptions: Array<{ id: number; name: string }>;
  caps?: CloudStorageCaps;
  onToggle: () => void;
  onPatch: (changes: Partial<PlanDraft>) => void;
  onMove: (delta: number) => void;
  onRemove: () => void;
}) {
  const controls = useDragControls();
  const quotaBytes = joinPlanSize(plan.quota, plan.quotaUnit);
  const maxItemBytes = joinPlanSize(plan.maxItem, plan.maxItemUnit);
  const sizeInvalid = Number.isNaN(quotaBytes) || Number.isNaN(maxItemBytes) || maxItemBytes > quotaBytes;
  const free = plan.tierId === FREE_TIER_ID;

  return (
    <Reorder.Item
      value={plan}
      dragListener={false}
      dragControls={controls}
      className={cn("rounded-xl border bg-card", sizeInvalid && "border-destructive/60")}
    >
      <div className="flex flex-wrap items-center gap-2 px-2 py-2">
        <button
          type="button"
          className="cursor-grab touch-none rounded-md p-1 text-muted-foreground hover:bg-muted active:cursor-grabbing"
          onPointerDown={(event) => controls.start(event)}
          aria-label="拖动排序"
        >
          <GripVertical className="size-4" />
        </button>
        <span className="w-5 text-center font-mono text-xs tabular-nums text-muted-foreground">{index + 1}</span>
        <Select value={String(plan.tierId)} onValueChange={(value) => onPatch({ tierId: Number(value) })}>
          <SelectTrigger size="sm" className="w-32 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {tierOptions.map((option) => (
              <SelectItem key={option.id} value={String(option.id)}>
                {option.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          className="h-8 w-36 text-xs"
          value={plan.name}
          maxLength={PLAN_LIMITS.nameLength}
          placeholder={tierLabel}
          onChange={(event) => onPatch({ name: event.target.value })}
        />
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1">
          {free ? (
            <Badge variant="outline" size="sm" className="font-normal">
              免费档
            </Badge>
          ) : null}
          {tierMissing ? (
            <Badge variant="danger" size="sm" className="font-normal">
              会员档位不存在
            </Badge>
          ) : null}
          <Badge variant="secondary" size="sm" className="font-normal tabular-nums">
            {Number.isNaN(quotaBytes) ? "配额无效" : formatBytes(quotaBytes)}
          </Badge>
          <Badge variant="secondary" size="sm" className="font-normal tabular-nums">
            {plan.syncEnabled ? `${plan.maxDevices || "—"} 台设备` : "无同步"}
          </Badge>
          {plan.syncEnabled ? (
            <Badge variant="secondary" size="sm" className="font-normal">
              {plan.realtime ? "实时" : `${plan.pollSeconds || "—"} 秒轮询`}
            </Badge>
          ) : null}
        </div>
        <div className="flex items-center">
          <Button size="icon-sm" variant="ghost" aria-label="上移" disabled={index === 0} onClick={() => onMove(-1)}>
            <ArrowUp className="size-3.5" />
          </Button>
          <Button size="icon-sm" variant="ghost" aria-label="下移" disabled={index === count - 1} onClick={() => onMove(1)}>
            <ArrowDown className="size-3.5" />
          </Button>
          <Button size="icon-sm" variant="ghost" aria-label={open ? "收起" : "展开"} aria-expanded={open} onClick={onToggle}>
            <ChevronDown className={cn("size-4 transition-transform", open && "rotate-180")} />
          </Button>
          <Button
            size="icon-sm"
            variant="ghost"
            className="text-muted-foreground hover:text-destructive"
            aria-label="删除档位"
            onClick={onRemove}
          >
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>

      {open ? (
        <div className="space-y-5 border-t px-3 py-4 sm:px-4">
          <div className="space-y-1.5">
            <Label className="text-[11px] font-medium">说明</Label>
            <Input
              className="h-8 text-xs"
              value={plan.description}
              maxLength={PLAN_LIMITS.descriptionLength}
              placeholder="5 GB · 6 台设备 · 实时同步"
              onChange={(event) => onPatch({ description: event.target.value })}
            />
          </div>

          <FieldGroup label="空间">
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <SizeField
                label="配额"
                value={plan.quota}
                unit={plan.quotaUnit}
                invalid={Number.isNaN(quotaBytes)}
                hint={caps ? `最大 ${formatBytes(caps.maxQuotaBytes)}` : undefined}
                onValue={(value) => onPatch({ quota: value })}
                onUnit={(unit) => onPatch({ quotaUnit: unit })}
              />
              <SizeField
                label="单条目上限"
                value={plan.maxItem}
                unit={plan.maxItemUnit}
                invalid={Number.isNaN(maxItemBytes) || maxItemBytes > quotaBytes}
                hint={caps ? `最大 ${formatBytes(caps.maxItemBytes)}` : undefined}
                onValue={(value) => onPatch({ maxItem: value })}
                onUnit={(unit) => onPatch({ maxItemUnit: unit })}
              />
              <NumberField
                label="条目数上限"
                unit="个"
                min={1}
                max={caps?.maxItems}
                value={plan.maxItems}
                onChange={(value) => onPatch({ maxItems: value })}
              />
              <NumberField
                label="保留修订"
                unit="个"
                min={1}
                max={caps?.maxRevisions}
                value={plan.maxRevisions}
                onChange={(value) => onPatch({ maxRevisions: value })}
              />
              <NumberField
                label="回收站保留"
                unit="天"
                min={0}
                max={caps?.maxTrashRetentionDays}
                hint="0 表示删除即清除"
                value={plan.trashRetentionDays}
                onChange={(value) => onPatch({ trashRetentionDays: value })}
              />
            </div>
          </FieldGroup>

          <FieldGroup label="同步">
            <div className="space-y-3">
              <div className="grid gap-2 sm:grid-cols-3">
                <SwitchRow label="多设备同步" checked={plan.syncEnabled} onChange={(value) => onPatch({ syncEnabled: value })} />
                <SwitchRow
                  label="实时推送"
                  checked={plan.realtime}
                  disabled={!plan.syncEnabled}
                  onChange={(value) => onPatch({ realtime: value })}
                />
                <SwitchRow
                  label="接力"
                  checked={plan.handoff}
                  disabled={!plan.syncEnabled}
                  onChange={(value) => onPatch({ handoff: value })}
                />
              </div>
              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                <NumberField
                  label="设备数"
                  unit="台"
                  min={1}
                  max={PLAN_LIMITS.maxDevices}
                  disabled={!plan.syncEnabled}
                  value={plan.maxDevices}
                  onChange={(value) => onPatch({ maxDevices: value })}
                />
                <NumberField
                  label="同步记录上限"
                  unit="条"
                  min={1}
                  max={PLAN_LIMITS.maxSyncRecords}
                  hint="不含墓碑"
                  disabled={!plan.syncEnabled}
                  value={plan.maxSyncRecords}
                  onChange={(value) => onPatch({ maxSyncRecords: value })}
                />
                <NumberField
                  label="历史保留"
                  unit="天"
                  min={0}
                  max={PLAN_LIMITS.maxHistoryRetentionDays}
                  hint="0 表示永久"
                  disabled={!plan.syncEnabled}
                  value={plan.historyRetentionDays}
                  onChange={(value) => onPatch({ historyRetentionDays: value })}
                />
                <NumberField
                  label="轮询间隔"
                  unit="秒"
                  min={PLAN_LIMITS.minPollSeconds}
                  max={PLAN_LIMITS.maxPollSeconds}
                  disabled={!plan.syncEnabled}
                  value={plan.pollSeconds}
                  onChange={(value) => onPatch({ pollSeconds: value })}
                />
              </div>
            </div>
          </FieldGroup>

          <FieldGroup
            label="可同步的内容"
            hint={plan.allCollections ? "全部" : `已选 ${plan.collections.length} 项`}
          >
            <CollectionPicker plan={plan} disabled={!plan.syncEnabled} onPatch={onPatch} />
          </FieldGroup>
        </div>
      ) : null}
    </Reorder.Item>
  );
}

function SizeField({
  label,
  value,
  unit,
  invalid,
  hint,
  onValue,
  onUnit
}: {
  label: string;
  value: string;
  unit: SizeUnit;
  invalid?: boolean;
  hint?: string;
  onValue: (value: string) => void;
  onUnit: (unit: SizeUnit) => void;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-[11px] font-medium">{label}</Label>
      <div className="flex">
        <Input
          type="number"
          min={0}
          step="any"
          inputMode="decimal"
          value={value}
          aria-invalid={invalid || undefined}
          className="rounded-r-none font-mono text-xs tabular-nums"
          onChange={(event) => onValue(event.target.value)}
        />
        <Select value={unit} onValueChange={(next) => onUnit(next as SizeUnit)}>
          <SelectTrigger className="w-[72px] rounded-l-none border-l-0 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SIZE_UNITS.map((item) => (
              <SelectItem key={item} value={item}>
                {item}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      {hint ? <p className="text-[10px] leading-snug text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

/** 集合多选：按领域分组；目录外的集合（名字合法即可同步）单列一组，可自行添加。 */
function CollectionPicker({
  plan,
  disabled,
  onPatch
}: {
  plan: PlanDraft;
  disabled: boolean;
  onPatch: (changes: Partial<PlanDraft>) => void;
}) {
  const [custom, setCustom] = useState("");
  const selected = new Set(plan.collections);
  const extras = plan.collections.filter((key) => !CLOUD_SYNC_COLLECTIONS.some((item) => item.key === key));
  const customKey = custom.trim().toLowerCase();
  const customValid = COLLECTION_RE.test(customKey) && !selected.has(customKey);
  const setCollection = (key: string, on: boolean) =>
    onPatch({ collections: on ? [...plan.collections, key] : plan.collections.filter((item) => item !== key) });

  return (
    <div className={cn("space-y-3", disabled && "pointer-events-none opacity-50")}>
      <SwitchRow
        label="全部内容"
        checked={plan.allCollections}
        onChange={(value) =>
          onPatch(
            value
              ? { allCollections: true, collections: [] }
              : { allCollections: false, collections: CLOUD_SYNC_COLLECTIONS.map((item) => item.key) }
          )
        }
      />
      {!plan.allCollections ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {CLOUD_SYNC_DOMAINS.map((domain) => {
            const items = CLOUD_SYNC_COLLECTIONS.filter((item) => item.domain === domain.key);
            const picked = items.filter((item) => selected.has(item.key)).length;
            return (
              <div key={domain.key} className="space-y-1.5 rounded-xl bg-muted px-3 py-2.5">
                <label className="flex cursor-pointer items-center gap-2 text-xs font-medium">
                  <Checkbox
                    checked={picked === items.length ? true : picked > 0 ? "indeterminate" : false}
                    onCheckedChange={(checked) => {
                      const keys = items.map((item) => item.key);
                      const rest = plan.collections.filter((key) => !keys.includes(key));
                      onPatch({ collections: checked === true ? [...rest, ...keys] : rest });
                    }}
                  />
                  {domain.name}
                </label>
                <div className="space-y-1 pl-6">
                  {items.map((item) => (
                    <label key={item.key} className="flex cursor-pointer items-center gap-2 text-[11px]" title={item.key}>
                      <Checkbox
                        checked={selected.has(item.key)}
                        onCheckedChange={(checked) => setCollection(item.key, checked === true)}
                      />
                      <span className="truncate">{item.name}</span>
                    </label>
                  ))}
                </div>
              </div>
            );
          })}
          <div className="space-y-1.5 rounded-xl bg-muted px-3 py-2.5">
            <div className="text-xs font-medium">其他</div>
            <div className="space-y-1">
              {extras.map((key) => (
                <label key={key} className="flex cursor-pointer items-center gap-2 text-[11px]">
                  <Checkbox checked onCheckedChange={() => setCollection(key, false)} />
                  <code className="truncate font-mono">{collectionName(key)}</code>
                </label>
              ))}
            </div>
            <div className="flex gap-1">
              <Input
                className="h-7 font-mono text-[11px]"
                value={custom}
                placeholder="domain.name"
                maxLength={64}
                aria-invalid={customKey !== "" && !COLLECTION_RE.test(customKey) ? true : undefined}
                onChange={(event) => setCustom(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && customValid) {
                    event.preventDefault();
                    setCollection(customKey, true);
                    setCustom("");
                  }
                }}
              />
              <Button
                size="icon-xs"
                variant="outline"
                className="size-7"
                aria-label="添加集合"
                disabled={!customValid}
                onClick={() => {
                  setCollection(customKey, true);
                  setCustom("");
                }}
              >
                <Plus />
              </Button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
