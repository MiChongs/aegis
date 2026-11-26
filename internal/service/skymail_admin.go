package service

import (
	emaildomain "aegis/internal/domain/email"
	apperrors "aegis/pkg/errors"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 在控制台里代管一套 Cloud Mail 邮局。
//
// 文档里的 18 个接口（登录之外）全部经这里转发：收件箱 / 发件箱、写信与回复转发、
// 邮箱账号、用户、全部邮件、注册码。入口只有两个 —— SkymailQuery（只读）与
// SkymailAction（写），对应两条管理路由；资源与动作是白名单，参数逐项按类型和范围校验后
// 才拼进请求，不会把浏览器给的任意字段原样转给对方。
//
// 凭据就是那条邮件通道的配置：管理员在「通道」里配好 Cloud Mail 之后，「邮局」视图直接可用，
// 不必再登录一次。能做什么取决于那个邮局账号的权限：管理员账号（permKeys 含 *）看得到用户、
// 全部邮件与注册码；普通账号只能管自己的邮箱和邮件，越权的请求由 Cloud Mail 以 403 拒绝。

// Skymail 只读资源。
const (
	SkymailResourceOverview      = "overview"      // 账号信息 + 邮箱账号首页 + 发信账号 id
	SkymailResourceEmails        = "emails"        // GET /api/email/list
	SkymailResourceEmail         = "email"         // 单封（带正文与附件）
	SkymailResourceAccounts      = "accounts"      // GET /api/account/list
	SkymailResourceUsers         = "users"         // GET /api/user/list
	SkymailResourceAllEmails     = "allEmails"     // GET /api/allEmail/list
	SkymailResourceAllEmail      = "allEmail"      // 全部邮件里的单封
	SkymailResourceRegKeys       = "regKeys"       // GET /api/regKey/list
	SkymailResourceRegKeyHistory = "regKeyHistory" // GET /api/regKey/history
	SkymailResourceRoles         = "roles"         // GET /api/role/selectUse（注册码下拉）
)

// Skymail 写操作。
const (
	SkymailActionSend            = "send"            // POST /api/email/send（含回复 / 转发）
	SkymailActionDeleteEmails    = "deleteEmails"    // DELETE /api/email/delete
	SkymailActionAddAccount      = "addAccount"      // POST /api/account/add
	SkymailActionDeleteAccount   = "deleteAccount"   // DELETE /api/account/delete
	SkymailActionAddUser         = "addUser"         // POST /api/user/add
	SkymailActionSetUserStatus   = "setUserStatus"   // PUT /api/user/setStatus
	SkymailActionDeleteUsers     = "deleteUsers"     // DELETE /api/user/delete
	SkymailActionDeleteAllEmails = "deleteAllEmails" // DELETE /api/allEmail/delete
	SkymailActionAddRegKey       = "addRegKey"       // POST /api/regKey/add
	SkymailActionDeleteRegKeys   = "deleteRegKeys"   // DELETE /api/regKey/delete
)

const (
	skymailMaxRecipients     = 50
	skymailMaxBatchIDs       = 200
	skymailMaxAttachmentSize = 25 << 20 // 解码后的附件总量
)

// SkymailOverview 「邮局」视图打开时的第一屏。
type SkymailOverview struct {
	BaseURL         string           `json:"baseUrl"`
	Profile         *SkymailProfile  `json:"profile"`
	Accounts        []SkymailAccount `json:"accounts"`
	SenderAccountID int64            `json:"senderAccountId"`
	SenderAddress   string           `json:"senderAddress"`
	// IsAdmin 账号带 * 权限：控制台据此决定显示不显示用户、全部邮件、注册码三页。
	IsAdmin bool `json:"isAdmin"`
	// SenderError 发件地址解析不出账号时的原因（不影响其余功能）。
	SenderError string `json:"senderError,omitempty"`
}

// SkymailActionResult 写操作的回执。
type SkymailActionResult struct {
	OK      bool            `json:"ok"`
	Emails  []SkymailEmail  `json:"emails,omitempty"`
	Account *SkymailAccount `json:"account,omitempty"`
}

func (s *EmailService) skymail() *skymailClient {
	if sender, ok := s.senders[emaildomain.ProviderSkymail].(*skymailEmailSender); ok {
		return sender.client
	}
	return nil
}

// loadSkymailConfig 取一条 Cloud Mail 配置的凭据（密码已解密）。
func (s *EmailService) loadSkymailConfig(ctx context.Context, appID int64, configID int64) (*emaildomain.Config, skymailCredentials, error) {
	if configID <= 0 {
		return nil, skymailCredentials{}, apperrors.New(40000, http.StatusBadRequest, "缺少邮件配置 id")
	}
	config, err := s.loadConfig(ctx, appID, configID)
	if err != nil {
		return nil, skymailCredentials{}, err
	}
	if normalizeEmailProvider(config.Provider) != emaildomain.ProviderSkymail {
		return nil, skymailCredentials{}, apperrors.New(40070, http.StatusBadRequest, "这条邮件通道不是 Cloud Mail，没有可管理的邮局")
	}
	if s.skymail() == nil {
		return nil, skymailCredentials{}, apperrors.New(50060, http.StatusInternalServerError, "Cloud Mail 客户端未装配")
	}
	cred := skymailCredentialsOf(config)
	if cred.Password == "" {
		return nil, skymailCredentials{}, apperrors.New(40068, http.StatusBadRequest, "Cloud Mail 登录密码未配置或解密失败，请在通道里重新填写")
	}
	return config, cred, nil
}

// decodeSkymailParams 严格解码参数：未知字段直接拒绝，拼错的字段名不会被静默忽略。
func decodeSkymailParams(raw json.RawMessage, out any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		trimmed = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return apperrors.New(40000, http.StatusBadRequest, "参数格式错误："+clampDetail(err.Error()))
	}
	return nil
}

func skymailParamError(message string) error {
	return apperrors.New(40000, http.StatusBadRequest, message)
}

// ── 查询 ──

type skymailEmailsParams struct {
	AccountID  int64 `json:"accountId"`
	Type       int   `json:"type"`
	EmailID    int64 `json:"emailId"`
	Size       int   `json:"size"`
	TimeSort   *int  `json:"timeSort"`
	AllReceive *int  `json:"allReceive"`
	Full       *int  `json:"full"`
}

type skymailEmailParams struct {
	AccountID int64 `json:"accountId"`
	Type      int   `json:"type"`
	EmailID   int64 `json:"emailId"`
}

type skymailAccountsParams struct {
	Size      int    `json:"size"`
	AccountID int64  `json:"accountId"`
	LastSort  *int64 `json:"lastSort"`
}

type skymailUsersParams struct {
	Num      int    `json:"num"`
	Size     int    `json:"size"`
	Email    string `json:"email"`
	TimeSort *int   `json:"timeSort"`
	Status   *int   `json:"status"`
	IsDel    *int   `json:"isDel"`
}

type skymailAllEmailsParams struct {
	EmailID      int64  `json:"emailId"`
	Size         int    `json:"size"`
	TimeSort     *int   `json:"timeSort"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	Subject      string `json:"subject"`
	UserEmail    string `json:"userEmail"`
	AccountEmail string `json:"accountEmail"`
	Full         *int   `json:"full"`
}

type skymailRegKeysParams struct {
	Code string `json:"code"`
}

type skymailRegKeyHistoryParams struct {
	RegKeyID int64 `json:"regKeyId"`
}

// SkymailQuery 只读地查询邮局里的一类资源。
func (s *EmailService) SkymailQuery(ctx context.Context, appID int64, configID int64, resource string, params json.RawMessage) (any, error) {
	config, cred, err := s.loadSkymailConfig(ctx, appID, configID)
	if err != nil {
		return nil, err
	}
	client := s.skymail()
	switch strings.TrimSpace(resource) {
	case SkymailResourceOverview:
		return s.skymailOverview(ctx, client, config, cred)

	case SkymailResourceEmails:
		var p skymailEmailsParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		accountID, err := s.skymailAccountOrSender(ctx, client, config, cred, p.AccountID)
		if err != nil {
			return nil, err
		}
		if p.Type != 0 && p.Type != 1 {
			return nil, skymailParamError("type 只能是 0（收件）或 1（发件）")
		}
		query := url.Values{
			"accountId": {strconv.FormatInt(accountID, 10)},
			"type":      {strconv.Itoa(p.Type)},
			"size":      {strconv.Itoa(clampInt(p.Size, 20, 1, 50))},
			"full":      {"0"},
		}
		if p.EmailID > 0 {
			query.Set("emailId", strconv.FormatInt(p.EmailID, 10))
		}
		if err := setSkymailFlag(query, "timeSort", p.TimeSort); err != nil {
			return nil, err
		}
		if err := setSkymailFlag(query, "allReceive", p.AllReceive); err != nil {
			return nil, err
		}
		if err := setSkymailFlag(query, "full", p.Full); err != nil {
			return nil, err
		}
		return client.listEmails(ctx, cred, query)

	case SkymailResourceEmail:
		var p skymailEmailParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		if p.EmailID <= 0 {
			return nil, skymailParamError("缺少邮件 id")
		}
		accountID, err := s.skymailAccountOrSender(ctx, client, config, cred, p.AccountID)
		if err != nil {
			return nil, err
		}
		// 文档没有单封详情接口。列表是「比游标 id 更早」的游标分页，
		// 拿 id+1 当游标、只取一条，得到的就是这封本身（若它属于该邮箱与收发类型）。
		page, err := client.listEmails(ctx, cred, url.Values{
			"accountId": {strconv.FormatInt(accountID, 10)},
			"type":      {strconv.Itoa(p.Type)},
			"emailId":   {strconv.FormatInt(p.EmailID+1, 10)},
			"size":      {"1"},
			"timeSort":  {"0"},
			"full":      {"1"},
		})
		if err != nil {
			return nil, err
		}
		return pickSkymailEmail(page, p.EmailID)

	case SkymailResourceAccounts:
		var p skymailAccountsParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		query := url.Values{"size": {strconv.Itoa(clampInt(p.Size, 30, 1, 30))}}
		if p.AccountID > 0 {
			query.Set("accountId", strconv.FormatInt(p.AccountID, 10))
		}
		if p.LastSort != nil {
			query.Set("lastSort", strconv.FormatInt(*p.LastSort, 10))
		}
		items, err := client.listAccounts(ctx, cred, query)
		if err != nil {
			return nil, err
		}
		return orEmptySlice(items), nil

	case SkymailResourceUsers:
		var p skymailUsersParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		query := url.Values{
			"num":  {strconv.Itoa(clampInt(p.Num, 1, 1, 100000))},
			"size": {strconv.Itoa(clampInt(p.Size, 20, 1, 50))},
		}
		if email := strings.TrimSpace(p.Email); email != "" {
			query.Set("email", clampString(email, 128))
		}
		if err := setSkymailFlag(query, "timeSort", p.TimeSort); err != nil {
			return nil, err
		}
		if p.Status != nil && *p.Status != -1 {
			if err := setSkymailFlag(query, "status", p.Status); err != nil {
				return nil, err
			}
		}
		if err := setSkymailFlag(query, "isDel", p.IsDel); err != nil {
			return nil, err
		}
		page, err := client.listUsers(ctx, cred, query)
		if err != nil {
			return nil, err
		}
		page.List = orEmptySlice(page.List)
		return page, nil

	case SkymailResourceAllEmails:
		var p skymailAllEmailsParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		kind := strings.TrimSpace(p.Type)
		if kind == "" {
			kind = "receive"
		}
		switch kind {
		case "all", "receive", "send", "delete", "noone":
		default:
			return nil, skymailParamError("type 只能是 all / receive / send / delete / noone")
		}
		query := url.Values{
			"type": {kind},
			"size": {strconv.Itoa(clampInt(p.Size, 20, 1, 50))},
			"full": {"0"},
		}
		if p.EmailID > 0 {
			query.Set("emailId", strconv.FormatInt(p.EmailID, 10))
		}
		for key, value := range map[string]string{
			"name": p.Name, "subject": p.Subject, "userEmail": p.UserEmail, "accountEmail": p.AccountEmail,
		} {
			if value = strings.TrimSpace(value); value != "" {
				query.Set(key, clampString(value, 128))
			}
		}
		if err := setSkymailFlag(query, "timeSort", p.TimeSort); err != nil {
			return nil, err
		}
		if err := setSkymailFlag(query, "full", p.Full); err != nil {
			return nil, err
		}
		page, err := client.listAllEmails(ctx, cred, query)
		if err != nil {
			return nil, err
		}
		page.List = orEmptySlice(page.List)
		return page, nil

	case SkymailResourceAllEmail:
		var p skymailEmailParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		if p.EmailID <= 0 {
			return nil, skymailParamError("缺少邮件 id")
		}
		page, err := client.listAllEmails(ctx, cred, url.Values{
			"type":     {"all"},
			"emailId":  {strconv.FormatInt(p.EmailID+1, 10)},
			"size":     {"1"},
			"timeSort": {"0"},
			"full":     {"1"},
		})
		if err != nil {
			return nil, err
		}
		return pickSkymailEmail(page, p.EmailID)

	case SkymailResourceRegKeys:
		var p skymailRegKeysParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		items, err := client.listRegKeys(ctx, cred, clampString(strings.TrimSpace(p.Code), 64))
		if err != nil {
			return nil, err
		}
		return orEmptySlice(items), nil

	case SkymailResourceRegKeyHistory:
		var p skymailRegKeyHistoryParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		if p.RegKeyID <= 0 {
			return nil, skymailParamError("缺少注册码 id")
		}
		items, err := client.regKeyHistory(ctx, cred, p.RegKeyID)
		if err != nil {
			return nil, err
		}
		return orEmptySlice(items), nil

	case SkymailResourceRoles:
		items, err := client.roles(ctx, cred)
		if err != nil {
			return nil, err
		}
		return orEmptySlice(items), nil
	}
	return nil, skymailParamError("不支持的查询：" + clampString(resource, 40))
}

func (s *EmailService) skymailOverview(ctx context.Context, client *skymailClient, config *emaildomain.Config, cred skymailCredentials) (*SkymailOverview, error) {
	profile, err := client.profile(ctx, cred)
	if err != nil {
		return nil, err
	}
	accounts, err := client.listAccounts(ctx, cred, url.Values{"size": {"30"}})
	if err != nil {
		return nil, err
	}
	fromAddress, _, _ := config.SenderIdentity()
	overview := &SkymailOverview{
		BaseURL:       cred.BaseURL,
		Profile:       profile,
		Accounts:      orEmptySlice(accounts),
		SenderAddress: firstNonEmpty(strings.TrimSpace(fromAddress), cred.Email),
	}
	for _, key := range profile.PermKeys {
		if key == "*" {
			overview.IsAdmin = true
			break
		}
	}
	if accountID, err := client.resolveAccountID(ctx, cred, fromAddress); err == nil {
		overview.SenderAccountID = accountID
	} else {
		overview.SenderError = err.Error()
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			overview.SenderError = appErr.Message
		}
	}
	return overview, nil
}

// skymailAccountOrSender 没指定邮箱账号时落到这条通道的发信账号。
func (s *EmailService) skymailAccountOrSender(ctx context.Context, client *skymailClient, config *emaildomain.Config, cred skymailCredentials, accountID int64) (int64, error) {
	if accountID > 0 {
		return accountID, nil
	}
	fromAddress, _, _ := config.SenderIdentity()
	return client.resolveAccountID(ctx, cred, fromAddress)
}

func pickSkymailEmail(page *SkymailEmailPage, emailID int64) (*SkymailEmail, error) {
	if page != nil {
		for i := range page.List {
			if page.List[i].EmailID == emailID {
				email := page.List[i]
				email.AttList = orEmptySlice(email.AttList)
				return &email, nil
			}
		}
	}
	return nil, apperrors.New(40469, http.StatusNotFound, "邮件不存在或已被删除")
}

// setSkymailFlag 0/1 开关参数：nil 表示不传，交给 Cloud Mail 用默认值。
func setSkymailFlag(query url.Values, key string, value *int) error {
	if value == nil {
		return nil
	}
	if *value != 0 && *value != 1 {
		return skymailParamError(key + " 只能是 0 或 1")
	}
	query.Set(key, strconv.Itoa(*value))
	return nil
}

// ── 写操作 ──

type skymailSendAttachmentParams struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`
	ContentType string `json:"contentType"`
}

type skymailSendParams struct {
	AccountID    int64                         `json:"accountId"`
	ReceiveEmail []string                      `json:"receiveEmail"`
	Subject      string                        `json:"subject"`
	Content      string                        `json:"content"`
	Text         string                        `json:"text"`
	Name         string                        `json:"name"`
	SendType     string                        `json:"sendType"`
	EmailID      int64                         `json:"emailId"`
	Attachments  []skymailSendAttachmentParams `json:"attachments"`
}

type skymailIDsParams struct {
	EmailIDs  []int64 `json:"emailIds,omitempty"`
	UserIDs   []int64 `json:"userIds,omitempty"`
	RegKeyIDs []int64 `json:"regKeyIds,omitempty"`
}

type skymailAddAccountParams struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

type skymailDeleteAccountParams struct {
	AccountID int64 `json:"accountId"`
}

type skymailAddUserParams struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type skymailSetUserStatusParams struct {
	UserID int64 `json:"userId"`
	Status int   `json:"status"`
}

type skymailAddRegKeyParams struct {
	Code       string `json:"code"`
	RoleID     int64  `json:"roleId"`
	Count      int64  `json:"count"`
	ExpireTime string `json:"expireTime"`
}

// SkymailAction 在邮局里执行一个写操作。
func (s *EmailService) SkymailAction(ctx context.Context, appID int64, configID int64, action string, params json.RawMessage) (*SkymailActionResult, error) {
	config, cred, err := s.loadSkymailConfig(ctx, appID, configID)
	if err != nil {
		return nil, err
	}
	client := s.skymail()
	ok := &SkymailActionResult{OK: true}
	action = strings.TrimSpace(action)
	switch action {
	case SkymailActionSend:
		var p skymailSendParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		request, err := buildSkymailSendRequest(p)
		if err != nil {
			return nil, err
		}
		if request.AccountID, err = s.skymailAccountOrSender(ctx, client, config, cred, p.AccountID); err != nil {
			return nil, err
		}
		if request.Name == "" {
			_, fromName, _ := config.SenderIdentity()
			request.Name = strings.TrimSpace(fromName)
		}
		sent, err := client.send(ctx, cred, request)
		if err != nil {
			return nil, err
		}
		ok.Emails = orEmptySlice(sent)
		return ok, nil

	case SkymailActionDeleteEmails, SkymailActionDeleteAllEmails:
		var p skymailIDsParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		ids, err := validSkymailIDs(p.EmailIDs, "邮件")
		if err != nil {
			return nil, err
		}
		if action == SkymailActionDeleteEmails {
			err = client.deleteEmails(ctx, cred, ids)
		} else {
			err = client.deleteAllEmails(ctx, cred, ids)
		}
		if err != nil {
			return nil, err
		}
		return ok, nil

	case SkymailActionAddAccount:
		var p skymailAddAccountParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		email, err := validSkymailAddress(p.Email)
		if err != nil {
			return nil, err
		}
		account, err := client.addAccount(ctx, cred, email, clampString(strings.TrimSpace(p.Token), 4096))
		if err != nil {
			return nil, err
		}
		ok.Account = account
		return ok, nil

	case SkymailActionDeleteAccount:
		var p skymailDeleteAccountParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		if p.AccountID <= 0 {
			return nil, skymailParamError("缺少邮箱账号 id")
		}
		if err := client.deleteAccount(ctx, cred, p.AccountID); err != nil {
			return nil, err
		}
		return ok, nil

	case SkymailActionAddUser:
		var p skymailAddUserParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		email, err := validSkymailAddress(p.Email)
		if err != nil {
			return nil, err
		}
		if n := len([]rune(p.Password)); n < 6 || n > 64 {
			return nil, skymailParamError("密码需要 6 到 64 位")
		}
		if err := client.addUser(ctx, cred, email, p.Password); err != nil {
			return nil, err
		}
		return ok, nil

	case SkymailActionSetUserStatus:
		var p skymailSetUserStatusParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		if p.UserID <= 0 {
			return nil, skymailParamError("缺少用户 id")
		}
		if p.Status != 0 && p.Status != 1 {
			return nil, skymailParamError("status 只能是 0（正常）或 1（禁用）")
		}
		if err := client.setUserStatus(ctx, cred, p.UserID, p.Status); err != nil {
			return nil, err
		}
		return ok, nil

	case SkymailActionDeleteUsers:
		var p skymailIDsParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		ids, err := validSkymailIDs(p.UserIDs, "用户")
		if err != nil {
			return nil, err
		}
		if err := client.deleteUsers(ctx, cred, ids); err != nil {
			return nil, err
		}
		return ok, nil

	case SkymailActionAddRegKey:
		var p skymailAddRegKeyParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		code := strings.TrimSpace(p.Code)
		if code == "" || len([]rune(code)) > 64 {
			return nil, skymailParamError("注册码需要 1 到 64 个字符")
		}
		if p.RoleID <= 0 {
			return nil, skymailParamError("请选择权限身份")
		}
		if p.Count <= 0 || p.Count > 1_000_000 {
			return nil, skymailParamError("可用次数需要在 1 到 1000000 之间")
		}
		expire, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(p.ExpireTime), time.UTC)
		if err != nil {
			return nil, skymailParamError("有效期格式应为 YYYY-MM-DD")
		}
		if !expire.After(time.Now().UTC()) {
			return nil, skymailParamError("有效期必须晚于今天")
		}
		if err := client.addRegKey(ctx, cred, code, p.RoleID, p.Count, expire.Format("2006-01-02")); err != nil {
			return nil, err
		}
		return ok, nil

	case SkymailActionDeleteRegKeys:
		var p skymailIDsParams
		if err := decodeSkymailParams(params, &p); err != nil {
			return nil, err
		}
		ids, err := validSkymailIDs(p.RegKeyIDs, "注册码")
		if err != nil {
			return nil, err
		}
		if err := client.deleteRegKeys(ctx, cred, ids); err != nil {
			return nil, err
		}
		return ok, nil
	}
	return nil, skymailParamError("不支持的操作：" + clampString(action, 40))
}

func buildSkymailSendRequest(p skymailSendParams) (skymailSendRequest, error) {
	request := skymailSendRequest{
		Subject:  strings.TrimSpace(p.Subject),
		Content:  p.Content,
		Text:     p.Text,
		Name:     clampString(strings.TrimSpace(p.Name), 64),
		SendType: strings.TrimSpace(p.SendType),
	}
	seen := map[string]bool{}
	for _, raw := range p.ReceiveEmail {
		address, err := validSkymailAddress(raw)
		if err != nil {
			return request, err
		}
		if !seen[strings.ToLower(address)] {
			seen[strings.ToLower(address)] = true
			request.ReceiveEmail = append(request.ReceiveEmail, address)
		}
	}
	if len(request.ReceiveEmail) == 0 {
		return request, skymailParamError("至少填一个收件人")
	}
	if len(request.ReceiveEmail) > skymailMaxRecipients {
		return request, skymailParamError(fmt.Sprintf("一次最多 %d 个收件人", skymailMaxRecipients))
	}
	if request.Subject == "" {
		return request, skymailParamError("主题不能为空")
	}
	request.Subject = truncateSubject(request.Subject)
	if strings.TrimSpace(request.Content) == "" {
		return request, skymailParamError("正文不能为空")
	}
	switch request.SendType {
	case "":
	case "reply", "forward":
		if p.EmailID <= 0 {
			return request, skymailParamError("回复或转发需要原邮件 id")
		}
		request.EmailID = p.EmailID
	default:
		return request, skymailParamError("sendType 只能是空、reply 或 forward")
	}
	if len(p.Attachments) > skymailMaxAttachments {
		return request, skymailParamError(fmt.Sprintf("最多 %d 个附件", skymailMaxAttachments))
	}
	var total int64
	for _, attachment := range p.Attachments {
		filename := strings.TrimSpace(attachment.Filename)
		if filename == "" {
			return request, skymailParamError("附件缺少文件名")
		}
		content := strings.TrimSpace(attachment.Content)
		// 浏览器 FileReader 给的是 data URL，裁掉前缀只留 Base64。
		if _, after, found := strings.Cut(content, ";base64,"); found && strings.HasPrefix(content, "data:") {
			content = after
		}
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return request, skymailParamError("附件 " + clampString(filename, 60) + " 不是合法的 Base64")
		}
		total += int64(len(decoded))
		if total > skymailMaxAttachmentSize {
			return request, skymailParamError("附件总大小不能超过 25 MB")
		}
		request.Attachments = append(request.Attachments, skymailSendAttachment{
			Filename:    clampString(filename, 200),
			Content:     content,
			ContentType: clampString(strings.TrimSpace(attachment.ContentType), 128),
			Size:        int64(len(decoded)),
		})
	}
	return request, nil
}

func validSkymailAddress(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value {
		return "", skymailParamError("邮箱地址格式错误：" + clampString(value, 80))
	}
	return value, nil
}

func validSkymailIDs(ids []int64, label string) ([]int64, error) {
	if len(ids) == 0 {
		return nil, skymailParamError("请选择要删除的" + label)
	}
	if len(ids) > skymailMaxBatchIDs {
		return nil, skymailParamError(fmt.Sprintf("一次最多删除 %d 个%s", skymailMaxBatchIDs, label))
	}
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, skymailParamError(label + " id 不合法")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

func clampInt(value int, fallback int, low int, high int) int {
	if value == 0 {
		value = fallback
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func clampString(value string, limit int) string {
	if len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func orEmptySlice[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
