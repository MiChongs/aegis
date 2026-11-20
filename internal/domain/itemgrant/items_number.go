package itemgrant

import "aegis/internal/domain/usernumber"

// MaxNumberRights 号码权益单笔上限（改号卡、靓号券、摇号次数）。
const MaxNumberRights = 1000

// validNumberText 指定号码：5–12 位、不以 0 开头的纯数字（应用的位数范围在发放时再判）。
func validNumberText(text string) bool {
	n, err := usernumber.ParseNumber(text)
	return err == nil && n >= 10000 && n <= 999999999999
}

// numberTicketName 靓号券的名称（带等级）。
func numberTicketName(level int64) string {
	return usernumber.RightName(usernumber.RightTicket, int(level))
}
