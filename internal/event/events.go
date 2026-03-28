package event

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	SubjectAuthLoginAuditRequested = "auth.login.audit.requested"
	SubjectSessionAuditRequested   = "auth.session.audit.requested"
	SubjectUserMyAccessed          = "user.my.accessed"
	SubjectUserProfileRefresh      = "user.profile.cache.refresh.requested"
	SubjectUserSignedIn            = "user.signin.completed"
	SubjectUserAutoSignSync        = "user.autosign.sync.requested"
	SubjectFirewallBlocked         = "firewall.blocked"
	SubjectRealtimeUserPrefix      = "realtime.user"

	// 系统公告事件
	SubjectSystemAnnouncement = "system.announcement"

	// 抽奖系统事件
	SubjectLotteryDrawCompleted    = "lottery.draw.completed"
	SubjectLotterySeedCommitted    = "lottery.seed.committed"
	SubjectLotterySeedRevealed     = "lottery.seed.revealed"
	SubjectLotteryActivityCreated  = "lottery.activity.created"
	SubjectLotteryActivityUpdated  = "lottery.activity.updated"
)

func SubjectRealtimeUser(appID int64, userID int64) string {
	return fmt.Sprintf("%s.%d.%d", SubjectRealtimeUserPrefix, appID, userID)
}

func MatchRealtimeUserSubject(subject string) (int64, int64, bool) {
	parts := strings.Split(strings.TrimSpace(subject), ".")
	if len(parts) != 4 {
		return 0, 0, false
	}
	if strings.Join(parts[:2], ".") != SubjectRealtimeUserPrefix {
		return 0, 0, false
	}
	appID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	userID, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return appID, userID, true
}
