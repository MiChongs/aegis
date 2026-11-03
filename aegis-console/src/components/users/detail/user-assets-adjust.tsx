"use client";

/**
 * 资产页的调整部件：积分、经验、余额共用一张调整表单，以及页内的分段选择与快捷标签。
 *
 * 三条账的结算语义不同（积分 / 经验是整数，余额是 decimal 字符串），
 * 所以表单只统一**交互**——方向、数量、预览、原因、确认、提交——不统一请求：
 * 每一种的 `onSubmit` 仍各走各的接口。
 *
 * 方向（增加 / 扣减）单独选，而不是让人在数量前面打负号：
 * 负号在输入框里很容易漏看，扣成了加、加成了扣都要再做一笔冲正。
 * 提交前再弹一次确认，把「当前 → 调整后」、原因与请求编号摆在一起看一眼。
 *
 * 数值一律按字符串放大成 BigInt 比较，不经过 Number：超过 2^53 的整数转成浮点数会被舍入，
 * 「调整后」预览与上限判断都会算错。表单在本地先拦下单次超限与调整后越界，
 * 后端仍会按同样的规则再校验一遍。
 *
 * 每次提交附带一个请求编号：同一笔（方向、数量、原因都没变）重试时复用，
 * 后端据此只生效一次 —— 网络超时后再点一次不会扣两遍。成功或改动内容后换新编号。
 */

import * as React from "react";
import { ArrowDown, ArrowRight, Loader2, Minus, Plus, ShieldCheck } from "lucide-react";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";
import { TONE_TEXT } from "./kit";

// ── 十进制运算 ──────────────────────────
//
// 余额是 shopspring/decimal 字符串，转 Number 会丢精度。预览「调整后余额」时
// 按 4 位小数放大成 BigInt 做整数运算，结果再交给 formatMoney 展示。

const SCALE = 4;
const ZERO = BigInt(0);

function toScaled(value?: string | null): bigint | null {
  const match = (value ?? "").trim().match(/^([+-])?(\d+)(?:\.(\d+))?$/);
  if (!match) return null;
  const fraction = (match[3] ?? "").padEnd(SCALE, "0").slice(0, SCALE);
  const scaled = BigInt(match[2] + fraction);
  return match[1] === "-" ? -scaled : scaled;
}

function fromScaled(value: bigint) {
  const negative = value < ZERO;
  const digits = (negative ? -value : value).toString().padStart(SCALE + 1, "0");
  return `${negative ? "-" : ""}${digits.slice(0, -SCALE)}.${digits.slice(-SCALE)}`;
}

/** 比较两个十进制字符串；任一无法解析时返回 null。 */
export function compareDecimal(a?: string | null, b?: string | null) {
  const left = toScaled(a);
  const right = toScaled(b);
  if (left === null || right === null) return null;
  return left === right ? 0 : left > right ? 1 : -1;
}

/** 千分位显示整数字符串（BigInt，不丢精度）。 */
export function formatIntegerString(value: string) {
  try {
    return BigInt(value).toLocaleString("zh-CN");
  } catch {
    return value;
  }
}

/** 生成请求编号；没有 randomUUID 的环境（非安全上下文）退回随机数拼接。 */
export function newRequestId() {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") return crypto.randomUUID();
  return `req-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}

/** 两个十进制字符串相加；任一无法解析时返回 null。 */
export function addDecimal(a?: string | null, b?: string | null) {
  const left = toScaled(a);
  const right = toScaled(b);
  if (left === null || right === null) return null;
  return fromScaled(left + right);
}

export function isNegativeDecimal(value?: string | null) {
  const scaled = toScaled(value);
  return scaled !== null && scaled < ZERO;
}

export function isPositiveDecimal(value?: string | null) {
  const scaled = toScaled(value);
  return scaled !== null && scaled > ZERO;
}

// ── 分段选择 ──────────────────────────

export type SegmentOption<T extends string> = {
  value: T;
  label: React.ReactNode;
  icon?: React.ComponentType<{ className?: string }>;
  count?: number;
  disabled?: boolean;
};

/** 小号分段选择，用在区块工具栏与表单里。 */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  stretch = false,
  className,
  ariaLabel
}: {
  value: T;
  onChange: (value: T) => void;
  options: Array<SegmentOption<T>>;
  /** 铺满父容器、各项等宽 */
  stretch?: boolean;
  className?: string;
  ariaLabel?: string;
}) {
  return (
    <ToggleGroup
      type="single"
      value={value}
      aria-label={ariaLabel}
      onValueChange={(next) => {
        if (next) onChange(next as T);
      }}
      className={cn(
        "max-w-full flex-wrap rounded-lg border bg-muted/40 p-0.5",
        stretch && "grid w-full",
        className
      )}
      style={stretch ? { gridTemplateColumns: `repeat(${options.length}, minmax(0, 1fr))` } : undefined}
    >
      {options.map((option) => {
        const Icon = option.icon;
        return (
          <ToggleGroupItem
            key={option.value}
            value={option.value}
            disabled={option.disabled}
            className="h-7 gap-1 rounded-md px-2.5 text-xs data-[state=on]:bg-background data-[state=on]:font-medium data-[state=on]:shadow-sm"
          >
            {Icon ? <Icon className="size-3.5" /> : null}
            {option.label}
            {typeof option.count === "number" ? (
              <span className="tabular-nums text-muted-foreground">{option.count}</span>
            ) : null}
          </ToggleGroupItem>
        );
      })}
    </ToggleGroup>
  );
}

/** 一排可点的快捷值（数量、天数、原因）。再点一次已选中的项不会取消，只有输入框能清空。 */
export function QuickChips({
  items,
  value,
  onPick,
  render,
  className
}: {
  items: string[];
  value?: string;
  onPick: (value: string) => void;
  render?: (value: string) => React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-wrap gap-1.5", className)}>
      {items.map((item) => {
        const active = value === item;
        return (
          <button
            key={item}
            type="button"
            aria-pressed={active}
            onClick={() => onPick(item)}
            className={cn(
              "inline-flex h-7 items-center rounded-md border px-2.5 text-xs tabular-nums transition-colors",
              active
                ? "border-primary/40 bg-primary/10 font-medium text-foreground"
                : "bg-background text-muted-foreground hover:bg-accent hover:text-foreground"
            )}
          >
            {render ? render(item) : item}
          </button>
        );
      })}
    </div>
  );
}

/** 带建议项的原因输入框：建议项走原生 datalist；`chips` 为 true 时另在下方列出可点的常用原因。 */
export function ReasonInput({
  value,
  onChange,
  suggestions,
  placeholder = "原因（选填）",
  className,
  onEnter,
  chips = false
}: {
  value: string;
  onChange: (value: string) => void;
  suggestions: string[];
  placeholder?: string;
  className?: string;
  onEnter?: () => void;
  chips?: boolean;
}) {
  const listId = React.useId();
  return (
    <div className="flex flex-col gap-2">
      <Input
        value={value}
        maxLength={200}
        list={listId}
        placeholder={placeholder}
        className={className}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && onEnter) {
            event.preventDefault();
            onEnter();
          }
        }}
      />
      <datalist id={listId}>
        {suggestions.map((item) => (
          <option key={item} value={item} />
        ))}
      </datalist>
      {chips && suggestions.length ? <QuickChips items={suggestions} value={value.trim()} onPick={onChange} /> : null}
    </div>
  );
}

// ── 调整表单 ──────────────────────────

type Direction = "add" | "deduct";

const DIRECTIONS: Array<SegmentOption<Direction>> = [
  { value: "add", label: "增加", icon: Plus },
  { value: "deduct", label: "扣减", icon: Minus }
];

const INTEGER_RE = /^\d+$/;
const DECIMAL_RE = /^\d+(\.\d{1,2})?$/;

export type AdjustFormProps = {
  title: string;
  /** 按钮与输入框里的单位：「积分」「经验」「元」 */
  unit: string;
  /** 余额允许两位小数，积分与经验只收整数 */
  decimal?: boolean;
  /** 当前值的十进制字符串；拿不到时不出预览，也不拦扣减 */
  current?: string;
  format: (value: string) => string;
  hint?: React.ReactNode;
  loading?: boolean;
  /** 数据不可用时的说明；给出后整张表单只读 */
  unavailable?: React.ReactNode;
  quick: string[];
  reasons: string[];
  /** 扣减超过当前值时的提示（后端同样拒绝），如「积分不足」 */
  shortageText: string;
  /** 只允许增加，不显示方向选择 */
  addOnly?: boolean;
  /**
   * 扣减时附加的内容（经验的等级处理）：`form` 放在数量下方，`confirm` 作为确认框里的一行。
   * 参数是当前输入的有效扣减数量（十进制字符串），无效时为 null。
   */
  deductExtra?: (amount: string | null) => { form?: React.ReactNode; confirm?: React.ReactNode };
  /** 单次调整数量上限（十进制字符串） */
  maxAmount?: string;
  /** 调整后余额上限（十进制字符串） */
  maxBalance?: string;
  pending: boolean;
  /** 提交前是否弹确认框，默认弹 */
  confirm?: boolean;
  /** 返回 true 表示成功，表单随即清空数量与原因；requestId 同一笔重试时不变 */
  onSubmit: (signedAmount: string, reason: string, requestId: string) => Promise<boolean>;
};

/**
 * 调整表单本体（不带外框）：左侧「当前 → 调整后」预览，右侧方向、数量、快捷数额、原因与提交。
 * 资产页把它放进一个 Section，用分段切换积分 / 经验 / 余额；切换时以 key 重建，状态不串。
 */
export function AdjustForm({
  title,
  unit,
  decimal = false,
  current,
  format,
  hint,
  loading = false,
  unavailable,
  quick,
  reasons,
  shortageText,
  addOnly = false,
  deductExtra,
  maxAmount,
  maxBalance,
  pending,
  confirm = true,
  onSubmit
}: AdjustFormProps) {
  const [pickedDirection, setDirection] = React.useState<Direction>("add");
  const [amount, setAmount] = React.useState("");
  const [reason, setReason] = React.useState("");
  const [confirming, setConfirming] = React.useState<{ signed: string; reason: string; requestId: string } | null>(null);
  const direction: Direction = addOnly ? "add" : pickedDirection;
  const amountId = React.useId();

  const trimmed = amount.trim();
  const amountValid = (decimal ? DECIMAL_RE : INTEGER_RE).test(trimmed) && isPositiveDecimal(trimmed);
  const signed = direction === "deduct" ? `-${trimmed}` : trimmed;
  const after = amountValid && current !== undefined ? addDecimal(current, signed) : null;
  const shortage = after !== null && isNegativeDecimal(after);
  const overAmount = amountValid && maxAmount !== undefined && (compareDecimal(trimmed, maxAmount) ?? 0) > 0;
  const overBalance = !shortage && after !== null && maxBalance !== undefined && (compareDecimal(after, maxBalance) ?? 0) > 0;
  const blocked = Boolean(unavailable) || loading;
  const disabled = blocked || pending || !amountValid || shortage || overAmount || overBalance;

  const verb = direction === "add" ? "增加" : "扣减";
  const extra = direction === "deduct" && deductExtra ? deductExtra(amountValid ? trimmed : null) : null;
  const amountLabel = decimal ? `¥${trimmed}` : `${formatIntegerString(trimmed)} ${unit}`;

  // 请求编号跟着「这一笔」走：内容不变就复用，改了任何一项就换新的
  const requestRef = React.useRef<{ key: string; id: string } | null>(null);
  function requestIdFor(key: string) {
    if (requestRef.current?.key !== key) requestRef.current = { key, id: newRequestId() };
    return requestRef.current.id;
  }

  async function run(input: { signed: string; reason: string; requestId: string }) {
    const ok = await onSubmit(input.signed, input.reason, input.requestId);
    if (ok) {
      requestRef.current = null;
      setAmount("");
      setReason("");
      setConfirming(null);
    }
  }

  function submit() {
    if (disabled) return;
    const finalReason = reason.trim();
    const input = { signed, reason: finalReason, requestId: requestIdFor(`${signed}|${finalReason}`) };
    if (confirm) setConfirming(input);
    else void run(input);
  }

  const message = overAmount ? (
    `单次最多调整 ${formatIntegerString(maxAmount ?? "")} ${unit}`
  ) : shortage ? (
    `${shortageText}，最多扣减 ${current !== undefined ? format(current) : ""}`
  ) : overBalance ? (
    `调整后将超过上限 ${formatIntegerString(maxBalance ?? "")}`
  ) : null;

  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,16rem)_minmax(0,1fr)]">
      {/* 预览 */}
      <div className="flex flex-col gap-3 rounded-xl border bg-muted/30 p-4">
        <div className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">当前{title}</span>
          {loading ? (
            <Skeleton className="h-8 w-32" />
          ) : (
            <span className="truncate text-2xl font-semibold tracking-tight tabular-nums">
              {current !== undefined ? format(current) : "—"}
            </span>
          )}
        </div>
        <ArrowDown className="size-4 text-muted-foreground" aria-hidden />
        <div className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">调整后</span>
          <span
            className={cn(
              "truncate text-lg font-semibold tabular-nums",
              message
                ? TONE_TEXT.danger
                : after === null
                  ? "text-muted-foreground"
                  : direction === "add"
                    ? TONE_TEXT.success
                    : TONE_TEXT.warning
            )}
          >
            {after !== null && !shortage ? format(after) : "—"}
          </span>
        </div>
        <div className="mt-auto min-h-4 text-xs leading-5 text-muted-foreground">
          {message ? <span className={TONE_TEXT.danger}>{message}</span> : (unavailable ?? hint)}
        </div>
      </div>

      {/* 表单 */}
      <div className="flex min-w-0 flex-col gap-4">
        <div className="flex flex-col gap-2">
          <Label htmlFor={amountId} className="text-xs text-muted-foreground">
            {addOnly ? "增加数量" : "方向与数量"}
          </Label>
          <div className="flex flex-col gap-2 sm:flex-row">
            {addOnly ? null : (
              <Segmented
                value={direction}
                onChange={setDirection}
                options={DIRECTIONS}
                ariaLabel={`${title}调整方向`}
                className="shrink-0 self-start sm:self-auto"
              />
            )}
            <div className="relative min-w-0 flex-1">
              <Input
                id={amountId}
                value={amount}
                inputMode={decimal ? "decimal" : "numeric"}
                placeholder={decimal ? "0.00" : "0"}
                disabled={blocked}
                maxLength={decimal ? 16 : 12}
                className="pr-12 tabular-nums"
                onChange={(event) => setAmount(event.target.value.replace(/[^\d.]/g, ""))}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    submit();
                  }
                }}
              />
              <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-muted-foreground">
                {decimal ? "元" : unit}
              </span>
            </div>
          </div>
          <QuickChips
            items={quick}
            value={trimmed}
            onPick={setAmount}
            render={(item) => (decimal ? `¥${item}` : formatIntegerString(item))}
          />
        </div>

        {extra?.form ? <div className="flex flex-col gap-2">{extra.form}</div> : null}

        <div className="flex flex-col gap-2">
          <Label className="text-xs text-muted-foreground">原因</Label>
          <ReasonInput
            value={reason}
            onChange={setReason}
            suggestions={reasons}
            placeholder="选填，会写入流水"
            chips
            onEnter={submit}
          />
        </div>

        <div className="flex flex-col gap-3 border-t pt-4 sm:flex-row sm:items-center sm:justify-between">
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <ShieldCheck className="size-3.5 shrink-0" />
            同一笔重试只生效一次
          </p>
          <Button size="sm" disabled={disabled} onClick={submit}>
            {pending ? <Loader2 className="animate-spin" /> : direction === "add" ? <Plus /> : <Minus />}
            {amountValid ? `${verb} ${amountLabel}` : verb}
          </Button>
        </div>
      </div>

      <AlertDialog open={Boolean(confirming)} onOpenChange={(open) => !open && !pending && setConfirming(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              确认{verb} {amountLabel}？
            </AlertDialogTitle>
            <AlertDialogDescription>提交后立即生效并写入流水，如需撤回请在流水中冲正。</AlertDialogDescription>
          </AlertDialogHeader>
          {confirming ? (
            <dl className="grid gap-2.5 rounded-lg border bg-muted/30 p-3 text-sm">
              <div className="flex items-center justify-between gap-3">
                <dt className="text-xs text-muted-foreground">{title}</dt>
                <dd className="flex items-center gap-1.5 tabular-nums">
                  <span>{current !== undefined ? format(current) : "—"}</span>
                  <ArrowRight className="size-3.5 text-muted-foreground" />
                  <span className={cn("font-medium", direction === "add" ? TONE_TEXT.success : TONE_TEXT.warning)}>
                    {after !== null ? format(after) : "—"}
                  </span>
                </dd>
              </div>
              {extra?.confirm ? (
                <div className="flex items-center justify-between gap-3">
                  <dt className="text-xs text-muted-foreground">等级</dt>
                  <dd className="min-w-0 text-right">{extra.confirm}</dd>
                </div>
              ) : null}
              <div className="flex items-center justify-between gap-3">
                <dt className="text-xs text-muted-foreground">原因</dt>
                <dd className="min-w-0 truncate">{confirming.reason || "未填写"}</dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="text-xs text-muted-foreground">请求编号</dt>
                <dd className="min-w-0 truncate font-mono text-xs text-muted-foreground">{confirming.requestId}</dd>
              </div>
            </dl>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>取消</AlertDialogCancel>
            <Button
              variant={direction === "deduct" ? "destructive" : "default"}
              disabled={pending}
              onClick={() => confirming && void run(confirming)}
            >
              {pending ? <Loader2 className="animate-spin" /> : null}
              确认{verb}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/** 带外框的调整卡（兼容旧用法）：标题行 + {@link AdjustForm}。 */
export function AdjustCard({
  icon,
  ...props
}: AdjustFormProps & {
  icon: React.ReactNode;
}) {
  return (
    <Card className="gap-0 overflow-hidden py-0">
      <div className="flex items-center gap-2 px-4 pt-4 pb-3 text-sm font-semibold sm:px-5">
        <span className="text-muted-foreground">{icon}</span>
        {props.title}
      </div>
      <div className="border-t px-4 py-4 sm:px-5">
        <AdjustForm {...props} />
      </div>
    </Card>
  );
}
