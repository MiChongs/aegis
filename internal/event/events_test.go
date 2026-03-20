package event

import "testing"

func TestRealtimeSubjectRoundTrip(t *testing.T) {
	subject := SubjectRealtimeUser(10000, 31)
	appID, userID, ok := MatchRealtimeUserSubject(subject)
	if !ok {
		t.Fatalf("expected subject to match")
	}
	if appID != 10000 || userID != 31 {
		t.Fatalf("unexpected values: appID=%d userID=%d", appID, userID)
	}
}
