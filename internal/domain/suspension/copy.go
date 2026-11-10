package suspension

// 对用户的标准文案。原因类别的说明在用户没有看到管理员写的对外说明（publicReason）时使用。

// ReasonCopy 一个原因类别的标题与说明
type ReasonCopy struct {
	Title       string
	Description string
}

var reasonCopy = map[string]map[string]ReasonCopy{
	"policy": {
		LocaleEN:   {"Violation of our Terms of Service", "Activity on your account violated our Terms of Service or community guidelines."},
		LocaleZhCN: {"违反使用条款", "账号上的行为违反了我们的使用条款或社区规范。"},
		LocaleZhTW: {"違反使用條款", "帳號上的行為違反了我們的使用條款或社群規範。"},
		LocaleJA:   {"利用規約への違反", "アカウントでの行為が利用規約またはコミュニティガイドラインに違反していました。"},
		LocaleKO:   {"이용약관 위반", "계정의 활동이 이용약관 또는 커뮤니티 가이드라인을 위반했습니다."},
	},
	"fraud": {
		LocaleEN:   {"Fraud or cheating", "We detected fraudulent activity or attempts to gain rewards or benefits through illegitimate means."},
		LocaleZhCN: {"欺诈或作弊", "检测到欺诈行为，或以不正当方式获取奖励、权益的行为。"},
		LocaleZhTW: {"詐欺或作弊", "偵測到詐欺行為，或以不正當方式獲取獎勵、權益的行為。"},
		LocaleJA:   {"不正行為・不正利用", "不正な手段で報酬や特典を得ようとする行為、または詐欺的な行為を検出しました。"},
		LocaleKO:   {"사기 또는 부정 행위", "부정한 방법으로 보상이나 혜택을 얻으려는 행위 또는 사기 행위가 감지되었습니다."},
	},
	"spam": {
		LocaleEN:   {"Spam or bulk registration", "We detected automated, bulk or duplicate account activity."},
		LocaleZhCN: {"垃圾信息或批量注册", "检测到自动化、批量或重复的账号行为。"},
		LocaleZhTW: {"垃圾訊息或大量註冊", "偵測到自動化、大量或重複的帳號行為。"},
		LocaleJA:   {"スパム・大量登録", "自動化された、または大量・重複したアカウントの操作を検出しました。"},
		LocaleKO:   {"스팸 또는 대량 가입", "자동화되었거나 대량·중복된 계정 활동이 감지되었습니다."},
	},
	"abuse": {
		LocaleEN:   {"Abuse of the service", "Your account was used in a way that disrupts the service or harms other users."},
		LocaleZhCN: {"滥用服务", "账号的使用方式干扰了服务的正常运行或损害了其他用户。"},
		LocaleZhTW: {"濫用服務", "帳號的使用方式干擾了服務的正常運作或損害了其他使用者。"},
		LocaleJA:   {"サービスの濫用", "サービスの運営を妨げる、または他のユーザーに害を及ぼす方法でアカウントが使用されました。"},
		LocaleKO:   {"서비스 남용", "계정이 서비스 운영을 방해하거나 다른 사용자에게 피해를 주는 방식으로 사용되었습니다."},
	},
	"security": {
		LocaleEN:   {"Account security risk", "We detected signs that your account may be compromised or used without your authorization."},
		LocaleZhCN: {"账号安全风险", "检测到账号可能已被盗用，或在未经授权的情况下被使用。"},
		LocaleZhTW: {"帳號安全風險", "偵測到帳號可能已被盜用，或在未經授權的情況下被使用。"},
		LocaleJA:   {"アカウントのセキュリティリスク", "アカウントが乗っ取られた、または許可なく使用されている可能性があります。"},
		LocaleKO:   {"계정 보안 위험", "계정이 도용되었거나 승인 없이 사용되고 있을 가능성이 감지되었습니다."},
	},
	"payment": {
		LocaleEN:   {"Payment risk", "We detected unusual or unauthorized payment activity associated with your account."},
		LocaleZhCN: {"支付风险", "检测到与账号相关的异常或未经授权的支付行为。"},
		LocaleZhTW: {"支付風險", "偵測到與帳號相關的異常或未經授權的支付行為。"},
		LocaleJA:   {"決済に関するリスク", "アカウントに関連する異常な、または許可されていない決済を検出しました。"},
		LocaleKO:   {"결제 위험", "계정과 관련된 비정상적이거나 승인되지 않은 결제 활동이 감지되었습니다."},
	},
	"other": {
		LocaleEN:   {"Other reasons", "Your account was suspended after a review by our team."},
		LocaleZhCN: {"其他原因", "账号经团队审核后被封禁。"},
		LocaleZhTW: {"其他原因", "帳號經團隊審核後被停權。"},
		LocaleJA:   {"その他の理由", "当チームによる審査の結果、アカウントが停止されました。"},
		LocaleKO:   {"기타 사유", "팀의 검토 결과 계정이 정지되었습니다."},
	},
}

// Reason 原因类别的文案；不认识的类别按 policy
func Reason(code, locale string) ReasonCopy {
	m, ok := reasonCopy[code]
	if !ok {
		m = reasonCopy["policy"]
	}
	return Pick(m, locale)
}

// ErrorMessage 40301 / 40302 的提示语
func ErrorMessage(permanent bool, locale string) string {
	if permanent {
		return Pick(map[string]string{
			LocaleEN:   "Your account has been suspended.",
			LocaleZhCN: "您的账号已被封禁。",
			LocaleZhTW: "您的帳號已被停權。",
			LocaleJA:   "お使いのアカウントは停止されています。",
			LocaleKO:   "계정이 정지되었습니다.",
		}, locale)
	}
	return Pick(map[string]string{
		LocaleEN:   "Your account has been temporarily suspended.",
		LocaleZhCN: "您的账号已被暂时封禁。",
		LocaleZhTW: "您的帳號已被暫時停權。",
		LocaleJA:   "お使いのアカウントは一時的に停止されています。",
		LocaleKO:   "계정이 일시적으로 정지되었습니다.",
	}, locale)
}
