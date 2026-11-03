package httptransport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

// 管理端积分 / 经验调整（/api/app/points/*）的请求解析。
//
// 这几条是旧版兼容接口，调用方五花八门：控制台曾经发 userId 而后端只认 user_id，
// 结果每次调整都报「缺少必要的请求参数」。这里同时接受下划线与驼峰两种写法，
// 两种都给了且值不同就拒绝，绝不猜。
//
// 数值一律按十进制整数解析：拒绝小数（1.5）、科学计数法（1e3）、超出 int64 的数，
// 不经过 float64 —— 那会把 2^53 以上的数悄悄舍入。

type pointsAdjustPayload struct {
	AppID         int64
	UserID        int64
	UserIDs       []int64
	Amount        int64
	Reason        string
	RequestID     string
	OperationType string
	// Kind 批量调整的数值类型：integral（默认）/ experience
	Kind string
	// LevelMode 扣减经验时的等级处理：keep（默认）/ recalc
	LevelMode string
}

type fieldAliases struct {
	name  string
	keys  []string
	label string
}

var (
	fieldUserID    = fieldAliases{"user_id", []string{"user_id", "userId", "userid"}, "用户 ID（user_id）"}
	fieldUserIDs   = fieldAliases{"user_ids", []string{"user_ids", "userIds"}, "用户 ID 列表（user_ids）"}
	fieldAppID     = fieldAliases{"appid", []string{"appid", "appId"}, "应用 ID（appid）"}
	fieldAmount    = fieldAliases{"amount", []string{"amount"}, "调整数量（amount）"}
	fieldReason    = fieldAliases{"reason", []string{"reason"}, "调整原因（reason）"}
	fieldRequestID = fieldAliases{"request_id", []string{"request_id", "requestId"}, "请求编号（request_id）"}
	fieldOperation = fieldAliases{"operation_type", []string{"operation_type", "operationType"}, "操作类型（operation_type）"}
	fieldKind      = fieldAliases{"kind", []string{"kind"}, "数值类型（kind）"}
	fieldLevelMode = fieldAliases{"level_mode", []string{"level_mode", "levelMode"}, "等级处理方式（level_mode）"}
)

// requestFields 把 JSON 或表单请求体统一成「键 → 原始值」。
type requestFields struct {
	json map[string]json.RawMessage
	form map[string][]string
}

func readRequestFields(c *gin.Context) (*requestFields, error) {
	fields := &requestFields{}
	contentType := strings.ToLower(c.ContentType())
	if strings.Contains(contentType, "form") {
		if err := c.Request.ParseForm(); err != nil {
			return nil, fmt.Errorf("请求参数格式错误")
		}
		fields.form = c.Request.PostForm
		return fields, nil
	}
	if c.Request.Body == nil || c.Request.Body == http.NoBody {
		return nil, fmt.Errorf("请求体不能为空")
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取请求体失败")
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("请求体不能为空")
	}
	if err := json.Unmarshal(body, &fields.json); err != nil {
		return nil, fmt.Errorf("请求体必须是 JSON 对象")
	}
	return fields, nil
}

// raw 取某字段的原始值；多个别名都出现且不相同时报错。
func (f *requestFields) raw(field fieldAliases) (string, bool, error) {
	var found []string
	for _, key := range field.keys {
		if f.form != nil {
			if values, ok := f.form[key]; ok && len(values) > 0 {
				found = append(found, values[0])
			}
			continue
		}
		value, ok := f.json[key]
		if !ok || string(bytes.TrimSpace(value)) == "null" {
			continue
		}
		found = append(found, string(bytes.TrimSpace(value)))
	}
	if len(found) == 0 {
		return "", false, nil
	}
	for _, v := range found[1:] {
		if v != found[0] {
			return "", false, fmt.Errorf("%s 以多种写法给出且取值不一致", field.label)
		}
	}
	return found[0], true, nil
}

func (f *requestFields) int64(field fieldAliases) (int64, bool, error) {
	raw, ok, err := f.raw(field)
	if err != nil || !ok {
		return 0, ok, err
	}
	v, err := parseStrictInt(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%s%s", field.label, err.Error())
	}
	return v, true, nil
}

func (f *requestFields) string(field fieldAliases) (string, error) {
	raw, ok, err := f.raw(field)
	if err != nil || !ok {
		return "", err
	}
	if f.form != nil {
		return raw, nil
	}
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return "", fmt.Errorf("%s必须是字符串", field.label)
	}
	return s, nil
}

func (f *requestFields) int64List(field fieldAliases) ([]int64, error) {
	if f.form != nil {
		var out []int64
		for _, key := range field.keys {
			for _, value := range f.form[key] {
				for _, part := range strings.Split(value, ",") {
					if strings.TrimSpace(part) == "" {
						continue
					}
					v, err := parseStrictInt(part)
					if err != nil {
						return nil, fmt.Errorf("%s%s", field.label, err.Error())
					}
					out = append(out, v)
				}
			}
		}
		return out, nil
	}
	raw, ok, err := f.raw(field)
	if err != nil || !ok {
		return nil, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("%s必须是数组", field.label)
	}
	out := make([]int64, 0, len(items))
	for _, item := range items {
		v, err := parseStrictInt(string(item))
		if err != nil {
			return nil, fmt.Errorf("%s%s", field.label, err.Error())
		}
		out = append(out, v)
	}
	return out, nil
}

// parseStrictInt 接受 JSON 整数字面量或十进制整数字符串（可带正负号）。
func parseStrictInt(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		var s string
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return 0, fmt.Errorf("必须是整数")
		}
		raw = strings.TrimSpace(s)
	}
	if raw == "" {
		return 0, fmt.Errorf("不能为空")
	}
	if strings.ContainsAny(raw, ".eE") {
		return 0, fmt.Errorf("必须是整数")
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		if numErr, ok := err.(*strconv.NumError); ok && numErr.Err == strconv.ErrRange {
			return 0, fmt.Errorf("超出允许范围")
		}
		return 0, fmt.Errorf("必须是整数")
	}
	return v, nil
}

// decodePointsAdjust 解析调整请求。应用 ID 以鉴权中间件确认过的为准，
// 请求体里的 appid 只用于核对，不一致就拒绝。
func decodePointsAdjust(c *gin.Context, batch bool) (*pointsAdjustPayload, error) {
	fields, err := readRequestFields(c)
	if err != nil {
		return nil, err
	}
	out := &pointsAdjustPayload{}

	bodyAppID, hasAppID, err := fields.int64(fieldAppID)
	if err != nil {
		return nil, err
	}
	if scoped, ok := c.Get(middleware.AdminScopeAppIDKey); ok {
		out.AppID, _ = scoped.(int64)
		if hasAppID && bodyAppID != out.AppID {
			return nil, fmt.Errorf("请求中的 appid 不一致")
		}
	} else {
		out.AppID = bodyAppID
	}
	if out.AppID <= 0 {
		return nil, fmt.Errorf("缺少%s", fieldAppID.label)
	}

	if batch {
		if out.UserIDs, err = fields.int64List(fieldUserIDs); err != nil {
			return nil, err
		}
		if len(out.UserIDs) == 0 {
			return nil, fmt.Errorf("缺少%s", fieldUserIDs.label)
		}
		if out.OperationType, err = fields.string(fieldOperation); err != nil {
			return nil, err
		}
		if out.Kind, err = fields.string(fieldKind); err != nil {
			return nil, err
		}
	} else {
		var ok bool
		if out.UserID, ok, err = fields.int64(fieldUserID); err != nil {
			return nil, err
		} else if !ok || out.UserID <= 0 {
			return nil, fmt.Errorf("缺少%s", fieldUserID.label)
		}
	}

	amount, ok, err := fields.int64(fieldAmount)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("缺少%s", fieldAmount.label)
	}
	out.Amount = amount

	if out.Reason, err = fields.string(fieldReason); err != nil {
		return nil, err
	}
	if out.RequestID, err = fields.string(fieldRequestID); err != nil {
		return nil, err
	}
	if out.LevelMode, err = fields.string(fieldLevelMode); err != nil {
		return nil, err
	}
	header := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	switch {
	case out.RequestID == "":
		out.RequestID = header
	case header != "" && header != strings.TrimSpace(out.RequestID):
		return nil, fmt.Errorf("请求体中的请求编号与 Idempotency-Key 不一致")
	}
	return out, nil
}
