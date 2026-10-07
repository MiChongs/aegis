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

func TestRealtimeAppSubjectRoundTrip(t *testing.T) {
	appID, ok := MatchRealtimeAppSubject(SubjectRealtimeApp(10000))
	if !ok || appID != 10000 {
		t.Fatalf("unexpected match: ok=%v appID=%d", ok, appID)
	}
	// 用户级主题不能被当成应用级广播，否则一条私信会投给整个应用
	if _, ok := MatchRealtimeAppSubject(SubjectRealtimeUser(10000, 31)); ok {
		t.Fatalf("user subject must not match app subject")
	}
}
