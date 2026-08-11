package httptransport

import (
	"encoding/json"
	"testing"

	authprotocol "aegis/internal/domain/authprotocol"
)

func TestValidateRegistrationInputAllowsOnlyDeclaredMutableProfile(t *testing.T) {
	policy := &authprotocol.Policy{RegistrationSchema: []authprotocol.RegistrationField{
		{Name: "account", Type: "text", Required: true},
		{Name: "password", Type: "password", Required: true},
		{Name: "nickname", Type: "text"},
		{Name: "company", Type: "text", Required: true, Mutable: true},
	}}
	req := authprotocol.RegisterInput{
		Account: "alice", Password: "correct horse battery staple",
		Profile: json.RawMessage(`{"company":"Aegis Labs"}`),
	}
	profile, err := validateRegistrationInput(policy, req)
	if err != nil {
		t.Fatal(err)
	}
	if profile["company"] != "Aegis Labs" {
		t.Fatalf("unexpected profile: %#v", profile)
	}
}

func TestValidateRegistrationInputRejectsUnknownAndReservedFields(t *testing.T) {
	policy := &authprotocol.Policy{RegistrationSchema: []authprotocol.RegistrationField{
		{Name: "account", Type: "text", Required: true},
		{Name: "password", Type: "password", Required: true},
	}}
	for _, raw := range []string{`{"role":"admin"}`, `{"register_ip":"127.0.0.1"}`} {
		req := authprotocol.RegisterInput{
			Account: "alice", Password: "correct horse battery staple", Profile: json.RawMessage(raw),
		}
		if _, err := validateRegistrationInput(policy, req); err == nil {
			t.Fatalf("profile %s must be rejected", raw)
		}
	}
}
