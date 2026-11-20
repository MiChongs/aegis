"use client";

import { useState } from "react";
import { ChevronDown, Plus, Sigma, Trash2 } from "lucide-react";
import { ITEM_ICON, describeItem } from "@/components/apps/item-grant/item-grant-meta";
import { defaultItem } from "@/components/apps/item-grant/item-grant-graph";
import { CosmeticRewardInput } from "@/components/apps/cosmetics/cosmetic-reward";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { ItemGrantCatalog, ItemGrantItem, ItemGrantItemSpec } from "@/lib/api/item-grant";
import { NUMBER_TICKET_LEVELS } from "@/lib/api/user-number";
import { cn } from "@/lib/utils";

/**
 * 检查器的表单原语与物品编辑器。
 *
 * 数字输入框按字符串编辑、失焦或每次合法输入时回写数字：存 number 的输入框清不空，
 * 而画布配置要的是数字（后端按 JSON number 解析）。
 */

export function Field({
  label,
  hint,
  children,
  className
}: {
  label: React.ReactNode;
  hint?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("space-y-1.5", className)}>
      <Label className="text-xs font-medium">{label}</Label>
      {children}
      {hint ? <p className="text-[11px] leading-4 text-muted-foreground">{hint}</p> : null}
    </div>
  );
}

export function FieldGroup({ title, children, aside }: { title: string; children: React.ReactNode; aside?: React.ReactNode }) {
  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <h4 className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{title}</h4>
        {aside}
      </div>
      {children}
    </section>
  );
}

/** 整数输入：空串回写 0（或 fallback），越界不截断、交给后端校验与问题面板提示。 */
export function NumberInput({
  value,
  onChange,
  min,
  max,
  step,
  suffix,
  className,
  placeholder,
  decimal
}: {
  value?: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  suffix?: React.ReactNode;
  className?: string;
  placeholder?: string;
  decimal?: boolean;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const text = draft ?? (value === undefined || value === null ? "" : String(value));
  return (
    <div className={cn("flex items-center gap-1.5", className)}>
      <Input
        type="number"
        inputMode={decimal ? "decimal" : "numeric"}
        className="h-8 min-w-0 flex-1 tabular-nums"
        value={text}
        min={min}
        max={max}
        step={step ?? (decimal ? "any" : 1)}
        placeholder={placeholder}
        onChange={(event) => {
          setDraft(event.target.value);
          const parsed = decimal ? Number.parseFloat(event.target.value) : Number.parseInt(event.target.value, 10);
          if (Number.isFinite(parsed)) onChange(parsed);
          else if (event.target.value === "") onChange(0);
        }}
        onBlur={() => setDraft(null)}
      />
      {suffix ? <span className="shrink-0 text-xs text-muted-foreground">{suffix}</span> : null}
    </div>
  );
}

/** 物品类型选择菜单：按目录分组。 */
export function ItemTypeMenu({
  catalog,
  onSelect,
  label = "添加物品",
  disabled
}: {
  catalog: ItemGrantCatalog;
  onSelect: (type: string) => void;
  label?: string;
  disabled?: boolean;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          disabled={disabled}
          className="h-8 w-full justify-start border-dashed text-xs font-normal text-muted-foreground"
        >
          <Plus className="size-3.5" /> {label}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-[420px] w-(--radix-dropdown-menu-trigger-width) min-w-60 overflow-y-auto">
        {catalog.itemGroups.map((group, index) => {
          const items = catalog.items.filter((item) => item.group === group.key);
          if (items.length === 0) return null;
          return (
            <DropdownMenuGroup key={group.key}>
              {index > 0 ? <DropdownMenuSeparator /> : null}
              <DropdownMenuLabel className="text-[10px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                {group.label}
              </DropdownMenuLabel>
              {items.map((spec) => {
                const Icon = ITEM_ICON[spec.type] ?? Plus;
                return (
                  <DropdownMenuItem key={spec.type} onSelect={() => onSelect(spec.type)} className="items-start gap-2">
                    <Icon className="mt-0.5 size-3.5 text-muted-foreground" />
                    <div className="min-w-0">
                      <div className="text-xs font-medium">{spec.label}</div>
                      <div className="line-clamp-2 text-[11px] leading-4 text-muted-foreground">{spec.hint}</div>
                    </div>
                  </DropdownMenuItem>
                );
              })}
            </DropdownMenuGroup>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** 一组物品的编辑器（发放节点、随机礼包的一个选项共用）。 */
export function ItemListEditor({
  catalog,
  items,
  onChange,
  max
}: {
  catalog: ItemGrantCatalog;
  items: ItemGrantItem[];
  onChange: (items: ItemGrantItem[]) => void;
  max?: number;
}) {
  const limit = max ?? catalog.limits.maxItemsPerNode ?? 12;
  const patch = (index: number, next: Partial<ItemGrantItem>) =>
    onChange(items.map((item, i) => (i === index ? { ...item, ...next } : item)));
  return (
    <div className="space-y-2">
      {items.length === 0 ? (
        <p className="rounded-lg border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">还没有物品</p>
      ) : null}
      {items.map((item, index) => (
        <ItemRow
          key={index}
          item={item}
          spec={catalog.items.find((spec) => spec.type === item.type)}
          catalog={catalog}
          onChange={(next) => patch(index, next)}
          onRemove={() => onChange(items.filter((_, i) => i !== index))}
        />
      ))}
      <ItemTypeMenu
        catalog={catalog}
        disabled={items.length >= limit}
        label={items.length >= limit ? `最多 ${limit} 项` : "添加物品"}
        onSelect={(type) => onChange([...items, defaultItem(type, catalog)])}
      />
    </div>
  );
}

function ItemRow({
  item,
  spec,
  catalog,
  onChange,
  onRemove
}: {
  item: ItemGrantItem;
  spec?: ItemGrantItemSpec;
  catalog: ItemGrantCatalog;
  onChange: (next: Partial<ItemGrantItem>) => void;
  onRemove: () => void;
}) {
  const [exprOpen, setExprOpen] = useState(Boolean(item.amountExpr));
  const Icon = ITEM_ICON[item.type] ?? Plus;
  const range = spec && spec.max ? `${spec.min ?? 0}–${spec.max.toLocaleString("zh-CN")}` : "";

  return (
    <div className="rounded-xl border bg-muted/20 p-2.5">
      <div className="flex items-center gap-2">
        <span className="flex size-6 shrink-0 items-center justify-center rounded-md bg-background text-muted-foreground">
          <Icon className="size-3.5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-xs font-medium">{spec?.label ?? item.type}</div>
          <div className="truncate text-[11px] text-muted-foreground">{describeItem(item, catalog)}</div>
        </div>
        {spec?.scalable ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                size="icon"
                variant={exprOpen ? "secondary" : "ghost"}
                className="size-7"
                onClick={() => {
                  if (exprOpen) onChange({ amountExpr: "" });
                  setExprOpen(!exprOpen);
                }}
                aria-label="按人计算数量"
              >
                <Sigma className="size-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>按人计算数量</TooltipContent>
          </Tooltip>
        ) : null}
        <Button size="icon" variant="ghost" className="size-7" onClick={onRemove} aria-label="移除物品">
          <Trash2 className="size-3.5" />
        </Button>
      </div>

      <div className="mt-2 space-y-2 pl-8">
        {spec?.value === "amount" ? (
          <NumberInput
            value={item.amount}
            min={spec.min}
            max={spec.max}
            onChange={(amount) => onChange({ amount })}
            suffix={`${spec.unit ?? ""}${range ? `（${range}）` : ""}`}
          />
        ) : null}

        {spec?.value === "money" ? (
          <div className="flex items-center gap-1.5">
            <Input
              className="h-8"
              inputMode="decimal"
              value={item.money ?? ""}
              onChange={(event) => onChange({ money: event.target.value })}
            />
            <span className="text-xs text-muted-foreground">元</span>
          </div>
        ) : null}

        {spec?.value === "ref" ? (
          catalog.plans.length === 0 ? (
            <p className="text-xs text-muted-foreground">还没有会员套餐</p>
          ) : (
            <Select value={item.refId ? String(item.refId) : undefined} onValueChange={(value) => onChange({ refId: Number(value) })}>
              <SelectTrigger size="sm" className="h-8 w-full text-xs">
                <SelectValue placeholder="选择套餐" />
              </SelectTrigger>
              <SelectContent position="popper">
                {catalog.plans.map((plan) => (
                  <SelectItem key={plan.id} value={String(plan.id)}>
                    {plan.name}（{plan.lifetime ? "永久" : `${plan.durationDays} 天`}）{plan.active ? "" : "（已下架）"}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )
        ) : null}

        {spec?.value === "feature" || spec?.value === "feature_minutes" ? (
          <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-1.5">
            <Select value={item.tag || undefined} onValueChange={(tag) => onChange({ tag })}>
              <SelectTrigger size="sm" className="h-8 w-full text-xs">
                <SelectValue placeholder="选择功能" />
              </SelectTrigger>
              <SelectContent position="popper">
                {catalog.features.map((feature) => (
                  <SelectItem key={feature.tag} value={feature.tag} disabled={spec.value === "feature" && !feature.isActive}>
                    {feature.name || feature.tag}
                    <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{feature.tag}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <NumberInput
              className="w-32"
              value={item.amount}
              min={spec.min}
              max={spec.max}
              onChange={(amount) => onChange({ amount })}
              suffix={spec.value === "feature" ? "天" : "分钟"}
            />
          </div>
        ) : null}
        {spec?.value === "feature" ? <p className="text-[11px] text-muted-foreground">天数为 0 即永久解锁</p> : null}

        {spec?.value === "cosmetic" ? (
          <CosmeticRewardInput
            refId={item.refId ?? 0}
            amount={item.amount ?? 0}
            max={spec.max}
            onChange={(next) =>
              onChange({
                ...(next.refId !== undefined ? { refId: next.refId } : {}),
                ...(next.amount !== undefined ? { amount: Math.max(0, Math.trunc(Number(next.amount) || 0)) } : {})
              })
            }
          />
        ) : null}

        {spec?.value === "text" && item.type === "number_assign" ? (
          <Input
            className="h-8 font-mono"
            inputMode="numeric"
            value={item.text ?? ""}
            maxLength={12}
            placeholder="例如 88886666"
            onChange={(event) => onChange({ text: event.target.value.replace(/\D/g, "") })}
          />
        ) : null}

        {spec?.value === "text" && item.type !== "number_assign" ? (
          <Input
            className="h-8"
            value={item.text ?? ""}
            maxLength={64}
            placeholder="例如：定制周边一套"
            onChange={(event) => onChange({ text: event.target.value })}
          />
        ) : null}

        {spec?.value === "number_ticket" ? (
          <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-1.5">
            <Select value={String(item.refId || 1)} onValueChange={(value) => onChange({ refId: Number(value) })}>
              <SelectTrigger size="sm" className="h-8 w-full text-xs">
                <SelectValue placeholder="选择等级" />
              </SelectTrigger>
              <SelectContent position="popper">
                {NUMBER_TICKET_LEVELS.map((tier) => (
                  <SelectItem key={tier.level} value={String(tier.level)}>
                    {tier.name}靓号券
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <NumberInput
              className="w-32"
              value={item.amount}
              min={spec.min ?? 1}
              max={spec.max}
              onChange={(amount) => onChange({ amount })}
              suffix={spec.unit ?? "张"}
            />
          </div>
        ) : null}

        {exprOpen && spec?.scalable ? (
          <div className="space-y-1">
            <Input
              className="h-8 font-mono text-xs"
              value={item.amountExpr ?? ""}
              placeholder="例如 50 + user.signStreak * 5"
              onChange={(event) => onChange({ amountExpr: event.target.value })}
            />
            <p className="text-[11px] leading-4 text-muted-foreground">
              结果向下取整并截在上下界内；算不出来时按上面的固定数量发放
            </p>
          </div>
        ) : null}

        {spec?.needsLoginCard ? (
          <p className="text-[11px] text-amber-600 dark:text-amber-400">领取人名下没有授权卡时，整笔发放失败</p>
        ) : null}
        {spec?.pending ? <p className="text-[11px] text-amber-600 dark:text-amber-400">不会自动到账，需要在发放记录里处理</p> : null}
      </div>
    </div>
  );
}

/** 可折叠的小节（随机礼包的每个选项）。 */
export function Collapsible({
  title,
  aside,
  children,
  defaultOpen = true
}: {
  title: React.ReactNode;
  aside?: React.ReactNode;
  children: React.ReactNode;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="rounded-xl border">
      <div className="flex items-center gap-2 px-2.5 py-2">
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left text-xs font-medium"
          onClick={() => setOpen(!open)}
        >
          <ChevronDown className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform", !open && "-rotate-90")} />
          <span className="min-w-0 flex-1 truncate">{title}</span>
        </button>
        {aside}
      </div>
      {open ? <div className="border-t px-2.5 py-2.5">{children}</div> : null}
    </div>
  );
}
