"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import { CheckCircle2, Clock, Loader2, RefreshCw, XCircle } from "lucide-react";
import {
  formatPayAmount,
  getPayReturnResult,
  payMethodLabel,
  payTypeLabel,
  type PayReturnView
} from "@/lib/api/pay-result";

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
 * 于是「还在待支付」是**正常且常见**的首屏状态：浏览器跳回来往往快过服务器通知。
 * 页面据此轮询而不是报错，超时后交回给用户手动刷新 —— 无限轮询在真失败时
 * 只会变成一个永远转圈的页面。
 */
export function PayResultView() {
  const searchParams = useSearchParams();
  const query = searchParams.toString();

  const [view, setView] = useState<PayReturnView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [gaveUp, setGaveUp] = useState(false);
  const attemptsRef = useRef(0);

  const load = useCallback(
    async (silent: boolean) => {
      if (!query) {
        setError("缺少支付回跳参数。请回到应用内查看订单状态。");
        setLoading(false);
        return null;
      }
      if (!silent) setLoading(true);
      try {
        const result = await getPayReturnResult(query);
        setView(result);
        setError(null);
        return result;
      } catch (err) {
        setError(err instanceof Error ? err.message : "查询失败");
        return null;
      } finally {
        setLoading(false);
      }
    },
    [query]
  );

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const tick = async (silent: boolean) => {
      const result = await load(silent);
      if (cancelled) return;
      // 只对「待支付」继续等：已支付 / 已过期 / 失败都是终态，再问也不会变。
      if (!result?.pending) return;
      attemptsRef.current += 1;
      if (attemptsRef.current >= MAX_POLLS) {
        setGaveUp(true);
        return;
      }
      timer = setTimeout(() => void tick(true), POLL_INTERVAL_MS);
    };

    void tick(false);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [load]);

  const manualRefresh = () => {
    attemptsRef.current = 0;
    setGaveUp(false);
    void load(false);
  };

  const tone = resolveTone(view, error);

  return (
    <div
      className="relative z-10 mx-auto flex w-full max-w-[520px] flex-col gap-6 rounded-[32px] p-7 max-md:rounded-[24px] max-md:p-5"
      style={{
        border: "1px solid rgba(255,255,255,0.1)",
        background:
          "linear-gradient(145deg, rgba(255,255,255,0.08), rgba(255,255,255,0.02)), rgba(10,14,21,0.42)",
        backdropFilter: "blur(18px)"
      }}
    >
      <div className="flex flex-col items-center gap-4 text-center">
        <div
          className="flex h-16 w-16 items-center justify-center rounded-full"
          style={{ background: tone.iconBackground, color: tone.iconColor }}
        >
          <tone.Icon className={tone.spin ? "h-8 w-8 animate-spin" : "h-8 w-8"} aria-hidden />
        </div>
        <div className="flex flex-col gap-1.5">
          <h1 className="text-xl font-semibold text-white md:text-2xl">{tone.title}</h1>
          <p className="text-sm text-white/70">{tone.description}</p>
        </div>
      </div>

      {view ? (
        <dl className="flex flex-col gap-3 rounded-2xl p-4" style={{ background: "rgba(255,255,255,0.05)" }}>
          <Row label="商品">{view.subject || "订单"}</Row>
          <Row label="金额">
            <span className="text-base font-semibold text-white">
              {formatPayAmount(view.amount, view.currency)}
            </span>
          </Row>
          <Row label="订单号">
            <span className="break-all" style={{ fontFamily: "var(--font-mono)" }}>
              {view.order_no}
            </span>
          </Row>
          <Row label="支付方式">{channelText(view)}</Row>
          {view.paid_at ? <Row label="支付时间">{formatLocalTime(view.paid_at)}</Row> : null}
        </dl>
      ) : null}

      {view?.pending || error ? (
        <button
          type="button"
          onClick={manualRefresh}
          disabled={loading}
          className="flex h-11 w-full items-center justify-center gap-2 rounded-full text-sm font-medium text-white transition disabled:opacity-50"
          style={{ border: "1px solid rgba(255,255,255,0.18)", background: "rgba(255,255,255,0.06)" }}
        >
          {loading ? (
            <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
          ) : (
            <RefreshCw className="h-4 w-4" aria-hidden />
          )}
          刷新订单状态
        </button>
      ) : null}

      <p className="text-center text-xs text-white/50">
        本页只展示订单状态，可以安全关闭。权益由服务端在支付确认后发放，请回到应用内查看。
      </p>
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 text-sm">
      <dt className="shrink-0 text-white/55">{label}</dt>
      <dd className="min-w-0 text-right text-white/90">{children}</dd>
    </div>
  );
}

function channelText(view: PayReturnView): string {
  const channel = payMethodLabel(view.payment_method);
  const sub = payTypeLabel(view.provider_type);
  return !sub || sub === channel ? channel : `${channel} · ${sub}`;
}

type Tone = {
  Icon: typeof CheckCircle2;
  spin: boolean;
  iconColor: string;
  iconBackground: string;
  title: string;
  description: string;
};

/**
 * 四种状态各自的说法。
 *
 * 「待支付」刻意不用告警色也不叫「失败」：绝大多数情况下它只是通知还在路上，
 * 把它画成红色会让一笔正在成功的支付看起来像出了事。
 */
function resolveTone(view: PayReturnView | null, error: string | null): Tone {
  if (error) {
    return {
      Icon: XCircle,
      spin: false,
      iconColor: "#fecaca",
      iconBackground: "rgba(239,68,68,0.18)",
      title: "无法确认支付结果",
      description: error
    };
  }
  if (!view) {
    return {
      Icon: Loader2,
      spin: true,
      iconColor: "#e2e8f0",
      iconBackground: "rgba(255,255,255,0.1)",
      title: "正在查询订单",
      description: "正在向服务端确认这笔交易的状态。"
    };
  }
  switch (view.status) {
    case "paid":
      return {
        Icon: CheckCircle2,
        spin: false,
        iconColor: "#bbf7d0",
        iconBackground: "rgba(34,197,94,0.18)",
        title: "支付成功",
        description: "款项已确认到账，可以关闭本页并回到应用。"
      };
    case "expired":
      return {
        Icon: Clock,
        spin: false,
        iconColor: "#fed7aa",
        iconBackground: "rgba(249,115,22,0.18)",
        title: "订单已过期",
        description: "这笔订单已超过有效期并自动关闭，请回到应用重新发起支付。"
      };
    case "failed":
      return {
        Icon: XCircle,
        spin: false,
        iconColor: "#fecaca",
        iconBackground: "rgba(239,68,68,0.18)",
        title: "支付失败",
        description: "渠道未能完成这笔交易，请回到应用重新发起支付。"
      };
    default:
      return {
        Icon: Loader2,
        spin: true,
        iconColor: "#e2e8f0",
        iconBackground: "rgba(255,255,255,0.1)",
        title: "正在确认支付结果",
        description:
          "渠道的支付通知通常比页面跳转晚几秒到达，本页会自动确认。若已扣款请稍候片刻。"
      };
  }
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

const POLL_INTERVAL_MS = 3_000;
/** 约 2 分钟。超过这个时间还没到账，多半是渠道那边失败了，继续空转没有意义。 */
const MAX_POLLS = 40;
