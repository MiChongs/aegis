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
	SubjectRealtimeUserPrefix      = "realtime.user"
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
