"""支付与交易：订单、退款、支付凭证、支付渠道配置、应用交易设置与交易概览。

/api/admin/app/payment-config/* 是旧式接口：一律 POST、应用 ID 放在请求体 appid 里。
其中的查询接口（列表、详情、可退额度）同样是 POST；规则表按 payment:read 判定，只读 MCP 密钥可以调用。
"""

from __future__ import annotations

from decimal import Decimal, InvalidOperation
from typing import Annotated, Any, Literal

from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import Field

from ..core import DESTRUCTIVE, GET, POST, PUT, READ, WRITE, AppRef, IsoTime, Limit, Page, app_id_of, body, q

NAMESPACE = "payment"
server = FastMCP("payment")

P = "/api/admin/app/payment-config"
METHODS = POST(f"{P}/methods")
CONFIG_LIST = POST(f"{P}/list")
CONFIG_DETAIL = POST(f"{P}/detail")
CONFIG_CREATE = POST(f"{P}/create")
CONFIG_UPDATE = POST(f"{P}/update")
CONFIG_DELETE = POST(f"{P}/delete")
CONFIG_TEST = POST(f"{P}/test")
EPAY_INIT = POST(f"{P}/epay/init")
ORDER_LIST = POST(f"{P}/orders/list")
ORDER_DETAIL = POST(f"{P}/orders/detail")
ORDER_RECEIPT_EMAIL = POST(f"{P}/orders/receipt/email")
RECEIPT_OPTIONS = POST(f"{P}/receipt/options")
REFUND_CREATE = POST(f"{P}/refunds/create")
REFUND_LIST = POST(f"{P}/refunds/list")
REFUND_ORDER = POST(f"{P}/refunds/order")
REFUNDABLE = POST(f"{P}/refunds/refundable")
REFUND_SYNC = POST(f"{P}/refunds/sync")
COMMERCE = GET("/api/admin/apps/:appkey/commerce")
COMMERCE_UPDATE = PUT("/api/admin/apps/:appkey/commerce")
COMMERCE_OVERVIEW = GET("/api/admin/apps/:appkey/commerce/overview")

Method = Annotated[str, Field(description="支付渠道标识：epay / rainbow_epay / xunhupay / payjs / qrpay / vmqpay / alipay_native / wechat_native / douyinpay / "
                                          "stripe / paypal / paddle / lemonsqueezy / razorpay / coinbase / square / afdian / balance（以 payment_methods 为准）")]
OrderNo = Annotated[str, Field(min_length=1, description="订单号（P 开头）")]
ConfigID = Annotated[int, Field(gt=0, description="支付配置 ID（payment_configs 结果里的 id）")]
DocumentType = Annotated[Literal["", "receipt", "invoice", "credit_note"], Field(description="凭证类型：receipt 收据 / invoice 发票 / credit_note 退款凭证；留空按订单状态推导")]


def _money(value: str) -> str:
    text = value.strip()
    try:
        amount = Decimal(text)
    except InvalidOperation as exc:
        raise ToolError("金额格式无效，请用十进制数字字符串，如 9.90") from exc
    if not amount.is_finite() or amount <= 0:
        raise ToolError("退款金额必须大于 0（全额退款请留空）")
    return text


# ── 渠道与配置 ──

@server.tool(**WRITE("payment", idempotent=True))
async def methods() -> Any:
    """平台支持的支付渠道目录：每个渠道的标识、名称、子支付类型、配置字段说明与能力（是否支持退款等）。新建配置前先看它。"""
    return await METHODS(body={})


@server.tool(**READ("payment"))
async def configs(
    app: AppRef,
    payment_method: Annotated[str, Field(description="只看某个渠道（如 epay），留空看全部")] = "",
    enabled_only: Annotated[bool, Field(description="只看启用中的配置")] = False,
) -> Any:
    """应用的支付配置列表。注意：返回里含渠道密钥等敏感配置，不要原样转述给无关的人。需要 payment:read。"""
    return await CONFIG_LIST(body=body(appid=await app_id_of(app), payment_method=payment_method or None, enabled_only=enabled_only or None))


@server.tool(**READ("payment"))
async def config(app: AppRef, config_id: ConfigID) -> Any:
    """一份支付配置的详情（含 config_data 原文，内有商户密钥，注意保密）。需要 payment:read。"""
    return await CONFIG_DETAIL(body={"appid": await app_id_of(app), "config_id": config_id})


@server.tool(**WRITE("payment"))
async def create_config(
    app: AppRef,
    payment_method: Method,
    config_name: Annotated[str, Field(min_length=1, description="配置名称（同一渠道下区分多套商户，下单时与渠道一起定位配置）")],
    config_data: Annotated[dict[str, Any], Field(description="渠道配置（商户号、密钥、网关地址等），字段见 payment_methods 里该渠道的说明；保存时由渠道校验")],
    enabled: Annotated[bool, Field(description="是否启用")] = True,
    is_default: Annotated[bool | None, Field(description="是否设为默认配置，不传按后端默认")] = None,
    description: Annotated[str, Field(description="备注")] = "",
) -> Any:
    """新建一份支付配置（启用后用户即可用它下单付款）。执行前先向用户确认渠道与商户信息。需要 payment:write。"""
    return await CONFIG_CREATE(body=body(
        appid=await app_id_of(app), payment_method=payment_method, config_name=config_name, config_data=config_data,
        enabled=enabled, is_default=is_default, description=description or None,
    ))


@server.tool(**WRITE("payment"))
async def update_config(
    app: AppRef,
    config_id: ConfigID,
    config_name: Annotated[str | None, Field(description="新名称，不改留空")] = None,
    config_data: Annotated[dict[str, Any] | None, Field(description="要修改的渠道配置字段，不改留空")] = None,
    merge_config_data: Annotated[bool, Field(description="true：config_data 与现有配置逐键合并（只改给出的键）；false：用 config_data 整体替换")] = True,
    enabled: Annotated[bool | None, Field(description="启用 / 停用（停用后不能再用它下单）")] = None,
    is_default: Annotated[bool | None, Field(description="是否设为默认配置")] = None,
    description: Annotated[str | None, Field(description="新备注（后端不支持清空，传空串视为不改）")] = None,
) -> Any:
    """修改支付配置。只传要改的字段；改密钥或停用会立即影响用户付款，执行前先确认。需要 payment:write。"""
    app_id = await app_id_of(app)
    data = config_data
    if config_data is not None and merge_config_data:
        current = await CONFIG_DETAIL(body={"appid": app_id, "config_id": config_id}) or {}
        data = {**(current.get("config_data") or current.get("configData") or {}), **config_data}
    return await CONFIG_UPDATE(body=body(
        appid=app_id, config_id=config_id, config_name=config_name or None, config_data=data,
        enabled=enabled, is_default=is_default, description=description or None,
    ))


@server.tool(**DESTRUCTIVE("payment"))
async def delete_config(app: AppRef, config_id: ConfigID) -> Any:
    """删除一份支付配置（不可恢复，用它的渠道立即无法下单；已有订单不受影响）。务必先向用户确认配置名称。需要 payment:write。"""
    return await CONFIG_DELETE(body={"appid": await app_id_of(app), "config_id": config_id})


@server.tool(**WRITE("payment", idempotent=True))
async def test_config(app: AppRef, config_id: ConfigID) -> Any:
    """连通性测试：用这份配置向渠道发起一次探测（不产生真实订单），返回是否可用与诊断信息。需要 payment:write。"""
    return await CONFIG_TEST(body={"appid": await app_id_of(app), "config_id": config_id})


@server.tool(**WRITE("payment"))
async def init_epay(
    app: AppRef,
    epay_config: Annotated[dict[str, Any], Field(description="易支付配置：pid、key、apiUrl、sitename、notifyUrl、returnUrl、signType、supportedTypes、"
                                                             "expireMinutes、minAmount、maxAmount、allowedIPs、verifyIP")],
) -> Any:
    """一键初始化应用的默认易支付配置（epay 渠道）。会创建或覆盖默认的易支付配置，执行前先确认。需要 payment:write。"""
    return await EPAY_INIT(body={"appid": await app_id_of(app), "epay_config": epay_config})


# ── 订单与凭证 ──

@server.tool(**READ("payment"))
async def orders(
    app: AppRef,
    status: Annotated[str, Field(description="订单状态：pending 待支付 / paid 已支付 / expired 已过期 等，留空不限")] = "",
    payment_method: Annotated[str, Field(description="支付渠道（如 epay、balance），留空不限")] = "",
    keyword: Annotated[str, Field(description="按订单号 / 渠道单号 / 标题搜索")] = "",
    user_id: Annotated[int | None, Field(gt=0, description="只看某个用户（管理端用户 ID）")] = None,
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """应用的支付订单列表（含退款状态与凭证入口）。需要 payment:read。"""
    return await ORDER_LIST(body=body(
        appid=await app_id_of(app), status=status or None, payment_method=payment_method or None,
        keyword=keyword or None, user_id=user_id, page=page, limit=limit,
    ))


@server.tool(**READ("payment"))
async def order(
    app: AppRef,
    order_no: OrderNo,
    include_refunds: Annotated[bool, Field(description="同时取这笔订单的退款记录")] = False,
    include_refundable: Annotated[bool, Field(description="同时取可退金额与渠道退款能力（发起退款前先看）")] = False,
) -> Any:
    """一笔订单的详情（金额、状态、渠道、履约结果、用户），可附带退款记录与可退额度。需要 payment:read。"""
    app_id = await app_id_of(app)
    req = {"appid": app_id, "order_no": order_no.strip()}
    result: dict[str, Any] = {"order": await ORDER_DETAIL(body=req)}
    if include_refunds:
        result["refunds"] = await REFUND_ORDER(body=req)
    if include_refundable:
        result["refundable"] = await REFUNDABLE(body=req)
    return result


@server.tool(**WRITE("payment"))
async def email_receipt(
    app: AppRef,
    order_no: OrderNo,
    email: Annotated[str, Field(min_length=3, description="收件邮箱（可以是任意地址，操作记入审计）")],
    locale: Annotated[str, Field(description="凭证语言（BCP 47，如 zh-Hans / en / ja），留空用平台默认")] = "",
    document_type: DocumentType = "",
    timezone: Annotated[str, Field(description="凭证上时间的时区（IANA），留空为 UTC")] = "",
) -> Any:
    """把一笔订单的 PDF 凭证寄到指定邮箱（客服代开、对账留档）。邮件发出后无法撤回，先确认收件地址。需要 payment:write。"""
    return await ORDER_RECEIPT_EMAIL(body=body(
        appid=await app_id_of(app), order_no=order_no.strip(), email=email.strip(),
        locale=locale or None, documentType=document_type or None, timezone=timezone or None,
    ))


@server.tool(**WRITE("payment", idempotent=True))
async def receipt_options(app: AppRef) -> Any:
    """凭证能力自述：可选的凭证语言、当前环境是否具备中日韩字体等。指定 locale 前可先看。"""
    return await RECEIPT_OPTIONS(body={"appid": await app_id_of(app)})


# ── 退款 ──

@server.tool(**READ("payment"))
async def refunds(
    app: AppRef,
    status: Annotated[Literal["", "pending", "processing", "success", "failed", "closed"], Field(description="退款状态：pending 待提交 / processing 渠道处理中 / success 成功 / failed 失败 / closed 关闭；空为不限")] = "",
    payment_method: Annotated[str, Field(description="支付渠道，留空不限")] = "",
    keyword: Annotated[str, Field(description="按退款单号 / 订单号搜索")] = "",
    page: Page = 1,
    limit: Limit = 20,
) -> Any:
    """应用的退款单列表。单笔订单的退款记录用 payment_order(include_refunds=true)。需要 payment:read。"""
    return await REFUND_LIST(body=body(
        appid=await app_id_of(app), status=status or None, payment_method=payment_method or None,
        keyword=keyword or None, page=page, limit=limit,
    ))


@server.tool(**DESTRUCTIVE("payment"))
async def refund(
    app: AppRef,
    order_no: OrderNo,
    amount: Annotated[str, Field(description="退款金额（十进制字符串，如 \"9.90\"），留空按剩余可退额度全额退款")] = "",
    reason: Annotated[str, Field(description="退款原因")] = "",
    reverse_fulfillment: Annotated[bool, Field(description="是否回收已发放的权益（余额 / 积分 / 会员时长），默认回收；不回收等于退钱又留货")] = True,
) -> Any:
    """对一笔已支付订单发起退款（向支付渠道提交，钱会真实退回，不可撤销）。

    执行前先用 payment_order(include_refundable=true) 看可退金额与渠道能力，再向用户复述订单号、金额与是否回收权益并取得确认。
    渠道异步处理时退款单状态为 processing，可用 payment_sync_refund 主动同步。需要 payment:write。
    """
    return await REFUND_CREATE(body=body(
        appid=await app_id_of(app), order_no=order_no.strip(), amount=_money(amount) if amount.strip() else None,
        reason=reason or None, reverse_fulfillment=reverse_fulfillment,
    ))


@server.tool(**WRITE("payment", idempotent=True))
async def sync_refund(app: AppRef, refund_no: Annotated[str, Field(min_length=1, description="退款单号")]) -> Any:
    """向支付渠道查询并同步一张退款单的最新状态（用于 processing 状态迟迟不变时）。需要 payment:write。"""
    return await REFUND_SYNC(body={"appid": await app_id_of(app), "refund_no": refund_no.strip()})


# ── 交易设置与概览 ──

@server.tool(**READ("payment"))
async def commerce(
    app: AppRef,
    include_overview: Annotated[bool, Field(description="同时取交易概览：订单、钱包、趋势三口径与凭证能力")] = False,
    start: IsoTime = "",
    end: IsoTime = "",
) -> Any:
    """应用的交易设置（积分兑换率、支付后自动寄凭证、凭证语言、钱包币种），可附带时间窗内的交易概览。"""
    result: dict[str, Any] = {"settings": await COMMERCE(appkey=app)}
    if include_overview:
        result["overview"] = await COMMERCE_OVERVIEW(appkey=app, query=q(start=start, end=end))
    return result


@server.tool(**WRITE("payment"))
async def update_commerce(
    app: AppRef,
    integral_per_currency: Annotated[int | None, Field(ge=1, le=1_000_000, description="每 1 单位金额兑换的积分数（积分充值订单用）")] = None,
    receipt_email_on_paid: Annotated[bool | None, Field(description="支付成功后自动把凭证寄到用户绑定的邮箱")] = None,
    receipt_locale: Annotated[str | None, Field(description="自动寄送凭证的语言（BCP 47），传空串改为按用户设置协商")] = None,
    wallet_currency: Annotated[str | None, Field(min_length=3, max_length=3, description="钱包记账币种（ISO 4217，如 CNY / USD），印在钱包凭证上")] = None,
) -> Any:
    """修改应用交易设置。只传要改的字段，其余保持原值（后端是整体覆盖，这里先读后合并）。改兑换率会影响之后的积分充值订单。"""
    current = await COMMERCE(appkey=app) or {}
    merged = {
        "integralPerCurrency": current.get("integralPerCurrency"),
        "receiptEmailOnPaid": bool(current.get("receiptEmailOnPaid")),
        "receiptLocale": current.get("receiptLocale") or "",
        "walletCurrency": current.get("walletCurrency") or "",
        **body(
            integralPerCurrency=integral_per_currency, receiptEmailOnPaid=receipt_email_on_paid,
            receiptLocale=receipt_locale, walletCurrency=wallet_currency.upper() if wallet_currency else None,
        ),
    }
    if not merged["integralPerCurrency"]:
        merged["integralPerCurrency"] = 100
    return await COMMERCE_UPDATE(appkey=app, body=merged)
