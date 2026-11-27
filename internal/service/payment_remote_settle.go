package service

import (
	"context"
	"sort"
	"strings"
	"time"

	paymentdomain "aegis/internal/domain/payment"

	"go.uber.org/zap"
)

// ── 主动查单补单（remoteOrderSettler）──
//
// 有的渠道推送会丢（爱发电文档原话「不保证能及时推送，建议结合 API 一起使用」）。
// 丢了推送，用户付过的钱就一直挂在「待支付」上，所以这里从两头补：
//
//	用户查单（GET /pay/orders/:orderNo）：待支付订单当场去上游核实一次，节流 15 秒
//	后台每 2 分钟：最近 24 小时内未结的订单按配置分组，指向同一上游账号的配置合并，每组翻一轮上游订单
//
// 核实到的结果走 applyVerifiedResult，与异步通知同一段交叉校验与履约。
// 过期订单也在范围内：用户在收银台磨蹭超过 30 分钟再付款，钱照样到了，单就该认。

const (
	remoteSettleWindow   = 24 * time.Hour
	remoteSettleBatch    = 200
	remoteSettleThrottle = 15 * time.Second
	remoteSettleInterval = 2 * time.Minute
)

// settlerMethods 实现了 remoteOrderSettler 的渠道标识（有序，便于日志与测试比对）
func (s *PaymentService) settlerMethods() []string {
	methods := make([]string, 0)
	for method, p := range s.providers {
		if _, ok := p.(remoteOrderSettler); ok {
			methods = append(methods, method)
		}
	}
	sort.Strings(methods)
	return methods
}

// orderSettleable 订单还值得去上游核实：未支付、没标成待人工处理、在补单窗口内、渠道支持主动查单
func (s *PaymentService) orderSettleable(order *paymentdomain.Order) bool {
	if order == nil || (order.Status != "pending" && order.Status != "expired") {
		return false
	}
	// 已标成待人工处理的（金额对不上）：再查多少次结论都一样，不再查
	if order.ReviewReason != "" {
		return false
	}
	if time.Since(order.CreatedAt) > remoteSettleWindow {
		return false
	}
	p, ok := s.providers[order.PaymentMethod]
	if !ok {
		return false
	}
	_, ok = p.(remoteOrderSettler)
	return ok
}

// settleOrdersWithConfig 用一份支付配置向上游核实一批订单，返回结单数。
func (s *PaymentService) settleOrdersWithConfig(ctx context.Context, config *paymentdomain.Config, orders []*paymentdomain.Order) int {
	provider, err := s.resolveProvider(config.PaymentMethod)
	if err != nil {
		return 0
	}
	settler, ok := provider.(remoteOrderSettler)
	if !ok {
		return 0
	}
	orderNos := make([]string, 0, len(orders))
	for _, order := range orders {
		orderNos = append(orderNos, order.OrderNo)
	}
	results, err := settler.SettleRemoteOrders(ctx, config.ConfigData, orderNos)
	if err != nil {
		// 翻页中途失败时已找到的结果照样处理，只记一条日志
		s.log.Warn("remote order settle failed",
			zap.String("method", config.PaymentMethod), zap.Int64("configId", config.ID), zap.Error(err))
	}
	settled := 0
	for _, order := range orders {
		result, ok := results[order.OrderNo]
		if !ok || result == nil || !result.Paid {
			continue
		}
		if _, err := s.applyVerifiedResult(ctx, order, config.PaymentMethod, "QUERY", "", result); err != nil {
			s.log.Warn("remote order settle apply failed",
				zap.String("orderNo", order.OrderNo), zap.Error(err))
			continue
		}
		settled++
	}
	return settled
}

// onDemandSettleKey 标记「用户自己在查单」的上下文。逐笔查单的渠道（抖音支付）
// 对后台补单做了退避，但用户刚付完款回来查的那一下不能被退避挡住。
type onDemandSettleKey struct{}

func withOnDemandSettle(ctx context.Context) context.Context {
	return context.WithValue(ctx, onDemandSettleKey{}, true)
}

func isOnDemandSettle(ctx context.Context) bool {
	v, _ := ctx.Value(onDemandSettleKey{}).(bool)
	return v
}

// settleOrderOnDemand 用户查单时的即时核实。返回 true 表示订单已在本次被确认支付。
func (s *PaymentService) settleOrderOnDemand(ctx context.Context, order *paymentdomain.Order, throttle bool) bool {
	if !s.orderSettleable(order) {
		return false
	}
	if throttle {
		now := time.Now()
		if last, ok := s.settleAttempts.Load(order.OrderNo); ok && now.Sub(last.(time.Time)) < remoteSettleThrottle {
			return false
		}
		s.settleAttempts.Store(order.OrderNo, now)
	}
	config, err := s.pg.GetPaymentConfigByID(ctx, order.AppID, order.ConfigID)
	if err != nil || config == nil {
		return false
	}
	return s.settleOrdersWithConfig(withOnDemandSettle(ctx), config, []*paymentdomain.Order{order}) > 0
}

// syncUnsettledOrders 后台补单：最近未结的订单按支付配置分组，每份配置向上游查一轮。
func (s *PaymentService) syncUnsettledOrders() {
	methods := s.settlerMethods()
	if len(methods) == 0 || s.pg == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	orders, err := s.pg.ListUnsettledPaymentOrders(ctx, methods, time.Now().Add(-remoteSettleWindow), remoteSettleBatch)
	if err != nil {
		s.log.Warn("list unsettled payment orders failed", zap.Error(err))
		return
	}
	// 先按支付配置分组，再把指向同一上游账号的配置并成一轮（多个应用共用一个爱发电账号时，
	// 每个应用各翻一遍最近订单只是重复消耗接口频次，结果完全一样）。
	type configKey struct{ appID, configID int64 }
	byConfig := map[configKey][]*paymentdomain.Order{}
	configKeys := make([]configKey, 0)
	for i := range orders {
		key := configKey{orders[i].AppID, orders[i].ConfigID}
		if _, ok := byConfig[key]; !ok {
			configKeys = append(configKeys, key)
		}
		byConfig[key] = append(byConfig[key], &orders[i])
	}
	type settleGroup struct {
		config *paymentdomain.Config
		orders []*paymentdomain.Order
	}
	groups := make([]*settleGroup, 0, len(configKeys))
	byScope := map[string]*settleGroup{}
	for _, key := range configKeys {
		config, err := s.pg.GetPaymentConfigByID(ctx, key.appID, key.configID)
		if err != nil || config == nil {
			continue
		}
		scope := ""
		if p, ok := s.providers[config.PaymentMethod]; ok {
			if scoper, ok := p.(remoteSettleScoper); ok {
				if sc := scoper.SettleScope(config.ConfigData); sc != "" {
					scope = config.PaymentMethod + "|" + sc
				}
			}
		}
		if scope != "" {
			if g, ok := byScope[scope]; ok {
				g.orders = append(g.orders, byConfig[key]...)
				continue
			}
		}
		g := &settleGroup{config: config, orders: byConfig[key]}
		groups = append(groups, g)
		if scope != "" {
			byScope[scope] = g
		}
	}
	total := 0
	for _, g := range groups {
		total += s.settleOrdersWithConfig(ctx, g.config, g.orders)
	}
	if total > 0 {
		s.log.Info("settled payment orders from upstream", zap.Int("count", total))
	}
	// 节流表只需要记住窗口内的订单，顺手清掉过期项，免得常驻进程里越攒越多
	s.settleAttempts.Range(func(key, value any) bool {
		if time.Since(value.(time.Time)) > remoteSettleThrottle {
			s.settleAttempts.Delete(key)
		}
		return true
	})
}

// CallbackAck 渠道自定义的回调应答。ok=false 表示渠道没有要求，按通用的 success/fail 文本应答。
func (s *PaymentService) CallbackAck(method string, result *paymentdomain.CallbackResult, err error) ([]byte, bool) {
	p, ok := s.providers[strings.TrimSpace(method)]
	if !ok {
		return nil, false
	}
	acker, ok := p.(callbackAcknowledger)
	if !ok {
		return nil, false
	}
	return acker.AckCallback(result, err), true
}
