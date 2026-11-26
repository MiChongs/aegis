package service

import (
	"context"
	"fmt"
	"time"

	"aegis/pkg/timeutil"
)

// 账号注销的两类信：验证码与状态告知。

// SendAccountDeletionCode 注销验证发往绑定邮箱的验证码。
//
// 码由注销服务生成并只保存摘要，这里只负责投递：不写 emailCodeKey ——
// 那个 key 能被公开的 /auth/email/verify 逐个试探。
func (s *EmailService) SendAccountDeletionCode(ctx context.Context, appID int64, email string, code string, expireMinutes int) error {
	config, err := s.resolveConfig(ctx, appID, "")
	if err != nil {
		return err
	}
	_, _, err = s.sendCodeMail(ctx, appID, config, email, code, "account_deletion", expireMinutes)
	return err
}

// 注销告知的种类。
const (
	AccountDeletionNoticeSubmitted = "submitted"
	AccountDeletionNoticeScheduled = "scheduled"
	AccountDeletionNoticeReminder  = "reminder"
	AccountDeletionNoticeCancelled = "cancelled"
	AccountDeletionNoticeRejected  = "rejected"
	AccountDeletionNoticeCompleted = "completed"
)

// AccountDeletionNotice 注销告知信里的事实。
type AccountDeletionNotice struct {
	Kind        string
	Account     string
	At          time.Time
	ScheduledAt *time.Time
	Reason      string
	Note        string
	IP          string
	Device      string
}

// SendAccountDeletionNotice 注销进度告知。账号已经删掉时（completed）这是用户能收到的最后一封信。
func (s *EmailService) SendAccountDeletionNotice(ctx context.Context, appID int64, email string, notice AccountDeletionNotice) error {
	config, err := s.resolveConfig(ctx, appID, "")
	if err != nil {
		return err
	}
	_, brand, err := s.mailContext(ctx, appID)
	if err != nil {
		return err
	}
	loc := timeutil.DefaultLocation()
	at := notice.At
	if at.IsZero() {
		at = timeutil.Now()
	}
	format := func(t time.Time) string { return t.In(loc).Format("2006-01-02 15:04") }
	details := []emailDetail{{Label: "账号", Value: notice.Account}, {Label: "时间", Value: format(at)}}
	if notice.ScheduledAt != nil {
		details = append(details, emailDetail{Label: "注销时间", Value: format(*notice.ScheduledAt)})
	}
	if notice.Device != "" {
		details = append(details, emailDetail{Label: "设备", Value: notice.Device})
	}
	if notice.IP != "" {
		details = append(details, emailDetail{Label: "IP 地址", Value: notice.IP})
	}
	if notice.Reason != "" {
		details = append(details, emailDetail{Label: "注销原因", Value: notice.Reason})
	}
	if notice.Note != "" {
		details = append(details, emailDetail{Label: "说明", Value: notice.Note})
	}

	var title, lead, footer, subject string
	switch notice.Kind {
	case AccountDeletionNoticeSubmitted:
		title, lead = "注销申请已提交", "账号的注销申请已提交，正在等待审核。审核结果会另行通知。"
		footer = "不是您本人在操作的话，请立即登录撤销申请并修改密码。"
	case AccountDeletionNoticeScheduled:
		title, lead = "账号进入注销冷静期", "账号已进入注销冷静期，到期后将被自动注销。冷静期内可以随时登录撤销。"
		footer = "不是您本人在操作的话，请立即登录撤销申请并修改密码。"
	case AccountDeletionNoticeReminder:
		title, lead = "账号即将注销", "冷静期即将结束，账号将在下方时间被注销，届时数据无法恢复。"
		footer = "改变主意的话，请在注销时间之前登录并撤销申请。"
	case AccountDeletionNoticeCancelled:
		title, lead = "注销申请已撤销", "账号的注销申请已撤销，账号可以照常使用。"
		footer = "不是您本人撤销的话，可以重新提交注销申请。"
	case AccountDeletionNoticeRejected:
		title, lead = "注销申请未通过", "账号的注销申请未通过审核，账号可以照常使用。"
		footer = "对审核结果有疑问的话，请联系客服。"
	case AccountDeletionNoticeCompleted:
		title, lead = "账号已注销", "账号已注销，相关数据已删除。感谢您一直以来的使用。"
		footer = "这是关于这个账号的最后一封信。不是您本人申请的话，请尽快联系客服。"
	default:
		return fmt.Errorf("unknown account deletion notice %q", notice.Kind)
	}
	subject = fmt.Sprintf("%s：%s", brand.Name, title)
	layout := emailLayout{
		Eyebrow:    "账号安全",
		Title:      title,
		Lead:       lead,
		Blocks:     []mailBlock{mailDetails(details...)},
		FooterNote: footer,
	}
	brand.apply(&layout)
	html, text := renderEmail(layout)
	_, err = s.sendRenderedMail(ctx, appID, config, email, subject, mailBody{HTML: html, Text: text}, "account_deletion_"+notice.Kind)
	return err
}
