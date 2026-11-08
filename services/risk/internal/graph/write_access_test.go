package graph

import (
	"strings"
	"testing"

	"aegis/services/risk/events"
)

func TestStatementsIdentityClientPasskey(t *testing.T) {
	base := events.Event{AppID: 10000, UserID: 7, TS: 1_700_000_000_000}
	e := base
	e.Type = events.TypeOAuthLogin
	e.Props = map[string]any{events.PropIdentityProvider: "qq", events.PropIdentityKey: events.Digest("U1"), events.PropIdentityNickname: "川意"}
	st := Statements(e)
	if !strings.Contains(st, "bound_identity") || !strings.Contains(st, IdentityVID("qq", "U1")) {
		t.Fatalf("identity edge missing: %s", st)
	}
	e.Type = events.TypeOAuthUnbind
	if strings.Contains(Statements(e), "bound_identity") {
		t.Fatal("unbind must not add a binding edge")
	}
	e = base
	e.Type = events.TypeOAuth2Authorize
	e.Props = map[string]any{events.PropClientID: "cli-1", events.PropClientName: "某应用"}
	if st := Statements(e); !strings.Contains(st, "authorized") || !strings.Contains(st, ClientVID("cli-1")) {
		t.Fatalf("authorized edge missing: %s", st)
	}
	e = base
	e.Type = events.TypePasskeyRegister
	e.Props = map[string]any{events.PropPasskeyKey: events.Digest("a1b2"), events.PropPasskeyName: "iPhone"}
	if st := Statements(e); !strings.Contains(st, "has_passkey") || !strings.Contains(st, PasskeyVID("a1b2")) {
		t.Fatalf("passkey edge missing: %s", st)
	}
}
