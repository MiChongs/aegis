package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
)

// ── 爱发电 Provider ──
//
// 爱发电不是收单网关，没有「商户下单」接口。能用的只有三样东西：
//
//	下单链接：/order/create 带上 custom_order_id，爱发电会把它原样带回订单里
//	Webhook：创作者后台填一个地址，每笔成交推一次，报文**没有签名**
//	开放 API：用 user_id + token 签名查订单（query-order）、查赞助者（query-sponsor）
//
// 所以这条渠道的安全模型是「Webhook 只当提醒」：收到推送后拿报文里的爱发电单号
// 回查 query-order，以 API 返回的金额、状态与 custom_order_id 为准。伪造的推送
// 查不到对应订单，篡改过的推送查到的是真实数据，都骗不过去。
//
// Webhook 推送不保证送达（官方文档原话），因此还实现了 remoteOrderSettler：
// 用户查单与后台定时任务会按 custom_order_id 翻最近几页订单，主动补上漏掉的推送。
//
// 文档：https://guide.afdian.com/creator/developer

type afdianProvider struct {
	client *resty.Client
}

func newAfdianProvider(client *resty.Client) *afdianProvider {
	return &afdianProvider{client: client}
}

const (
	afdianDefaultSite  = "https://afdian.com"
	afdianOrdersPage   = 50 // query-order 每页条数（官方固定）
	afdianDefaultPages = 3
	afdianMaxPages     = 20

	afdianModeCustom = "custom" // 自选金额：custom_price 填订单金额
	afdianModePlan   = "plan"   // 赞助方案：按金额映射到方案 plan_id

	afdianStatusPaid = 2 // 交易成功（目前爱发电只推送这一种）
)

func (p *afdianProvider) Name() string { return paymentdomain.MethodAfdian }

// PayerAdjustsAmount 爱发电的自选金额付款页上付款人可以改金额（2026-10 至 11 月线上 8 笔永久会员订单被改成了 50.00 / 49.00）：
// 多付按已支付处理，少付标成待人工处理（payment_review.go）。方案模式下能不能改月数未实测，多付同样按已支付处理。
func (p *afdianProvider) PayerAdjustsAmount() bool { return true }

func (p *afdianProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodAfdian,
		Name:         "爱发电",
		Description:  "创作者赞助平台。用户跳转爱发电付款，按自定义订单号对账，无需企业资质。",
		Category:     paymentdomain.CategoryAggregate,
		Icon:         "afdian",
		BrandColor:   "#946CE6",
		DocURL:       "https://guide.afdian.com/creator/developer",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodAfdian,
		CallbackNote: "把此地址填到爱发电开发者后台（afdian.com/dashboard/dev）的 Webhook 地址。推送不带签名，" +
			"平台收到后会用 API Token 回查订单核实；推送丢失时由主动查单补齐。",
		Regions:    []string{"中国大陆"},
		Currencies: []string{"CNY"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, Webhook: true, RemoteQuery: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("alipay", "支付宝", "在爱发电收银台内选择"),
			payType("wxpay", "微信支付", "在爱发电收银台内选择"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fText("userId", "创作者 user_id", "32 位十六进制", "开发者后台显示的 user_id", true),
				fSecret("token", "API Token", "开发者后台生成的 Token", "只参与签名，不会发送给爱发电", true),
			),
			inGroup(paymentdomain.GroupGateway,
				fSelect("orderMode", "下单方式", "自选金额直接按订单金额付款；赞助方案需按金额映射到已建好的方案", afdianModeCustom,
					opt(afdianModeCustom, "自选金额"),
					opt(afdianModePlan, "赞助方案"),
				),
				fArea("planMap", "方案映射", "29.90=a45353328af911eb973052540025c377",
					"赞助方案模式使用。每行一条「金额=方案 ID」，下单金额必须与方案单月价格一致", false),
				fSelect("siteUrl", "爱发电域名", "下单链接与开放 API 使用的域名", afdianDefaultSite,
					opt("https://afdian.com", "afdian.com"),
					opt("https://ifdian.net", "ifdian.net"),
				),
			),
			limitFields("5", "50000"),
			advanced(inGroup(paymentdomain.GroupAdvanced,
				fNum("scanPages", "主动查单页数", "3", "主动查单时最多翻阅的订单页数（每页 50 条，最多 20 页）", afdianDefaultPages),
			)...),
		),
	})
}

// ── 配置 ──

type afdianConfig struct {
	UserID    string `json:"userId"`
	Token     string `json:"token"`
	OrderMode string `json:"orderMode"`
	PlanMap   string `json:"planMap"`
	SiteURL   string `json:"siteUrl"`
}

func decodeAfdianConfig(data map[string]any) (*afdianConfig, error) {
	cfg, err := decodeProviderConfig[afdianConfig](data)
	if err != nil {
		return nil, err
	}
	cfg.UserID = strings.TrimSpace(cfg.UserID)
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.OrderMode = strings.TrimSpace(cfg.OrderMode)
	if cfg.OrderMode == "" {
		cfg.OrderMode = afdianModeCustom
	}
	cfg.SiteURL = strings.TrimRight(pickString(strings.TrimSpace(cfg.SiteURL), afdianDefaultSite), "/")
	return cfg, nil
}

// afdianScanPages 主动查单的翻页上限。配置是 number 字段，JSON 解出来可能是任何数值形态。
func afdianScanPages(data map[string]any) int {
	pages := int(configFloat(data, "scanPages"))
	if pages <= 0 {
		return afdianDefaultPages
	}
	return min(pages, afdianMaxPages)
}

// parseAfdianPlanMap 解析「金额=方案 ID」映射。也接受中文等号与冒号，空行与 # 注释跳过。
func parseAfdianPlanMap(raw string) (map[string]string, error) {
	out := map[string]string{}
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sep := strings.IndexAny(line, "=＝:：")
		if sep <= 0 {
			return nil, fmt.Errorf("第 %d 行缺少「=」", i+1)
		}
		amountRaw := strings.TrimSpace(line[:sep])
		planID := strings.TrimSpace(strings.TrimLeft(line[sep:], "=＝:："))
		amount, err := decimal.NewFromString(amountRaw)
		if err != nil || !amount.IsPositive() {
			return nil, fmt.Errorf("第 %d 行金额无效：%s", i+1, amountRaw)
		}
		if planID == "" {
			return nil, fmt.Errorf("第 %d 行缺少方案 ID", i+1)
		}
		out[amount.StringFixed(2)] = planID
	}
	return out, nil
}

func (p *afdianProvider) ValidateConfig(data map[string]any) error {
	cfg, err := decodeAfdianConfig(data)
	if err != nil {
		return err
	}
	if cfg.UserID == "" || cfg.Token == "" {
		return apperrors.New(40078, http.StatusBadRequest, "爱发电配置不完整：缺少 user_id 或 API Token")
	}
	switch cfg.OrderMode {
	case afdianModeCustom:
	case afdianModePlan:
		plans, err := parseAfdianPlanMap(cfg.PlanMap)
		if err != nil {
			return apperrors.New(40078, http.StatusBadRequest, "爱发电方案映射有误："+err.Error())
		}
		if len(plans) == 0 {
			return apperrors.New(40078, http.StatusBadRequest, "爱发电赞助方案模式至少需要一条方案映射")
		}
	default:
		return apperrors.New(40078, http.StatusBadRequest, "爱发电下单方式无效："+cfg.OrderMode)
	}
	if u, err := url.Parse(cfg.SiteURL); err != nil || u.Scheme != "https" || u.Host == "" {
		return apperrors.New(40078, http.StatusBadRequest, "爱发电域名必须是 https 地址")
	}
	return nil
}

// ── 开放 API ──

// afdianSign 爱发电开放 API 签名：md5(token + "params" + params + "ts" + ts + "user_id" + user_id)。
// 参数键固定为这三个，按键名排序后恰好是这个顺序，token 只参与计算不发送。
func afdianSign(token, params string, ts int64, userID string) string {
	sum := md5.Sum([]byte(token + "params" + params + "ts" + strconv.FormatInt(ts, 10) + "user_id" + userID))
	return hex.EncodeToString(sum[:])
}

// afdianEnvelope 开放 API 与 Webhook 共用的外层结构
type afdianEnvelope struct {
	EC   int             `json:"ec"`
	EM   string          `json:"em"`
	Data json.RawMessage `json:"data"`
}

// afdianNumber 爱发电的数值字段在不同接口里有时是数字、有时是字符串，统一按文本收下。
type afdianNumber string

func (n *afdianNumber) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*n = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*n = afdianNumber(strings.TrimSpace(s))
		return nil
	}
	*n = afdianNumber(string(b))
	return nil
}

func (n afdianNumber) Int() int {
	v, _ := strconv.Atoi(strings.TrimSpace(string(n)))
	return v
}

func (n afdianNumber) Decimal() decimal.Decimal {
	d, err := decimal.NewFromString(strings.TrimSpace(string(n)))
	if err != nil {
		return decimal.Zero
	}
	return d
}

// afdianOrder 订单字段（见文档「订单字段说明」；custom_order_id 是下单链接带过去的那个值）
type afdianOrder struct {
	OutTradeNo    string           `json:"out_trade_no"`
	CustomOrderID string           `json:"custom_order_id"`
	UserID        string           `json:"user_id"`
	UserPrivateID string           `json:"user_private_id"`
	PlanID        string           `json:"plan_id"`
	Title         string           `json:"title"`
	Month         afdianNumber     `json:"month"`
	TotalAmount   afdianNumber     `json:"total_amount"`
	ShowAmount    afdianNumber     `json:"show_amount"`
	Status        afdianNumber     `json:"status"`
	Remark        string           `json:"remark"`
	RedeemID      string           `json:"redeem_id"`
	ProductType   afdianNumber     `json:"product_type"`
	Discount      afdianNumber     `json:"discount"`
	SkuDetail     []map[string]any `json:"sku_detail"`
}

type afdianOrderPage struct {
	List       []json.RawMessage `json:"list"`
	TotalCount int               `json:"total_count"`
	TotalPage  int               `json:"total_page"`
}

// afdianEmptyParams 爱发电把空对象 "{}" 判为非法 params（ec=400003 params was not valid json string），
// 不需要参数的接口（如 ping）只能塞一个无意义字段，官方样例用的就是 {"a":333}。
const afdianEmptyParams = `{"a":333}`

// afdianCall 调用开放 API，ec 不为 200 一律视为失败
func (p *afdianProvider) afdianCall(ctx context.Context, cfg *afdianConfig, endpoint string, params any) (json.RawMessage, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	if s := string(paramsJSON); s == "{}" || s == "null" {
		paramsJSON = []byte(afdianEmptyParams)
	}
	ts := time.Now().Unix()
	body := map[string]any{
		"user_id": cfg.UserID,
		"params":  string(paramsJSON),
		"ts":      ts,
		"sign":    afdianSign(cfg.Token, string(paramsJSON), ts, cfg.UserID),
	}
	resp, err := p.client.R().SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		Post(cfg.SiteURL + "/api/open/" + endpoint)
	if err != nil {
		return nil, apperrors.New(50084, http.StatusBadGateway, "爱发电接口请求失败："+err.Error())
	}
	var env afdianEnvelope
	if err := json.Unmarshal(resp.Body(), &env); err != nil {
		return nil, apperrors.New(50084, http.StatusBadGateway, fmt.Sprintf("爱发电接口响应无法解析（HTTP %d）", resp.StatusCode()))
	}
	if env.EC != 200 {
		return nil, apperrors.New(50084, http.StatusBadGateway, fmt.Sprintf("爱发电接口返回 %d：%s", env.EC, env.EM))
	}
	return env.Data, nil
}

// queryOrders 拉一页订单。保留每条订单的原始 JSON，回调日志里存的是爱发电给的原样数据。
func (p *afdianProvider) queryOrders(ctx context.Context, cfg *afdianConfig, params map[string]any) ([]afdianOrder, []map[string]any, *afdianOrderPage, error) {
	data, err := p.afdianCall(ctx, cfg, "query-order", params)
	if err != nil {
		return nil, nil, nil, err
	}
	var page afdianOrderPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, nil, nil, apperrors.New(50084, http.StatusBadGateway, "爱发电订单列表解析失败")
	}
	orders := make([]afdianOrder, 0, len(page.List))
	raws := make([]map[string]any, 0, len(page.List))
	for _, item := range page.List {
		var order afdianOrder
		if err := json.Unmarshal(item, &order); err != nil {
			continue
		}
		var raw map[string]any
		_ = json.Unmarshal(item, &raw)
		orders = append(orders, order)
		raws = append(raws, raw)
	}
	return orders, raws, &page, nil
}

// findByCustomOrderID 从最新订单往前翻，找出 custom_order_id 落在 wanted 里的订单。
// 每个订单号都找到成交单、翻到最后一页或达到页数上限即停。
func (p *afdianProvider) findByCustomOrderID(ctx context.Context, cfg *afdianConfig, wanted map[string]bool, maxPages int) (map[string]afdianOrder, map[string]map[string]any, error) {
	found := map[string]afdianOrder{}
	raws := map[string]map[string]any{}
	paid := 0
	for page := 1; page <= maxPages && paid < len(wanted); page++ {
		orders, rawList, meta, err := p.queryOrders(ctx, cfg, map[string]any{"page": page})
		if err != nil {
			return found, raws, err
		}
		for i, order := range orders {
			id := strings.TrimSpace(order.CustomOrderID)
			if !wanted[id] {
				continue
			}
			// 同一个订单号可能对应多笔爱发电订单（用户重复付款、先用兑换码再付钱）。
			// 列表按时间倒序：已有成交单就不再替换，否则用这一笔顶上。
			if prev, ok := found[id]; ok && afdianOrderPaid(prev) {
				continue
			}
			found[id] = order
			raws[id] = rawList[i]
			if afdianOrderPaid(order) {
				paid++
			}
		}
		if meta.TotalPage <= page || len(orders) < afdianOrdersPage {
			break
		}
	}
	return found, raws, nil
}

// afdianOrderPaid 交易成功且真金白银付了钱（兑换码订单实付 0.00）
func afdianOrderPaid(order afdianOrder) bool {
	return order.Status.Int() == afdianStatusPaid && order.TotalAmount.Decimal().IsPositive()
}

// afdianResult 把一笔爱发电订单转成回调结果。
//
// 兑换码订单的实付金额是 0.00：网关层的金额校验只在金额为正时生效，
// 让它带着 Paid=true 过去就成了「零元购」，所以这里直接判为未支付。
func afdianResult(order afdianOrder, raw map[string]any, source string) *paymentdomain.CallbackResult {
	amount := order.TotalAmount.Decimal()
	status := order.Status.Int()
	tradeStatus := "STATUS_" + strconv.Itoa(status)
	paid := afdianOrderPaid(order)
	switch {
	case paid:
		tradeStatus = "TRADE_SUCCESS"
	case status == afdianStatusPaid:
		tradeStatus = "REDEEMED"
	}
	rawData := map[string]any{"source": source}
	if raw != nil {
		rawData["order"] = raw
	}
	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         strings.TrimSpace(order.CustomOrderID),
		ProviderOrderNo: order.OutTradeNo,
		TradeStatus:     tradeStatus,
		PaymentMethod:   paymentdomain.MethodAfdian,
		Amount:          amount,
		RawData:         rawData,
	}
}

func (p *afdianProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := decodeAfdianConfig(data)
	if err != nil {
		return nil, err
	}
	if cfg.UserID == "" || cfg.Token == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "爱发电配置不完整：缺少 user_id 或 API Token")
	}
	// ping 只校验签名；顺带取两页统计，确认 Token 对订单与赞助者接口都有权限
	if _, err := p.afdianCall(ctx, cfg, "ping", nil); err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	result := map[string]any{"config_valid": true, "api_accessible": true}
	if _, _, page, err := p.queryOrders(ctx, cfg, map[string]any{"page": 1}); err == nil {
		result["order_count"] = page.TotalCount
	} else {
		result["order_error"] = err.Error()
	}
	if raw, err := p.afdianCall(ctx, cfg, "query-sponsor", map[string]any{"page": 1}); err == nil {
		var sponsors struct {
			TotalCount int `json:"total_count"`
		}
		_ = json.Unmarshal(raw, &sponsors)
		result["sponsor_count"] = sponsors.TotalCount
	} else {
		result["sponsor_error"] = err.Error()
	}
	return result, nil
}

// CreateOrder 拼爱发电下单链接。
//
// 本地订单号放进 custom_order_id，爱发电会在推送与查单结果里原样带回，
// 这是对账的唯一依据。remark 也写上订单号：用户若在页面上改掉了链接参数，
// 创作者后台的留言里还能看到这笔钱是为哪张单付的。
func (p *afdianProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := decodeAfdianConfig(data)
	if err != nil {
		return nil, err
	}
	if cfg.UserID == "" || cfg.Token == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "爱发电配置不完整：缺少 user_id 或 API Token")
	}
	query := url.Values{}
	switch cfg.OrderMode {
	case afdianModePlan:
		plans, err := parseAfdianPlanMap(cfg.PlanMap)
		if err != nil {
			return nil, apperrors.New(40078, http.StatusBadRequest, "爱发电方案映射有误："+err.Error())
		}
		planID, ok := plans[req.Amount.StringFixed(2)]
		if !ok {
			return nil, apperrors.New(40117, http.StatusBadRequest,
				fmt.Sprintf("爱发电没有价格为 %s 的赞助方案", req.Amount.StringFixed(2)))
		}
		query.Set("plan_id", planID)
		query.Set("product_type", "0")
	default:
		query.Set("user_id", cfg.UserID)
		query.Set("custom_price", req.Amount.StringFixed(2))
	}
	query.Set("month", "1")
	query.Set("custom_order_id", req.OrderNo)
	query.Set("remark", "订单 "+req.OrderNo)

	payURL := cfg.SiteURL + "/order/create?" + query.Encode()
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   payURL,
		RedirectURL:  payURL,
		ProviderType: req.ProviderType,
	}, nil
}

// QueryRemoteOrder 爱发电查单只认它自己的单号，所以按 custom_order_id 翻最近几页。
func (p *afdianProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := decodeAfdianConfig(data)
	if err != nil {
		return nil, err
	}
	orderNo = strings.TrimSpace(orderNo)
	found, raws, err := p.findByCustomOrderID(ctx, cfg, map[string]bool{orderNo: true}, afdianScanPages(data))
	if err != nil {
		return nil, err
	}
	order, ok := found[orderNo]
	if !ok {
		return map[string]any{"found": false, "order_no": orderNo}, nil
	}
	result := afdianResult(order, raws[orderNo], "query")
	return map[string]any{
		"found":             true,
		"order_no":          orderNo,
		"paid":              result.Paid,
		"trade_status":      result.TradeStatus,
		"provider_order_no": order.OutTradeNo,
		"total_amount":      result.Amount.StringFixed(2),
		"order":             raws[orderNo],
	}, nil
}

// afdianWebhookOrder 从推送报文里取出订单对象（data.type 目前只有 order）
func afdianWebhookOrder(rawBody string) (*afdianOrder, error) {
	var env afdianEnvelope
	if err := json.Unmarshal([]byte(rawBody), &env); err != nil {
		return nil, err
	}
	var payload struct {
		Type  string      `json:"type"`
		Order afdianOrder `json:"order"`
	}
	if err := json.Unmarshal(env.Data, &payload); err != nil {
		return nil, err
	}
	if payload.Type != "" && payload.Type != "order" {
		return nil, fmt.Errorf("不支持的推送类型 %s", payload.Type)
	}
	return &payload.Order, nil
}

// ExtractOrderNo Webhook 地址是创作者级的，订单号只能从报文的 custom_order_id 取。
// 爱发电后台的「发送测试」与不经本平台下单的普通赞助都没有这个字段，返回空串，
// 由网关按「缺少订单号」处理，再经 AckCallback 回 ec=200，免得爱发电判定推送失败。
func (p *afdianProvider) ExtractOrderNo(callbackData map[string]string) string {
	order, err := afdianWebhookOrder(callbackData[CallbackRawBodyKey])
	if err != nil {
		return ""
	}
	return strings.TrimSpace(order.CustomOrderID)
}

// HandleCallback 推送报文不带签名，不能当真：用报文里的爱发电单号回查 query-order，
// 以接口返回的订单为准。查不到就是伪造，custom_order_id 对不上就是被改过。
func (p *afdianProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := decodeAfdianConfig(data)
	if err != nil {
		return nil, err
	}
	rawBody := callbackData[CallbackRawBodyKey]
	if strings.TrimSpace(rawBody) == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "爱发电推送缺少报文")
	}
	pushed, err := afdianWebhookOrder(rawBody)
	if err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "爱发电推送解析失败")
	}
	outTradeNo := strings.TrimSpace(pushed.OutTradeNo)
	if outTradeNo == "" {
		return nil, apperrors.New(40076, http.StatusBadRequest, "爱发电推送缺少订单号")
	}
	orders, raws, _, err := p.queryOrders(ctx, cfg, map[string]any{"out_trade_no": outTradeNo})
	if err != nil {
		// 回查失败是暂时的（网络、限流），返回 5xx 让爱发电按失败处理，之后由主动查单补上
		return nil, err
	}
	for i, order := range orders {
		if order.OutTradeNo != outTradeNo {
			continue
		}
		if strings.TrimSpace(order.CustomOrderID) != strings.TrimSpace(pushed.CustomOrderID) {
			return nil, apperrors.New(40076, http.StatusBadRequest, "爱发电推送与查单结果不一致")
		}
		return afdianResult(order, raws[i], "webhook"), nil
	}
	return nil, apperrors.New(40076, http.StatusBadRequest, "爱发电查无此订单："+outTradeNo)
}

// SettleRemoteOrders 实现 remoteOrderSettler：按 custom_order_id 在最近的订单里找这批本地订单。
func (p *afdianProvider) SettleRemoteOrders(ctx context.Context, data map[string]any, orderNos []string) (map[string]*paymentdomain.CallbackResult, error) {
	cfg, err := decodeAfdianConfig(data)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(orderNos))
	for _, no := range orderNos {
		if no = strings.TrimSpace(no); no != "" {
			wanted[no] = true
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	found, raws, err := p.findByCustomOrderID(ctx, cfg, wanted, afdianScanPages(data))
	out := make(map[string]*paymentdomain.CallbackResult, len(found))
	for no, order := range found {
		out[no] = afdianResult(order, raws[no], "query")
	}
	return out, err
}

// AckCallback 爱发电只认 {"ec":200}，其余一律算推送失败。
//
// 4xx（不是本平台的单、查无此单、金额不符）重推多少次结果都一样，照样回 200，
// 原因写进 em；只有 5xx（回查爱发电失败、履约失败）才回非 200，留给爱发电重推。
func (p *afdianProvider) AckCallback(result *paymentdomain.CallbackResult, err error) []byte {
	ec, em := 200, ""
	if err != nil {
		em = err.Error()
		if appErr, ok := errors.AsType[*apperrors.AppError](err); !ok || appErr.HTTPStatus >= 500 {
			ec = 500
		}
	}
	body, _ := json.Marshal(map[string]any{"ec": ec, "em": em})
	return body
}
