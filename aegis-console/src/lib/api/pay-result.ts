import { apiRequest } from "./client";

/**
 * 支付结果页的取数入口。
 *
 * 与本目录其余模块不同，这条**不带 token**：付款人是应用的终端用户，
 * 被渠道跳回来的浏览器上没有控制台登录态。凭据是渠道附在 return_url 上的
 * 签名 query 本身，由服务端用该订单所属渠道的商户密钥验证。
 */

/** `GET /api/public/pay/return` 的响应。 */
export type PayReturnView = {
  order_no: string;
  subject: string;
  /** 定点字符串（服务端是 decimal）。**不要转成 number**，钱不能走浮点。 */
  amount: string;
  currency?: string;
  payment_method: string;
  provider_type?: string;
  status: string;
  paid_at?: string;
  /**
   * 异步通知还没到。**这不是失败** —— 浏览器跳回来通常快过渠道的服务器通知，
   * 页面据此继续轮询，而不是当场告诉用户支付失败。
   */
  pending: boolean;
};

/**
 * 把渠道回跳时附上的整串 query 原样交给服务端换取订单状态。
 *
 * 原样转发是刻意的：签名覆盖的是渠道发出的**全部**参数，页面这边挑挑拣拣
 * 只会让验签失败，而且失败原因（少传了哪个参数）在这一层根本看不出来。
 */
export function getPayReturnResult(rawQuery: string) {
  const query = rawQuery.startsWith("?") ? rawQuery : `?${rawQuery}`;
  return apiRequest<PayReturnView>(`/api/public/pay/return${query}`);
}

/** 渠道 key → 中文名。表里没有的照原样显示，新渠道上线时不至于变成空白。 */
export function payMethodLabel(method: string): string {
  const table: Record<string, string> = {
    balance: "余额支付",
    epay: "易支付",
    rainbow_epay: "彩虹易支付",
    xunhupay: "虎皮椒",
    payjs: "PAYJS",
    qrpay: "码支付",
    vmqpay: "V免签",
    alipay_native: "支付宝",
    wechat_native: "微信支付",
    stripe: "Stripe",
    paypal: "PayPal",
    paddle: "Paddle",
    lemonsqueezy: "Lemon Squeezy",
    razorpay: "Razorpay",
    coinbase: "Coinbase Commerce",
    square: "Square"
  };
  return table[method?.toLowerCase()] ?? method;
}

/** 子支付类型 → 中文名。空串表示这个渠道没有子类型概念。 */
export function payTypeLabel(providerType?: string): string {
  const table: Record<string, string> = {
    alipay: "支付宝",
    wxpay: "微信支付",
    wechat: "微信支付",
    qqpay: "QQ 钱包",
    bank: "网银"
  };
  return table[providerType?.toLowerCase() ?? ""] ?? "";
}

/** 金额展示。服务端给的是定点字符串，这里只补币种符号，绝不做数值运算。 */
export function formatPayAmount(amount: string, currency?: string): string {
  const value = (amount ?? "").trim() || "0.00";
  switch ((currency ?? "").toUpperCase()) {
    case "":
    case "CNY":
      return `¥${value}`;
    case "USD":
      return `$${value}`;
    default:
      return `${value} ${(currency ?? "").toUpperCase()}`;
  }
}
