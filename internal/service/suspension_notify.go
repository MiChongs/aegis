package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	notificationdomain "aegis/internal/domain/notification"
	"aegis/internal/domain/suspension"
	userdomain "aegis/internal/domain/user"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

// 封禁、解封、申诉受理、申诉未通过四类通知：邮件（绑定了邮箱或申诉时填了联系邮箱）+ 站内消息。
// 文案在 domain/suspension/notices.go，按用户语言；品牌（名称、Logo、强调色、页脚）取应用的邮件策略。
// sent 是同一批通知里已经发过的邮箱（全站封禁连带的多个账号共用一个邮箱时只发一封）。

func (s *SuspensionService) notifySuspended(ctx context.Context, ban *userdomain.AccountBan, sent map[string]bool) {
	if ban == nil {
		return
	}
	s.sendNotice(ctx, suspension.NoticeSuspended, ban.AppID, ban.UserID, ban, nil, "", sent)
}

func (s *SuspensionService) notifyReinstated(ctx context.Context, ban *userdomain.AccountBan, sent map[string]bool) {
	if ban == nil {
		return
	}
	s.sendNotice(ctx, suspension.NoticeReinstated, ban.AppID, ban.UserID, ban, nil, "", sent)
}

func (s *SuspensionService) notifyAppeal(ctx context.Context, kind string, appeal *suspension.Appeal, ban *userdomain.AccountBan) {
	if appeal == nil {
		return
	}
	s.sendNotice(ctx, kind, appeal.AppID, appeal.UserID, ban, appeal, appeal.ContactEmail, map[string]bool{})
}

func (s *SuspensionService) sendNotice(ctx context.Context, kind string, appID, userID int64, ban *userdomain.AccountBan, appeal *suspension.Appeal, extraEmail string, sent map[string]bool) {
	contact, err := s.pg.SuspensionContactOf(ctx, appID, userID)
	if err != nil || contact == nil {
		return
	}
	locale := suspension.NormalizeLocale(contact.Locale)
	if appeal != nil && appeal.Locale != "" {
		locale = appeal.Locale
	}
	if locale == "" {
		locale = suspension.DefaultLocale
	}
	settings := s.settingsFor(ctx, appID)
	vars := map[string]string{"app": firstNonEmpty(contact.AppName, "Aegis"), "account": contact.Account}
	lb := suspension.LabelsFor(locale)
	when := func(t time.Time) string { return formatLocalTime(t, locale) }
	if appeal != nil {
		vars["id"] = fmt.Sprintf("#%d", appeal.ID)
		if settings.CooldownHours > 0 {
			vars["date"] = when(time.Now().Add(time.Duration(settings.CooldownHours) * time.Hour))
		} else {
			vars["date"] = when(time.Now())
		}
	}
	copy := suspension.Notice(kind, locale, vars)

	// 站内消息：用户能登录时在消息中心看到（封禁中的账号解封后也能看到历史）
	s.mu.RLock()
	notify, email := s.notify, s.email
	s.mu.RUnlock()
	if notify != nil {
		content := copy.Lead
		if len(copy.Body) > 0 {
			content += "\n\n" + copy.Body[0]
		}
		if appeal != nil && appeal.Reply != "" && kind == suspension.NoticeAppealUpheld {
			content += "\n\n" + lb.Reply + "：" + appeal.Reply
		}
		level := "warning"
		if kind == suspension.NoticeReinstated {
			level = "success"
		}
		if _, err := notify.Deliver(ctx, notificationdomain.DeliverInput{
			AppID: appID, UserID: userID, Type: "account_suspension", Title: copy.Title, Content: content, Level: level,
			Metadata: map[string]any{"kind": kind, "banId": banIDOf(ban), "appealId": appealIDOf(appeal)},
			Action:   &notificationdomain.Action{Type: "route", Target: notificationdomain.RouteSuspension},
		}); err != nil {
			s.log.Debug("封禁站内消息没有发出", zap.String("kind", kind), zap.Error(err))
		}
	}

	if email == nil {
		return
	}
	recipients := []string{}
	for _, addr := range []string{contact.Email, extraEmail} {
		addr = strings.ToLower(strings.TrimSpace(addr))
		if addr == "" || sent[addr] {
			continue
		}
		dup := false
		for _, r := range recipients {
			if r == addr {
				dup = true
			}
		}
		if !dup {
			recipients = append(recipients, addr)
		}
	}
	if len(recipients) == 0 {
		return
	}

	// 详情表
	details := []emailDetail{{Label: lb.Account, Value: firstNonEmpty(contact.Nickname, contact.Account)}}
	if ban != nil && (kind == suspension.NoticeSuspended || kind == suspension.NoticeAppealUpheld) {
		reason := suspension.Reason(ban.ReasonCode, locale)
		details = append(details, emailDetail{Label: lb.Reason, Value: reason.Title})
		details = append(details, emailDetail{Label: lb.Details, Value: firstNonEmpty(ban.PublicReason, reason.Description)})
		scope := lb.LevelApp
		if ban.Level == userdomain.AccountBanLevelPlatform {
			scope = lb.LevelPlatform
		}
		details = append(details, emailDetail{Label: lb.Level, Value: scope})
		if ban.EndAt != nil {
			details = append(details, emailDetail{Label: lb.Until, Value: when(*ban.EndAt)})
		} else {
			details = append(details, emailDetail{Label: lb.Type, Value: lb.Permanent})
		}
	}
	if appeal != nil {
		details = append(details, emailDetail{Label: lb.Reference, Value: fmt.Sprintf("#%d", appeal.ID)})
	}

	blocks := []mailBlock{mailParagraph(copy.Greeting)}
	for _, p := range copy.Body {
		blocks = append(blocks, mailParagraph(p))
	}
	if ban != nil && ban.Level == userdomain.AccountBanLevelPlatform && kind == suspension.NoticeSuspended {
		blocks = append(blocks, mailParagraph(suspension.PlatformScopeLine(locale)))
	}
	blocks = append(blocks, mailDetails(details...))
	if appeal != nil && appeal.Reply != "" && kind == suspension.NoticeAppealUpheld {
		blocks = append(blocks, mailNotice(lb.Reply, appeal.Reply))
	}
	footer := copy.Footer
	if kind == suspension.NoticeSuspended && ban != nil && ban.Appealable && settings.Enabled && settings.AppealURL != "" && ban.Status == userdomain.AccountBanStatusActive {
		link := settings.AppealURL
		sep := "?"
		if strings.Contains(link, "?") {
			sep = "&"
		}
		link += sep + "token=" + s.signToken(ban.AppID, ban.UserID, ban.ID, time.Now().Add(7*24*time.Hour))
		blocks = append(blocks, mailButton(copy.CTA, link))
	}
	if kind == suspension.NoticeAppealUpheld && ban != nil {
		view := s.viewOf(ctx, ban, locale)
		if view.Appeal.Remaining <= 0 || !ban.Appealable {
			footer = suspension.FinalDecisionLine(locale)
		}
	}
	blocks = append(blocks, mailParagraph(copy.SignOff))

	config, err := email.resolveConfig(ctx, appID, "")
	if err != nil {
		s.log.Debug("封禁通知邮件：应用没有可用的发信配置", zap.Int64("appid", appID), zap.Error(err))
		return
	}
	_, brand, err := email.mailContext(ctx, appID)
	if err != nil {
		return
	}
	layout := emailLayout{
		Lang: emailLang(locale), Eyebrow: copy.Eyebrow, Title: copy.Title, Lead: copy.Lead, Preheader: copy.Lead,
		Blocks: blocks, FooterNote: footer, NoReplyNote: lb.NoReply,
	}
	brand.apply(&layout)
	html, text := renderEmail(layout)
	for _, to := range recipients {
		if _, err := email.sendRenderedMail(ctx, appID, config, to, copy.Subject, mailBody{HTML: html, Text: text}, kind); err != nil {
			s.log.Warn("封禁通知邮件发送失败", zap.String("kind", kind), zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
			continue
		}
		sent[to] = true
	}
}

func banIDOf(b *userdomain.AccountBan) int64 {
	if b == nil {
		return 0
	}
	return b.ID
}

func appealIDOf(a *suspension.Appeal) int64 {
	if a == nil {
		return 0
	}
	return a.ID
}

// emailLang HTML 的 lang 属性
func emailLang(locale string) string {
	switch locale {
	case suspension.LocaleZhTW:
		return "zh-Hant"
	case suspension.LocaleZhCN:
		return "zh-CN"
	}
	return locale
}

// formatLocalTime 邮件里的时间：按语言选格式，时区用服务的默认时区
func formatLocalTime(t time.Time, locale string) string {
	t = t.In(defaultNoticeLocation())
	switch locale {
	case suspension.LocaleEN:
		return t.Format("Jan 2, 2006 15:04 MST")
	case suspension.LocaleJA:
		return t.Format("2006年1月2日 15:04 MST")
	case suspension.LocaleKO:
		return t.Format("2006년 1월 2일 15:04 MST")
	}
	return t.Format("2006年1月2日 15:04")
}

func defaultNoticeLocation() *time.Location { return timeutil.DefaultLocation() }
