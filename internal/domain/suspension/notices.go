package suspension

import "strings"

// 四类通知：封禁、解封、申诉受理、申诉未通过。邮件与站内消息共用这里的文案。
// 版式与措辞参照主流平台的账号处置通知：先一句话说结论，再说原因与期限，最后说能做什么；不渲染情绪，不用营销语气。
// 文案里的占位：{app} 应用名、{account} 账号、{until} 解封时间、{id} 申诉编号、{date} 下次可申诉时间。

// 通知种类
const (
	NoticeSuspended      = "account_suspended"
	NoticeReinstated     = "account_reinstated"
	NoticeAppealReceived = "appeal_received"
	NoticeAppealUpheld   = "appeal_upheld"
)

// NoticeCopy 一类通知在一种语言里的文案
type NoticeCopy struct {
	Subject  string
	Eyebrow  string
	Title    string
	Lead     string
	Body     []string // 正文段落
	CTA      string   // 按钮文字（有链接时用）
	Footer   string   // 卡片末尾的一句
	Greeting string
	SignOff  string // 落款，{app} 会替换
}

// Labels 邮件详情表里的字段名
type Labels struct {
	Account, Level, Type, Reason, Details, Until, Reference, Reply, Permanent, LevelApp, LevelPlatform, NoReply string
}

var labels = map[string]Labels{
	LocaleEN: {"Account", "Scope", "Duration", "Reason", "Details", "Suspended until", "Appeal reference", "Message from our team",
		"Permanent", "This app only", "All of our services", "This is an automated message. Please do not reply to this email."},
	LocaleZhCN: {"账号", "范围", "期限", "原因", "说明", "解封时间", "申诉编号", "审核回复",
		"永久", "仅本应用", "所有服务", "这是一封系统邮件，请勿直接回复。"},
	LocaleZhTW: {"帳號", "範圍", "期限", "原因", "說明", "解除停權時間", "申訴編號", "審核回覆",
		"永久", "僅本應用程式", "所有服務", "這是一封系統郵件，請勿直接回覆。"},
	LocaleJA: {"アカウント", "範囲", "期間", "理由", "詳細", "停止解除日時", "異議申し立て番号", "審査担当からのメッセージ",
		"無期限", "このアプリのみ", "すべてのサービス", "このメールは送信専用です。返信はできません。"},
	LocaleKO: {"계정", "범위", "기간", "사유", "상세 내용", "정지 해제 일시", "이의 신청 번호", "검토 담당자의 메시지",
		"영구", "이 앱만", "모든 서비스", "본 메일은 발신 전용입니다. 회신하지 마십시오."},
}

// LabelsFor 详情表字段名
func LabelsFor(locale string) Labels { return Pick(labels, locale) }

var notices = map[string]map[string]NoticeCopy{
	NoticeSuspended: {
		LocaleEN: {
			Subject: "Your account has been suspended",
			Eyebrow: "Account notice",
			Title:   "Your account has been suspended",
			Lead:    "We're writing to let you know that your {app} account has been suspended.",
			Body: []string{
				"Our Trust & Safety team identified activity on your account that violates our Terms of Service. As a result, you can no longer sign in to or use your account while the suspension is in effect.",
				"If you believe this decision was made in error, you can submit an appeal. Every appeal is reviewed, and we'll email you the outcome.",
			},
			CTA:      "Submit an appeal",
			Footer:   "To appeal from the app, open {app} and sign in. You'll be guided to the appeal form.",
			Greeting: "Hello,",
			SignOff:  "The {app} Team",
		},
		LocaleZhCN: {
			Subject: "您的账号已被封禁",
			Eyebrow: "账号通知",
			Title:   "您的账号已被封禁",
			Lead:    "特此通知，您的 {app} 账号已被封禁。",
			Body: []string{
				"我们的安全团队发现您的账号存在违反使用条款的行为。封禁生效期间，您将无法登录或使用该账号。",
				"如果您认为这一决定有误，可以提交申诉。每一份申诉都会经过审核，结果将通过邮件告知您。",
			},
			CTA:      "提交申诉",
			Footer:   "也可以打开 {app} 并尝试登录，按提示进入申诉页面。",
			Greeting: "您好：",
			SignOff:  "{app} 团队",
		},
		LocaleZhTW: {
			Subject: "您的帳號已被停權",
			Eyebrow: "帳號通知",
			Title:   "您的帳號已被停權",
			Lead:    "特此通知，您的 {app} 帳號已被停權。",
			Body: []string{
				"我們的安全團隊發現您的帳號存在違反使用條款的行為。停權期間，您將無法登入或使用該帳號。",
				"如果您認為這項決定有誤，可以提出申訴。每一份申訴都會經過審核，結果將以電子郵件通知您。",
			},
			CTA:      "提出申訴",
			Footer:   "也可以開啟 {app} 並嘗試登入，依照提示進入申訴頁面。",
			Greeting: "您好：",
			SignOff:  "{app} 團隊",
		},
		LocaleJA: {
			Subject: "アカウントが停止されました",
			Eyebrow: "アカウントに関するお知らせ",
			Title:   "アカウントが停止されました",
			Lead:    "お客様の {app} アカウントが停止されたことをお知らせします。",
			Body: []string{
				"当社の安全対策チームは、お客様のアカウントで利用規約に違反する行為を確認しました。停止期間中は、アカウントにログインしたり、アカウントを利用したりすることはできません。",
				"この決定が誤りであるとお考えの場合は、異議を申し立てることができます。すべての申し立てを審査し、結果をメールでお知らせします。",
			},
			CTA:      "異議を申し立てる",
			Footer:   "{app} を開いてログインすると、異議申し立てのページに案内されます。",
			Greeting: "お客様各位",
			SignOff:  "{app} チーム",
		},
		LocaleKO: {
			Subject: "계정이 정지되었습니다",
			Eyebrow: "계정 안내",
			Title:   "계정이 정지되었습니다",
			Lead:    "고객님의 {app} 계정이 정지되었음을 알려 드립니다.",
			Body: []string{
				"당사 안전팀은 고객님의 계정에서 이용약관을 위반하는 활동을 확인했습니다. 정지 기간 동안에는 계정에 로그인하거나 계정을 이용할 수 없습니다.",
				"이 결정이 잘못되었다고 생각되시면 이의를 신청하실 수 있습니다. 모든 이의 신청을 검토한 후 결과를 이메일로 알려 드립니다.",
			},
			CTA:      "이의 신청하기",
			Footer:   "{app} 앱을 열고 로그인하시면 이의 신청 페이지로 안내됩니다.",
			Greeting: "안녕하세요.",
			SignOff:  "{app} 팀 드림",
		},
	},
	NoticeReinstated: {
		LocaleEN: {
			Subject: "Your account has been reinstated",
			Eyebrow: "Account notice",
			Title:   "Your account has been reinstated",
			Lead:    "Your {app} account is no longer suspended.",
			Body: []string{
				"After review, we've lifted the suspension on your account. You can sign in and use {app} as usual.",
				"Thank you for your patience while we looked into this.",
			},
			Footer:   "If you didn't expect this email, you can ignore it.",
			Greeting: "Hello,",
			SignOff:  "The {app} Team",
		},
		LocaleZhCN: {
			Subject: "您的账号已恢复",
			Eyebrow: "账号通知",
			Title:   "您的账号已恢复",
			Lead:    "您的 {app} 账号已解除封禁。",
			Body: []string{
				"经过审核，我们已解除对您账号的封禁。您现在可以正常登录并使用 {app}。",
				"感谢您在此期间的耐心等待。",
			},
			Footer:   "如果您没有预期收到这封邮件，可以忽略它。",
			Greeting: "您好：",
			SignOff:  "{app} 团队",
		},
		LocaleZhTW: {
			Subject: "您的帳號已恢復",
			Eyebrow: "帳號通知",
			Title:   "您的帳號已恢復",
			Lead:    "您的 {app} 帳號已解除停權。",
			Body: []string{
				"經過審核，我們已解除您帳號的停權。您現在可以正常登入並使用 {app}。",
				"感謝您在此期間的耐心等候。",
			},
			Footer:   "如果您沒有預期收到這封郵件，可以忽略它。",
			Greeting: "您好：",
			SignOff:  "{app} 團隊",
		},
		LocaleJA: {
			Subject: "アカウントの停止が解除されました",
			Eyebrow: "アカウントに関するお知らせ",
			Title:   "アカウントの停止が解除されました",
			Lead:    "お客様の {app} アカウントの停止は解除されました。",
			Body: []string{
				"審査の結果、お客様のアカウントの停止を解除しました。これまでどおり {app} にログインしてご利用いただけます。",
				"確認にお時間をいただき、ありがとうございました。",
			},
			Footer:   "このメールに心当たりがない場合は、破棄してください。",
			Greeting: "お客様各位",
			SignOff:  "{app} チーム",
		},
		LocaleKO: {
			Subject: "계정 정지가 해제되었습니다",
			Eyebrow: "계정 안내",
			Title:   "계정 정지가 해제되었습니다",
			Lead:    "고객님의 {app} 계정 정지가 해제되었습니다.",
			Body: []string{
				"검토 결과 고객님 계정의 정지를 해제했습니다. 이제 평소처럼 {app}에 로그인하여 이용하실 수 있습니다.",
				"확인하는 동안 기다려 주셔서 감사합니다.",
			},
			Footer:   "이 메일을 예상하지 못하셨다면 무시하셔도 됩니다.",
			Greeting: "안녕하세요.",
			SignOff:  "{app} 팀 드림",
		},
	},
	NoticeAppealReceived: {
		LocaleEN: {
			Subject: "We've received your appeal",
			Eyebrow: "Appeal",
			Title:   "We've received your appeal",
			Lead:    "Thanks for reaching out. We've received your appeal regarding the suspension of your {app} account.",
			Body: []string{
				"Our team will review your appeal and email you once a decision has been made. You don't need to submit another appeal in the meantime.",
			},
			Footer:   "Keep your appeal reference for any follow-up.",
			Greeting: "Hello,",
			SignOff:  "The {app} Team",
		},
		LocaleZhCN: {
			Subject: "我们已收到您的申诉",
			Eyebrow: "申诉",
			Title:   "我们已收到您的申诉",
			Lead:    "感谢您的反馈。我们已收到您就 {app} 账号封禁提交的申诉。",
			Body: []string{
				"我们会审核您的申诉，有结果后将通过邮件告知您。在此期间无需重复提交。",
			},
			Footer:   "请保留申诉编号，以便后续查询。",
			Greeting: "您好：",
			SignOff:  "{app} 团队",
		},
		LocaleZhTW: {
			Subject: "我們已收到您的申訴",
			Eyebrow: "申訴",
			Title:   "我們已收到您的申訴",
			Lead:    "感謝您的回覆。我們已收到您就 {app} 帳號停權提出的申訴。",
			Body: []string{
				"我們會審核您的申訴，有結果後將以電子郵件通知您。在此期間無須重複提交。",
			},
			Footer:   "請保留申訴編號，以便後續查詢。",
			Greeting: "您好：",
			SignOff:  "{app} 團隊",
		},
		LocaleJA: {
			Subject: "異議申し立てを受け付けました",
			Eyebrow: "異議申し立て",
			Title:   "異議申し立てを受け付けました",
			Lead:    "ご連絡ありがとうございます。{app} アカウントの停止に関する異議申し立てを受け付けました。",
			Body: []string{
				"申し立ての内容を審査し、結果が出しだいメールでお知らせします。その間に再度申し立てを行う必要はありません。",
			},
			Footer:   "お問い合わせの際は、異議申し立て番号をお知らせください。",
			Greeting: "お客様各位",
			SignOff:  "{app} チーム",
		},
		LocaleKO: {
			Subject: "이의 신청이 접수되었습니다",
			Eyebrow: "이의 신청",
			Title:   "이의 신청이 접수되었습니다",
			Lead:    "연락해 주셔서 감사합니다. {app} 계정 정지에 대한 이의 신청이 접수되었습니다.",
			Body: []string{
				"이의 신청을 검토한 후 결정이 나는 대로 이메일로 알려 드리겠습니다. 그동안 다시 신청하실 필요는 없습니다.",
			},
			Footer:   "문의하실 때를 위해 이의 신청 번호를 보관해 주십시오.",
			Greeting: "안녕하세요.",
			SignOff:  "{app} 팀 드림",
		},
	},
	NoticeAppealUpheld: {
		LocaleEN: {
			Subject: "An update on your appeal",
			Eyebrow: "Appeal",
			Title:   "Your appeal was not approved",
			Lead:    "We've carefully reviewed your appeal regarding the suspension of your {app} account.",
			Body: []string{
				"After review, we've determined that the original decision was correct. Your account will remain suspended.",
			},
			Footer:   "You may submit another appeal after {date} if you have new information.",
			Greeting: "Hello,",
			SignOff:  "The {app} Team",
		},
		LocaleZhCN: {
			Subject: "关于您申诉的处理结果",
			Eyebrow: "申诉",
			Title:   "您的申诉未获通过",
			Lead:    "我们已仔细审核了您就 {app} 账号封禁提交的申诉。",
			Body: []string{
				"经过审核，我们认为原决定是正确的，您的账号将继续保持封禁。",
			},
			Footer:   "如有新的补充信息，可在 {date} 之后再次提交申诉。",
			Greeting: "您好：",
			SignOff:  "{app} 团队",
		},
		LocaleZhTW: {
			Subject: "關於您申訴的處理結果",
			Eyebrow: "申訴",
			Title:   "您的申訴未獲通過",
			Lead:    "我們已仔細審核您就 {app} 帳號停權提出的申訴。",
			Body: []string{
				"經過審核，我們認為原決定是正確的，您的帳號將維持停權。",
			},
			Footer:   "如有新的補充資訊，可在 {date} 之後再次提出申訴。",
			Greeting: "您好：",
			SignOff:  "{app} 團隊",
		},
		LocaleJA: {
			Subject: "異議申し立ての審査結果について",
			Eyebrow: "異議申し立て",
			Title:   "異議申し立ては承認されませんでした",
			Lead:    "{app} アカウントの停止に関する異議申し立てを慎重に審査しました。",
			Body: []string{
				"審査の結果、当初の判断は適切であると判断しました。アカウントの停止は継続されます。",
			},
			Footer:   "新たな情報がある場合は、{date} 以降に再度申し立てを行うことができます。",
			Greeting: "お客様各位",
			SignOff:  "{app} チーム",
		},
		LocaleKO: {
			Subject: "이의 신청 검토 결과 안내",
			Eyebrow: "이의 신청",
			Title:   "이의 신청이 승인되지 않았습니다",
			Lead:    "{app} 계정 정지에 대한 이의 신청을 신중하게 검토했습니다.",
			Body: []string{
				"검토 결과 원래의 결정이 적절하다고 판단했습니다. 계정 정지는 유지됩니다.",
			},
			Footer:   "새로운 정보가 있으시면 {date} 이후에 다시 이의를 신청하실 수 있습니다.",
			Greeting: "안녕하세요.",
			SignOff:  "{app} 팀 드림",
		},
	},
}

// Notice 一类通知的文案，占位已替换
func Notice(kind, locale string, vars map[string]string) NoticeCopy {
	c := Pick(notices[kind], locale)
	r := replacer(vars)
	c.Subject, c.Eyebrow, c.Title, c.Lead = r.Replace(c.Subject), r.Replace(c.Eyebrow), r.Replace(c.Title), r.Replace(c.Lead)
	c.CTA, c.Footer, c.Greeting, c.SignOff = r.Replace(c.CTA), r.Replace(c.Footer), r.Replace(c.Greeting), r.Replace(c.SignOff)
	body := make([]string, len(c.Body))
	for i, b := range c.Body {
		body[i] = r.Replace(b)
	}
	c.Body = body
	return c
}

// PlatformScopeLine 全站封禁时补的一句
func PlatformScopeLine(locale string) string {
	return Pick(map[string]string{
		LocaleEN:   "This suspension applies to all of your accounts across our services, including accounts linked to the same email, phone number, sign-in identity or device.",
		LocaleZhCN: "此次封禁适用于您在我们所有服务中的账号，包括与同一邮箱、手机号、第三方登录身份或设备关联的账号。",
		LocaleZhTW: "此次停權適用於您在我們所有服務中的帳號，包括與同一電子郵件、手機號碼、第三方登入身分或裝置關聯的帳號。",
		LocaleJA:   "この停止は、同じメールアドレス、電話番号、ログイン ID、端末に関連付けられたアカウントを含め、当社のすべてのサービスにおけるお客様のアカウントに適用されます。",
		LocaleKO:   "이번 정지는 동일한 이메일, 전화번호, 로그인 계정 또는 기기와 연결된 계정을 포함하여 당사 모든 서비스의 고객님 계정에 적용됩니다.",
	}, locale)
}

// FinalDecisionLine 申诉次数用完时代替「可再次申诉」的那句
func FinalDecisionLine(locale string) string {
	return Pick(map[string]string{
		LocaleEN:   "This decision is final.",
		LocaleZhCN: "此为最终决定。",
		LocaleZhTW: "此為最終決定。",
		LocaleJA:   "この決定は最終的なものです。",
		LocaleKO:   "이 결정은 최종 결정입니다.",
	}, locale)
}

func replacer(vars map[string]string) *strings.Replacer {
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, "{"+k+"}", v)
	}
	return strings.NewReplacer(pairs...)
}
