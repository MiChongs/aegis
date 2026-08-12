"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import {
  CheckCircle2,
  ClipboardCheck,
  Clock,
  Copy,
  Loader2,
  RefreshCw,
  ShieldCheck,
  TriangleAlert,
  XCircle
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { toast } from "sonner";
import {
  formatPayAmount,
  getPayReturnResult,
  payBrandSlug,
  payMethodLabel,
  payTypeLabel,
  type PayReturnView
} from "@/lib/api/pay-result";
import { PaymentBrandBadge } from "@/components/payment/payment-brand-icon";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle
} from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/**
 * 支付结果页。
 *
 * 渠道在用户付完款后把浏览器跳到这里（配置项「同步跳转地址」），并把与异步通知
 * 同一批带签名的参数附在 URL 上。页面把整串 query 原样交给服务端换取订单状态。
 *
 * **这一页不判断支付是否成功，只显示服务端的结论。** URL 上确实带着
 * `trade_status=TRADE_SUCCESS`，但那串地址停在用户手里 —— 可以刷新、可以只走一半、
 * 可以转发给别人。真正的到账与否只有渠道打给服务端的那次异步通知说得算。
 * 拿浏览器上的参数当结论，是收银界面能犯的最严重的错误。
 *
 * 于是「还在确认」是**正常且常见**的首屏状态：浏览器跳回来往往快过服务器通知。
 * 页面据此轮询而不是报错，并把「还要等多久」画出来 —— 一个不说明自己在等什么的
 * 转圈动画，会让人以为卡死了然后重复付款。超时后交回给用户手动刷新：
 * 无限轮询在真失败时只会变成一个永远转圈的页面。
 */

const POLL_INTERVAL_MS = 3_000;
/** 约 2 分钟。超过这个时间还没到账，多半是渠道那边出了事，继续空转没有意义。 */
const MAX_POLLS = 40;

type Fetched =
  | { status: "loading" }
  | { status: "ready"; view: PayReturnView }
  | { status: "error"; message: string };

export function PayResultView() {
  const searchParams = useSearchParams();
  const query = searchParams.toString();
  const hasQuery = query.length > 0;

  const [fetched, setFetched] = useState<Fetched>({ status: "loading" });
  const [attempts, setAttempts] = useState(0);
  const [gaveUp, setGaveUp] = useState(false);
  const [countdown, setCountdown] = useState<number | null>(null);
  // 没有 query 时下面的 effect 直接不跑，busy 初值给 true 会让刷新按钮永远是禁用态
  const [busy, setBusy] = useState(hasQuery);
  // 手动刷新靠自增这个值重跑整条轮询链，而不是另写一份取数逻辑 ——
  // 两份逻辑迟早会在「刷新之后还要不要继续轮询」上产生分歧。
  const [cycle, setCycle] = useState(0);

  useEffect(() => {
    if (!hasQuery) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let ticker: ReturnType<typeof setInterval> | undefined;

    const schedule = (attempt: number) => {
      let remain = POLL_INTERVAL_MS / 1000;
      setCountdown(remain);
      ticker = setInterval(() => {
        remain -= 1;
        setCountdown(remain > 0 ? remain : 0);
      }, 1_000);
      timer = setTimeout(() => {
        if (ticker) clearInterval(ticker);
        setBusy(true);
        void run(attempt);
      }, POLL_INTERVAL_MS);
    };

    // 整个函数体第一句就是 await：effect 同步阶段不会 setState，
    // 既不触发级联渲染，也过得了 react-hooks/set-state-in-effect。
    const run = async (attempt: number) => {
      try {
        const result = await getPayReturnResult(query);
        if (cancelled) return;
        setFetched({ status: "ready", view: result });
        setBusy(false);
        // 只对「还在确认」继续等：已支付 / 已过期 / 失败都是终态，再问也不会变。
        if (!result.pending) {
          setCountdown(null);
          return;
        }
        const next = attempt + 1;
        setAttempts(next);
        if (next >= MAX_POLLS) {
          setGaveUp(true);
          setCountdown(null);
          return;
        }
        schedule(next);
      } catch (err) {
        if (cancelled) return;
        setFetched({ status: "error", message: err instanceof Error ? err.message : "查询失败" });
        setBusy(false);
        setCountdown(null);
      }
    };

    void run(0);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
      if (ticker) clearInterval(ticker);
    };
  }, [query, hasQuery, cycle]);

  const refresh = useCallback(() => {
    setBusy(true);
    setAttempts(0);
    setGaveUp(false);
    setCountdown(null);
    setCycle((value) => value + 1);
  }, []);

  const view = fetched.status === "ready" ? fetched.view : null;
  const tone = useMemo(
    () => resolveTone({ hasQuery, fetched, gaveUp }),
    [hasQuery, fetched, gaveUp]
  );

  if (hasQuery && fetched.status === "loading") {
    return <PayResultSkeleton />;
  }

  const waiting = tone.key === "pending";
  // 没有 query 时不给刷新按钮：这一页的参数由渠道跳转生成，刷新一百次也不会长出来。
  const retryable = hasQuery && (waiting || tone.key === "error");

  return (
    // 本项目的 Tooltip 不自带 Provider（挂在 ConsoleShell 上），而这一页在 Shell 之外。
    // 少了它，页面上任何一个 Tooltip 都会在渲染时直接抛错、整页落到错误边界。
    <TooltipProvider delayDuration={200}>
      <Card className="overflow-hidden shadow-sm">
        {/* CardHeader 是 grid，靠 justify-items-center 而不是 items-center 做水平居中 */}
        <CardHeader className="justify-items-center gap-3 text-center">
          <span
            className={cn(
              "flex size-14 items-center justify-center rounded-full ring-8",
              tone.iconClass,
              tone.ringClass
            )}
          >
            <tone.icon className={cn("size-7", tone.spin && "animate-spin")} aria-hidden />
          </span>
          <div className="w-full space-y-1.5">
            <CardTitle className="text-xl tracking-tight">{tone.title}</CardTitle>
            {tone.description ? (
              <CardDescription className="leading-6">{tone.description}</CardDescription>
            ) : null}
          </div>
          <Badge variant={tone.badgeVariant}>{tone.badgeLabel}</Badge>
        </CardHeader>

        <CardContent className="space-y-5">
          {view ? <AmountHero view={view} /> : null}

          {fetched.status === "error" ? (
            <Alert variant="destructive">
              <TriangleAlert aria-hidden />
              <AlertTitle>服务端返回</AlertTitle>
              <AlertDescription>{fetched.message}</AlertDescription>
            </Alert>
          ) : null}

          {!hasQuery ? (
            <Alert>
              <TriangleAlert aria-hidden />
              <AlertTitle>缺少回跳参数</AlertTitle>
              <AlertDescription>
                这个地址由支付渠道跳转时生成。请回到应用内查看订单状态。
              </AlertDescription>
            </Alert>
          ) : null}

          {view ? <OrderFacts view={view} /> : null}

          {waiting ? <WaitingMeter attempts={attempts} countdown={countdown} busy={busy} /> : null}
        </CardContent>

        {retryable ? (
          <>
            <Separator />
            <CardFooter className="flex-col gap-3">
              <Button variant="outline" className="w-full" onClick={refresh} disabled={busy}>
                {busy ? (
                  <Loader2 className="animate-spin" aria-hidden />
                ) : (
                  <RefreshCw aria-hidden />
                )}
                刷新订单状态
              </Button>
              {gaveUp ? (
                <p className="text-center text-xs leading-5 text-muted-foreground">
                  已等待约 2 分钟仍未收到到账通知。若确认已扣款，回到应用查看订单或联系客服，
                  重复支付不会加快到账。
                </p>
              ) : null}
            </CardFooter>
          </>
        ) : null}
      </Card>
    </TooltipProvider>
  );
}

/**
 * 骨架屏。
 *
 * 首屏不画一个空卡片再突然填满：这一页承载的是「我的钱怎么样了」，
 * 布局跳变会被读成状态跳变。骨架的形状与真实内容一一对应。
 */
export function PayResultSkeleton() {
  return (
    <Card className="overflow-hidden shadow-sm">
      <CardHeader className="items-center gap-4 pb-2">
        <Skeleton className="size-14 rounded-full" />
        <div className="w-full space-y-2">
          <Skeleton className="mx-auto h-6 w-40" />
          <Skeleton className="mx-auto h-4 w-64" />
        </div>
        <Skeleton className="h-5 w-16 rounded-full" />
      </CardHeader>
      <CardContent className="space-y-5">
        <Skeleton className="h-20 w-full rounded-xl" />
        <div className="space-y-3">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-5/6" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      </CardContent>
    </Card>
  );
}

/**
 * 金额主体。
 *
 * 付款人最先找的就是这个数字，因此它单独占一块、字号压过标题之外的一切。
 * 金额是服务端给的定点字符串，这里只做展示，**绝不参与任何数值运算** —— 钱不走浮点。
 */
function AmountHero({ view }: { view: PayReturnView }) {
  return (
    <div className="rounded-xl border bg-muted/40 px-4 py-4 text-center">
      <div className="text-xs text-muted-foreground">支付金额</div>
      <div className="mt-1 text-3xl font-semibold tabular-nums tracking-tight">
        {formatPayAmount(view.amount, view.currency)}
      </div>
      {view.subject ? (
        <div className="mt-1.5 truncate text-sm text-muted-foreground">{view.subject}</div>
      ) : null}
    </div>
  );
}

function OrderFacts({ view }: { view: PayReturnView }) {
  const channel = payMethodLabel(view.payment_method);
  const sub = payTypeLabel(view.provider_type);
  const channelText = !sub || sub === channel ? channel : `${channel} · ${sub}`;

  return (
    <dl className="divide-y rounded-xl border">
      <Fact label="订单号">
        <CopyableOrderNo value={view.order_no} />
      </Fact>
      <Fact label="支付方式">
        <span className="flex items-center justify-end gap-2">
          <PaymentBrandBadge
            slug={payBrandSlug(view.payment_method, view.provider_type)}
            name={channel}
            size="sm"
          />
          <span className="truncate">{channelText}</span>
        </span>
      </Fact>
      {view.paid_at ? <Fact label="支付时间">{formatLocalTime(view.paid_at)}</Fact> : null}
    </dl>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 px-4 py-3 text-sm">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-right">{children}</dd>
    </div>
  );
}

/**
 * 订单号一键复制。
 *
 * 找客服时对方第一句就是「订单号发我」，而这串东西没人抄得对。
 * 复制失败（非安全上下文 / 权限被拒）如实提示，不做静默失败 ——
 * 那会让人以为已经复制好了，粘贴出来却是别的东西。
 */
function CopyableOrderNo({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      toast.success("订单号已复制");
      setTimeout(() => setCopied(false), 2_000);
    } catch {
      toast.error("复制失败，请手动选中订单号");
    }
  }

  return (
    <span className="flex items-center justify-end gap-1">
      <span className="truncate font-mono text-xs">{value}</span>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="ghost" size="icon" className="size-7 shrink-0" onClick={copy} aria-label="复制订单号">
            {copied ? <ClipboardCheck className="text-emerald-600 dark:text-emerald-400" /> : <Copy />}
          </Button>
        </TooltipTrigger>
        <TooltipContent>复制订单号</TooltipContent>
      </Tooltip>
    </span>
  );
}

/**
 * 等待进度。
 *
 * 光转圈不说明在等什么，人会以为页面卡死然后去重复支付。这里把三件事画出来：
 * 已经等了多久、下一次什么时候问、最长等到什么时候为止。
 */
function WaitingMeter({
  attempts,
  countdown,
  busy
}: {
  attempts: number;
  countdown: number | null;
  busy: boolean;
}) {
  const elapsed = Math.round((attempts * POLL_INTERVAL_MS) / 1000);
  const percent = Math.min(100, Math.round((attempts / MAX_POLLS) * 100));

  return (
    <div className="space-y-2">
      <Progress value={percent} className="h-1.5" />
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <span>已等待 {elapsed} 秒</span>
        <span>
          {busy ? "正在查询" : countdown !== null ? `${countdown} 秒后自动重试` : "等待中"}
        </span>
      </div>
    </div>
  );
}

type Tone = {
  key: "pending" | "paid" | "expired" | "failed" | "error";
  icon: LucideIcon;
  spin: boolean;
  iconClass: string;
  ringClass: string;
  badgeVariant: "success" | "warning" | "danger" | "secondary";
  badgeLabel: string;
  title: string;
  /** 省略表示这一档的说明已经在下方 Alert 里，标题下不必再写一遍 */
  description?: string;
};

/**
 * 五种状态各自的说法。
 *
 * 「还在确认」刻意不用告警色、也不叫「失败」：绝大多数情况下它只是通知还在路上，
 * 把它画成红色会让一笔正在成功的支付看起来像出了事，而那种误判的代价是重复支付。
 *
 * 配色一律走语义色阶（emerald / amber / destructive）而不是写死的 rgba，
 * 深浅两套主题下都成立 —— 这一页会在各种手机浏览器里被打开，
 * 其中相当一部分跟随系统开着深色。
 */
function resolveTone({
  hasQuery,
  fetched,
  gaveUp
}: {
  hasQuery: boolean;
  fetched: Fetched;
  gaveUp: boolean;
}): Tone {
  const failed: Omit<Tone, "title" | "description" | "key"> = {
    icon: XCircle,
    spin: false,
    iconClass: "bg-destructive/10 text-destructive",
    ringClass: "ring-destructive/5",
    badgeVariant: "danger",
    badgeLabel: "未完成"
  };

  if (!hasQuery) {
    return {
      ...failed,
      key: "error",
      icon: TriangleAlert,
      title: "无法识别这笔订单"
    };
  }
  if (fetched.status === "error") {
    return {
      ...failed,
      key: "error",
      title: "无法确认支付结果",
      description: "没能从服务端取到这笔订单的状态，稍后重试或回到应用查看。"
    };
  }
  if (fetched.status === "loading") {
    return {
      key: "pending",
      icon: Loader2,
      spin: true,
      iconClass: "bg-muted text-muted-foreground",
      ringClass: "ring-muted/50",
      badgeVariant: "secondary",
      badgeLabel: "查询中",
      title: "正在查询订单",
      description: "正在向服务端确认这笔交易的状态。"
    };
  }

  switch (fetched.view.status) {
    case "paid":
      return {
        key: "paid",
        icon: CheckCircle2,
        spin: false,
        iconClass: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
        ringClass: "ring-emerald-500/5",
        badgeVariant: "success",
        badgeLabel: "已支付",
        title: "支付成功",
        description: "款项已确认到账，可以关闭本页回到应用。"
      };
    case "expired":
      return {
        key: "expired",
        icon: Clock,
        spin: false,
        iconClass: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
        ringClass: "ring-amber-500/5",
        badgeVariant: "warning",
        badgeLabel: "已关闭",
        title: "订单已过期",
        description: "这笔订单超过有效期已自动关闭，请回到应用重新发起支付。"
      };
    case "failed":
      return {
        ...failed,
        key: "failed",
        badgeLabel: "已失败",
        title: "支付失败",
        description: "渠道未能完成这笔交易，请回到应用重新发起支付。"
      };
    default:
      return gaveUp
        ? {
            key: "pending",
            icon: Clock,
            spin: false,
            iconClass: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
            ringClass: "ring-amber-500/5",
            badgeVariant: "warning",
            badgeLabel: "待确认",
            title: "还没等到到账通知",
            description: "渠道的通知迟迟没有到达，可以手动刷新，或回到应用查看订单。"
          }
        : {
            key: "pending",
            icon: Loader2,
            spin: true,
            iconClass: "bg-primary/10 text-primary",
            ringClass: "ring-primary/5",
            badgeVariant: "secondary",
            badgeLabel: "确认中",
            title: "正在确认到账",
            description: "渠道的服务器通知通常比页面跳转晚几秒，本页会自动确认。已扣款请稍候。"
          };
  }
}

/** 页脚的安全说明。放在卡片外面：它对每一种状态都成立。 */
export function PayResultNote() {
  return (
    <p className="flex items-start justify-center gap-1.5 px-2 text-center text-xs leading-5 text-muted-foreground">
      <ShieldCheck className="mt-0.5 size-3.5 shrink-0" aria-hidden />
      <span>本页只展示订单状态，可以安全关闭。权益由服务端在收到渠道通知后发放。</span>
    </p>
  );
}

/** 服务端时间戳是 UTC，原样显示等于拿 UTC 冒充本地时间。 */
function formatLocalTime(raw: string): string {
  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) return raw;
  return parsed.toLocaleString(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}
