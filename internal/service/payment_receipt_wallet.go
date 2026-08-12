package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	userdomain "aegis/internal/domain/user"
	vipdomain "aegis/internal/domain/vip"
	walletdomain "aegis/internal/domain/wallet"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/receipt"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// 钱包流水凭证。
//
// 支付订单与钱包流水是平台上**两条并行的资金记录**，此前只有前者能出凭证。
// 结果是：余额直购会员、业务消费、管理员调账都只落 wallet_transactions，
// 用户手里只有一行流水，拿不到任何可归档、可报销、可对账的文件 ——
// 而这几条恰好是「用钱包付的钱」的全部形态。
//
// 补齐的方式不是再造一套凭证引擎，而是让钱包流水成为凭证的第二类主体，
// 与订单共用 pkg/receipt 的同一份排版、同一份译文与同一套导出/寄送/清理链路。
//
// 一条铁律贯穿本文件：**同一笔钱只出一份凭证**。
// 流水挂着 related_order_no 时（充值到账、余额支付订单），凭证由**订单**出具，
// 钱包这边只是把同一份文档再交付一次。否则同一笔交易会有两个凭证编号，
// 对账时无从判断哪个算数。

// ── 用户侧 ──

// RenderUserWalletReceipt 直接返回钱包流水凭证的 PDF 字节，不落盘。
func (s *PaymentService) RenderUserWalletReceipt(ctx context.Context, session *authdomain.Session, transactionNo string, opts paymentdomain.ReceiptOptions) ([]byte, string, error) {
	if session == nil {
		return nil, "", apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	doc, result, err := s.renderUserWalletReceipt(ctx, session, transactionNo, opts)
	if err != nil {
		return nil, "", err
	}
	return result.PDF, receiptFileName(doc.Type, receiptSubjectNo(doc), result.Locale), nil
}

// CreateUserWalletReceipt 生成钱包流水凭证并落盘，返回一次性下载凭据。
func (s *PaymentService) CreateUserWalletReceipt(ctx context.Context, session *authdomain.Session, transactionNo string, opts paymentdomain.ReceiptOptions) (*paymentdomain.ReceiptExport, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	txn, err := s.requireUserWalletTransaction(ctx, session, transactionNo)
	if err != nil {
		return nil, err
	}
	doc, result, err := s.renderWalletReceiptFor(ctx, txn, session.UserID, opts)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	export := paymentdomain.ReceiptExport{
		BillID:          randomReceiptID(16),
		OrderNo:         doc.OrderNo,
		TransactionNo:   txn.TransactionNo,
		FileName:        receiptFileName(doc.Type, receiptSubjectNo(doc), result.Locale),
		DocumentType:    string(doc.Type),
		Locale:          result.Locale,
		RequestedLocale: result.RequestedLocale,
		LocaleFallback:  result.LocaleFallback,
		DegradedGlyphs:  string(result.MissingGlyphs),
		Currency:        doc.Currency,
		Pages:           result.Pages,
		Size:            len(result.PDF),
		CreatedAt:       now,
		ExpiresAt:       now.Add(s.resolveReceiptTTL(opts.TTL)),
	}
	// 下载走与订单凭证同一条路由：那条路由校验的是 (appid, userId, billId)，
	// 与凭证主体无关，另开一条只会多一处要同步的权限判定。
	export.DownloadURL = fmt.Sprintf("/api/pay/bills/%s/download", export.BillID)
	if err := s.persistReceipt(session.AppID, session.UserID, export, result.PDF); err != nil {
		return nil, err
	}
	return &export, nil
}

// EmailUserWalletReceipt 用户自助把钱包流水凭证寄到账号绑定的邮箱。
// 收件地址同样不接受调用方指定，理由见 EmailUserOrderReceipt。
func (s *PaymentService) EmailUserWalletReceipt(ctx context.Context, session *authdomain.Session, transactionNo string, opts paymentdomain.ReceiptOptions) (*ReceiptEmailResult, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	txn, err := s.requireUserWalletTransaction(ctx, session, transactionNo)
	if err != nil {
		return nil, err
	}
	user, profile, err := s.loadWalletCustomer(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	to := ""
	if profile != nil {
		to = strings.TrimSpace(profile.Email)
	}
	if to == "" {
		return nil, apperrors.New(40097, http.StatusBadRequest, "账号尚未绑定邮箱，请先在个人资料中绑定后再补发凭证")
	}
	if err := s.ensureReceiptEmailQuota(ctx, session.AppID, to); err != nil {
		return nil, err
	}
	return s.deliverWalletReceiptEmail(ctx, txn, user, profile, to, opts, s.loadUserUISettings(ctx, session.UserID))
}

// ── 管理端 ──

// RenderAppWalletReceipt 管理端按应用维度导出任意流水的凭证（不落盘）。
func (s *PaymentService) RenderAppWalletReceipt(ctx context.Context, appID int64, transactionNo string, opts paymentdomain.ReceiptOptions) ([]byte, string, error) {
	txn, err := s.requireAppWalletTransaction(ctx, appID, transactionNo)
	if err != nil {
		return nil, "", err
	}
	doc, result, err := s.renderWalletReceiptFor(ctx, txn, txn.UserID, opts)
	if err != nil {
		return nil, "", err
	}
	return result.PDF, receiptFileName(doc.Type, receiptSubjectNo(doc), result.Locale), nil
}

// EmailAppWalletReceipt 管理端把钱包流水凭证寄到指定邮箱（客服代发 / 补发到财务邮箱）。
func (s *PaymentService) EmailAppWalletReceipt(ctx context.Context, appID int64, transactionNo string, to string, opts paymentdomain.ReceiptOptions) (*ReceiptEmailResult, error) {
	to = strings.TrimSpace(to)
	if !isEmailAddress(to) {
		return nil, apperrors.New(40098, http.StatusBadRequest, "收件邮箱格式错误")
	}
	txn, err := s.requireAppWalletTransaction(ctx, appID, transactionNo)
	if err != nil {
		return nil, err
	}
	user, profile, err := s.loadWalletCustomer(ctx, appID, txn.UserID)
	if err != nil {
		return nil, err
	}
	return s.deliverWalletReceiptEmail(ctx, txn, user, profile, to, opts, nil)
}

// autoEmailWalletReceipt 钱包购买成功后自动把凭证寄给用户。
//
// 与订单支付的自动寄送共用同一个应用级开关（receiptEmailOnPaid）：
// 「买东西会不会收到收据」不该因为付款用的是余额还是支付宝而不同。
//
// 只挂在**购买**类流水上（余额直购会员），不挂消费与调账：
// consume 是应用自定义的业务扣费，一天可能发生几百次，每次寄一封信
// 会先把邮件出口的配额烧光、再把用户的收件箱埋掉。
func (s *PaymentService) autoEmailWalletReceipt(appID int64, transactionNo string) {
	if s.email == nil || s.apps == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	app, err := s.apps.GetApp(ctx, appID)
	if err != nil || app == nil {
		return
	}
	// 先看开关再取流水：绝大多数应用没开这个开关，不该为此多打一次库
	if !s.apps.ResolveCommerceSettings(app).ReceiptEmailOnPaid {
		return
	}
	txn, err := s.pg.GetWalletTransactionByNo(ctx, appID, transactionNo)
	if err != nil || txn == nil {
		return
	}
	user, profile, err := s.loadWalletCustomer(ctx, appID, txn.UserID)
	if err != nil {
		s.log.Warn("auto wallet receipt email: load customer failed",
			zap.String("transactionNo", transactionNo), zap.Error(err))
		return
	}
	to := ""
	if profile != nil {
		to = strings.TrimSpace(profile.Email)
	}
	if to == "" {
		// 没绑邮箱不是错误，只是这条链路无处可送
		s.log.Info("auto wallet receipt email skipped: customer has no email",
			zap.String("transactionNo", transactionNo))
		return
	}
	settings := s.apps.ResolveCommerceSettings(app)
	result, err := s.deliverWalletReceiptEmail(ctx, txn, user, profile, to,
		paymentdomain.ReceiptOptions{Locale: settings.ReceiptLocale}, s.loadUserUISettings(ctx, txn.UserID))
	if err != nil {
		s.log.Warn("auto wallet receipt email failed",
			zap.String("transactionNo", transactionNo), zap.String("to", maskEmail(to)), zap.Error(err))
		return
	}
	s.log.Info("auto wallet receipt email sent",
		zap.String("transactionNo", transactionNo), zap.String("to", maskEmail(to)),
		zap.String("locale", result.Locale), zap.Bool("attached", result.Attached))
}

// ── 流水上的凭证入口 ──

// BuildWalletTransactionReceipt 算出挂在一条流水上的凭证入口。
//
// **不查订单表**：列表一页 20 行，逐行去确认关联订单是否还在，就是 20 次查库。
// 出具方按 related_order_no 是否存在推导即可 —— 真正的委派判定在导出时做，
// 而下载入口恒指向钱包这条路由，因此即便关联订单被清理了按钮也不会失效。
func (s *PaymentService) BuildWalletTransactionReceipt(txn *walletdomain.Transaction, info orderReceiptContext) walletdomain.TransactionReceipt {
	if s.receipts == nil || txn == nil {
		return walletdomain.TransactionReceipt{Available: false, EmailHint: "凭证渲染器不可用"}
	}
	entry := walletdomain.TransactionReceipt{
		Available:    true,
		Source:       walletdomain.ReceiptSourceWallet,
		DocumentType: string(resolveWalletReceiptDocType(txn, "")),
		Locale:       info.locale,
		Currency:     info.walletCurrency,
		DownloadURL:  walletReceiptDownloadURL(txn.TransactionNo),
		ExportURL:    walletReceiptExportURL(txn.TransactionNo),
		EmailURL:     walletReceiptEmailURL(txn.TransactionNo),
		Emailable:    info.emailable,
		EmailHint:    info.emailHint,
	}
	if orderNo := strings.TrimSpace(txn.RelatedOrderNo); orderNo != "" {
		entry.Source = walletdomain.ReceiptSourceOrder
		entry.OrderNo = orderNo
		// 由订单出具时，凭证类型也随订单状态走（未支付的充值单出的是账单不是收据）
		entry.DocumentType = ""
	}
	return entry
}

// BuildVipPurchaseReceipt 余额直购会员的凭证入口。
//
// 会员直购不产生支付订单，扣款流水就是这笔购买唯一的资金凭据，
// 因此凭证入口与钱包流水完全一致 —— 三条地址由同一组辅助函数拼出，
// 不在两处各拼一遍：路由改了只会改一处，另一处会静默指向 404。
func (s *PaymentService) BuildVipPurchaseReceipt(ctx context.Context, session *authdomain.Session, transactionNo string) *vipdomain.ReceiptEntry {
	transactionNo = strings.TrimSpace(transactionNo)
	// 0 元套餐不动钱包，没有流水也就没有凭证 —— 没发生的资金往来不该有凭据
	if s.receipts == nil || session == nil || transactionNo == "" {
		return nil
	}
	info := s.resolveOrderReceiptContext(ctx, session, paymentdomain.ReceiptOptions{})
	return &vipdomain.ReceiptEntry{
		Available:     true,
		TransactionNo: transactionNo,
		DocumentType:  string(receipt.TypeReceipt),
		Locale:        info.locale,
		Currency:      s.resolveWalletCurrency(ctx, session.AppID),
		DownloadURL:   walletReceiptDownloadURL(transactionNo),
		ExportURL:     walletReceiptExportURL(transactionNo),
		EmailURL:      walletReceiptEmailURL(transactionNo),
		Emailable:     info.emailable,
		EmailHint:     info.emailHint,
	}
}

// 钱包凭证的三条入口地址。路由前缀只写在这里一处。
func walletReceiptDownloadURL(no string) string {
	return fmt.Sprintf("/api/wallet/transactions/%s/receipt", no)
}
func walletReceiptExportURL(no string) string {
	return fmt.Sprintf("/api/wallet/transactions/%s/bill", no)
}
func walletReceiptEmailURL(no string) string {
	return fmt.Sprintf("/api/wallet/transactions/%s/receipt/email", no)
}

// ── 取数与归属校验 ──

func (s *PaymentService) requireUserWalletTransaction(ctx context.Context, session *authdomain.Session, transactionNo string) (*walletdomain.Transaction, error) {
	transactionNo = strings.TrimSpace(transactionNo)
	if transactionNo == "" {
		return nil, apperrors.New(40099, http.StatusBadRequest, "流水号不能为空")
	}
	txn, err := s.pg.GetWalletTransactionByNoForUser(ctx, session.AppID, session.UserID, transactionNo)
	if err != nil {
		return nil, err
	}
	if txn == nil {
		return nil, apperrors.New(40477, http.StatusNotFound, "流水不存在")
	}
	return txn, nil
}

func (s *PaymentService) requireAppWalletTransaction(ctx context.Context, appID int64, transactionNo string) (*walletdomain.Transaction, error) {
	transactionNo = strings.TrimSpace(transactionNo)
	if transactionNo == "" {
		return nil, apperrors.New(40099, http.StatusBadRequest, "流水号不能为空")
	}
	txn, err := s.pg.GetWalletTransactionByNo(ctx, appID, transactionNo)
	if err != nil {
		return nil, err
	}
	if txn == nil {
		return nil, apperrors.New(40477, http.StatusNotFound, "流水不存在")
	}
	return txn, nil
}

func (s *PaymentService) loadWalletCustomer(ctx context.Context, appID int64, userID int64) (*userdomain.User, *userdomain.Profile, error) {
	user, err := s.pg.GetUserByID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	profile, err := s.pg.GetUserProfileByUserID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return user, profile, nil
}

// ── 渲染 ──

func (s *PaymentService) renderUserWalletReceipt(ctx context.Context, session *authdomain.Session, transactionNo string, opts paymentdomain.ReceiptOptions) (receipt.Document, *receipt.Result, error) {
	txn, err := s.requireUserWalletTransaction(ctx, session, transactionNo)
	if err != nil {
		return receipt.Document{}, nil, err
	}
	return s.renderWalletReceiptFor(ctx, txn, session.UserID, opts)
}

// renderWalletReceiptFor 出具一条流水的凭证。
//
// 委派判定在这里，而且只在这里：挂着有效关联订单的流水一律交给订单那条链路，
// 拿到的是与「我的订单」里下载到的**完全同一份**文档（同编号、同金额、同退款明细）。
func (s *PaymentService) renderWalletReceiptFor(ctx context.Context, txn *walletdomain.Transaction, userID int64, opts paymentdomain.ReceiptOptions) (receipt.Document, *receipt.Result, error) {
	if s.receipts == nil {
		return receipt.Document{}, nil, errReceiptRendererUnavailable()
	}
	user, profile, err := s.loadWalletCustomer(ctx, txn.AppID, txn.UserID)
	if err != nil {
		return receipt.Document{}, nil, err
	}
	settings := s.loadUserUISettings(ctx, userID)

	if order := s.resolveWalletReceiptOrder(ctx, txn); order != nil {
		return s.renderReceipt(ctx, order, user, profile, opts, settings)
	}
	doc, err := s.buildWalletReceiptDocument(ctx, txn, user, profile, opts)
	if err != nil {
		return receipt.Document{}, nil, err
	}
	result, err := s.renderReceiptDocument(doc, txn.TransactionNo, opts, settings)
	if err != nil {
		return receipt.Document{}, nil, err
	}
	return doc, result, nil
}

// resolveWalletReceiptOrder 该流水的凭证是否应由某张订单出具。
//
// 三重校验缺一不可：订单存在、属于同一应用、属于同一用户。
// 关联单号是业务侧写进去的字符串，不是外键；只按单号取回就直接用，
// 等于把「凭证给谁」交给一列没有约束的文本决定。
func (s *PaymentService) resolveWalletReceiptOrder(ctx context.Context, txn *walletdomain.Transaction) *paymentdomain.Order {
	orderNo := strings.TrimSpace(txn.RelatedOrderNo)
	if orderNo == "" {
		return nil
	}
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, orderNo)
	if err != nil {
		// 订单读不到就退回按流水自行出具：为一次查库抖动让用户拿不到凭证不划算，
		// 关联单号仍会印在附加信息里，人工对账不会断线。
		s.log.Warn("resolve wallet receipt order failed",
			zap.String("transactionNo", txn.TransactionNo), zap.String("orderNo", orderNo), zap.Error(err))
		return nil
	}
	if order == nil || order.AppID != txn.AppID {
		return nil
	}
	if order.UserID == nil || *order.UserID != txn.UserID {
		return nil
	}
	return order
}

// ── 文档装配 ──

// walletReceiptEnv 装配一份钱包凭证所需的、与流水本身无关的环境事实。
//
// 单独一个结构是为了让装配逻辑成为**纯函数**：这三项各要打一次库
// （应用、平台设置、应用级交易设置），混在装配里就意味着「凭证长什么样」
// 这件事没有数据库就测不了。
type walletReceiptEnv struct {
	AppName  string
	Brand    string
	Currency string
}

func (s *PaymentService) walletReceiptEnv(ctx context.Context, appID int64) (walletReceiptEnv, error) {
	app, err := s.requireApp(ctx, appID)
	if err != nil {
		return walletReceiptEnv{}, err
	}
	return walletReceiptEnv{
		AppName:  app.Name,
		Brand:    s.platformBrand(ctx),
		Currency: s.resolveWalletCurrency(ctx, appID),
	}, nil
}

// buildWalletReceiptDocument 把一条钱包流水装配成一份凭证文档。
func (s *PaymentService) buildWalletReceiptDocument(
	ctx context.Context,
	txn *walletdomain.Transaction,
	user *userdomain.User,
	profile *userdomain.Profile,
	opts paymentdomain.ReceiptOptions,
) (receipt.Document, error) {
	env, err := s.walletReceiptEnv(ctx, txn.AppID)
	if err != nil {
		return receipt.Document{}, err
	}
	return assembleWalletReceiptDocument(txn, user, profile, opts, env), nil
}

// assembleWalletReceiptDocument 流水 → 凭证文档（纯函数）。
func assembleWalletReceiptDocument(
	txn *walletdomain.Transaction,
	user *userdomain.User,
	profile *userdomain.Profile,
	opts paymentdomain.ReceiptOptions,
	env walletReceiptEnv,
) receipt.Document {
	// 金额取绝对值：凭证上的「合计」是这笔交易的规模，方向由类型与附注表达。
	// 印一个负数总额会让任何一款报销系统都拒收。
	amount := txn.Amount.Abs()
	docType := resolveWalletReceiptDocType(txn, opts.DocumentType)

	return receipt.Document{
		Type: docType,
		// 钱包流水没有中间态：写进 wallet_transactions 的那一刻钱已经动了，
		// 不存在「待支付的流水」，因此状态恒为已完成。
		Status:   receipt.StatusPaid,
		Number:   receiptNumberFor(docType, txn.TransactionNo),
		IssuedAt: time.Now().UTC(),
		Brand:    pickString(env.Brand, "Aegis"),
		Issuer: receipt.Party{
			Name:      pickString(env.AppName, fmt.Sprintf("App #%d", txn.AppID)),
			Reference: fmt.Sprintf("APP-%d", txn.AppID),
		},
		Customer: buildWalletReceiptCustomer(txn, user, profile),
		Currency: env.Currency,
		Items:    []receipt.LineItem{buildWalletLineItem(txn, amount)},
		Total:    amount,
		Payment: receipt.PaymentInfo{
			MethodKey:   paymentdomain.MethodBalance,
			MethodLabel: paymentdomain.MethodBalance,
			// 内部账本没有上游交易号，流水号走附加信息区。
			// 把它塞进「交易流水号」会让人以为存在一个可以拿去找渠道对账的号。
			PaidAt:   &txn.CreatedAt,
			ClientIP: txn.ClientIP,
		},
		Metadata: buildWalletReceiptMetadata(txn, env.Currency),
		Notes:    walletReceiptNotes(txn.Amount.IsPositive()),
	}
}

// buildWalletLineItem 行项目。
//
// 品名分两种来源，必须分开处理：
//   - consume 的标题是**接入方填的**消费说明（「购买 3 张地图券」），是真正的业务内容，
//     原样展示；翻译它等于把用户的数据改掉。
//   - 其余类型的标题是平台生成的中文常量（「余额支付订单」「管理员余额调整」），
//     走译文键，否则一份英文凭证的商品栏会是中文。
func buildWalletLineItem(txn *walletdomain.Transaction, amount decimal.Decimal) receipt.LineItem {
	item := receipt.LineItem{
		Name:        strings.TrimSpace(txn.Title),
		Description: strings.TrimSpace(txn.Remark),
		Quantity:    decimal.NewFromInt(1),
		UnitPrice:   amount,
		Amount:      amount,
	}
	if txn.Type != walletdomain.TxnTypeConsume {
		item.NameKey = "wallet.type." + txn.Type
	}
	return item
}

func buildWalletReceiptCustomer(txn *walletdomain.Transaction, user *userdomain.User, profile *userdomain.Profile) receipt.Party {
	var party receipt.Party
	if profile != nil && strings.TrimSpace(profile.Nickname) != "" {
		party.Name = strings.TrimSpace(profile.Nickname)
	} else if user != nil && strings.TrimSpace(user.Account) != "" {
		party.Name = strings.TrimSpace(user.Account)
	}
	if user != nil {
		party.Subtitle = strings.TrimSpace(user.Account)
	}
	if profile != nil {
		party.Email = strings.TrimSpace(profile.Email)
	}
	party.Reference = fmt.Sprintf("UID-%d", txn.UserID)
	return party
}

// buildWalletReceiptMetadata 附加信息区。
//
// 变动前后余额一定要印：一份只写「扣了 50」的凭证无法自证，
// 而「变动前 200 → 变动后 150」是可以与对账单逐行核对的。
func buildWalletReceiptMetadata(txn *walletdomain.Transaction, currency string) []receipt.KeyValue {
	direction := "wallet.direction.out"
	if txn.Amount.IsPositive() {
		direction = "wallet.direction.in"
	}
	pairs := []receipt.KeyValue{
		{Key: "meta.walletTxnNo", Value: txn.TransactionNo},
		{Key: "meta.walletType", ValueKey: "wallet.type." + txn.Type},
		{Key: "meta.direction", ValueKey: direction},
		{Key: "meta.balanceBefore", Value: moneyWithCode(txn.BalanceBefore, currency)},
		{Key: "meta.balanceAfter", Value: moneyWithCode(txn.BalanceAfter, currency)},
		{Key: "meta.appId", Value: fmt.Sprintf("%d", txn.AppID)},
		{Key: "meta.userId", Value: fmt.Sprintf("%d", txn.UserID)},
	}
	if orderNo := strings.TrimSpace(txn.RelatedOrderNo); orderNo != "" {
		pairs = append(pairs, receipt.KeyValue{Key: "meta.relatedOrder", Value: orderNo})
	}
	if operator := strings.TrimSpace(txn.Operator); operator != "" {
		pairs = append(pairs, receipt.KeyValue{Key: "meta.operator", Value: operator})
	}
	// VIP 直购的套餐快照就在流水 metadata 里，印上去凭证才说得清买的是什么
	if name := metaString(txn.Metadata, "planName"); name != "" {
		pairs = append(pairs, receipt.KeyValue{Key: "meta.plan", Value: name})
	}
	if days := metaInt64(txn.Metadata, "durationDays"); days > 0 {
		pairs = append(pairs, receipt.KeyValue{Key: "meta.duration", Value: fmt.Sprintf("%d", days)})
	}
	return pairs
}

// moneyWithCode 金额 + 币种代码。
//
// 不走本地化的货币格式：这里是在文档装配阶段，还没协商出语言。
// 「150.00 CNY」在任何语言下都读得懂，也不会因为语言变化而对不上账。
func moneyWithCode(amount decimal.Decimal, currency string) string {
	value := amount.StringFixed(2)
	if currency = strings.TrimSpace(currency); currency == "" {
		return value
	}
	return value + " " + currency
}

// resolveWalletReceiptDocType 钱包凭证类型。
//
// 只有退款入账出「退款凭证」—— 这份文件此时证明的是一笔退回的钱。
// 其余（充值、消费、会员开通、订单支付、管理员调整）都是已完成的账本条目，
// 一律出收据：钱确实动过，且没有中间态可言。
func resolveWalletReceiptDocType(txn *walletdomain.Transaction, override string) receipt.DocType {
	switch strings.TrimSpace(override) {
	case paymentdomain.ReceiptTypeReceipt:
		return receipt.TypeReceipt
	case paymentdomain.ReceiptTypeInvoice:
		return receipt.TypeInvoice
	case paymentdomain.ReceiptTypeCreditNote:
		return receipt.TypeCreditNote
	}
	if txn.Type == walletdomain.TxnTypeRefund {
		return receipt.TypeCreditNote
	}
	return receipt.TypeReceipt
}

func walletReceiptNotes(credit bool) []receipt.Note {
	if credit {
		return []receipt.Note{{Key: "notes.walletCredit"}}
	}
	return []receipt.Note{{Key: "notes.walletDebit"}}
}

// resolveWalletCurrency 钱包记账币种。
//
// 钱包余额本身没有币种列（余额只是一个数），而一份印着数字却不说是哪国钱的
// 凭证既不能报销也不能对账。取应用级配置，未配置时退回余额渠道**自述**的首选货币 ——
// 与订单凭证的 defaultCurrencyForMethod 同一条取数规则，不另建一张表。
func (s *PaymentService) resolveWalletCurrency(ctx context.Context, appID int64) string {
	if s.apps != nil {
		if app, err := s.apps.GetApp(ctx, appID); err == nil && app != nil {
			if code := strings.ToUpper(strings.TrimSpace(s.apps.ResolveCommerceSettings(app).WalletCurrency)); code != "" {
				return code
			}
		}
	}
	return s.defaultCurrencyForMethod(paymentdomain.MethodBalance)
}

// receiptSubjectNo 文件名里用的主体单号：订单凭证用订单号，钱包凭证用流水号。
// 从文档本身取而不是从调用方传，是为了让委派到订单时文件名跟着变成订单号 ——
// 下载下来的两份文件叫同一个名字才对，因为它们本来就是同一份。
func receiptSubjectNo(doc receipt.Document) string {
	if orderNo := strings.TrimSpace(doc.OrderNo); orderNo != "" {
		return orderNo
	}
	// 编号形如 RCP-WAL2026...，去掉类型前缀即主体单号
	if index := strings.Index(doc.Number, "-"); index >= 0 {
		return doc.Number[index+1:]
	}
	return doc.Number
}

// ── 邮件投递 ──

// deliverWalletReceiptEmail 与订单凭证邮件共用同一条投递链路（能力探测 → 落盘 →
// 签名链接 → 同语言正文 → 附件）。差别只在文档从哪来，因此这里只负责把
// 钱包流水渲染成文档，之后交给 sendReceiptDocumentEmail。
func (s *PaymentService) deliverWalletReceiptEmail(
	ctx context.Context,
	txn *walletdomain.Transaction,
	user *userdomain.User,
	profile *userdomain.Profile,
	to string,
	opts paymentdomain.ReceiptOptions,
	uiSettings map[string]any,
) (*ReceiptEmailResult, error) {
	if s.email == nil {
		return nil, apperrors.New(50374, http.StatusServiceUnavailable, "邮件服务不可用，无法投递凭证")
	}
	// 挂着订单的流水直接走订单那条链路：邮件里的凭证必须与下载到的是同一份
	if order := s.resolveWalletReceiptOrder(ctx, txn); order != nil {
		return s.deliverReceiptEmail(ctx, order, user, profile, to, opts, uiSettings)
	}
	if s.receipts == nil {
		return nil, errReceiptRendererUnavailable()
	}
	doc, err := s.buildWalletReceiptDocument(ctx, txn, user, profile, opts)
	if err != nil {
		return nil, err
	}
	rendered, err := s.renderReceiptDocument(doc, txn.TransactionNo, opts, uiSettings)
	if err != nil {
		return nil, err
	}
	return s.sendReceiptDocumentEmail(ctx, receiptEmailSubject{
		AppID:         txn.AppID,
		UserID:        txn.UserID,
		TransactionNo: txn.TransactionNo,
		PaidAt:        &txn.CreatedAt,
		PaymentMethod: paymentdomain.MethodBalance,
	}, doc, rendered, profile, to, opts, uiSettings)
}
