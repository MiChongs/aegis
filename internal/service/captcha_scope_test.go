package service

import (
	"testing"

	captchadomain "aegis/internal/domain/captcha"
)

func TestCaptchaRecordMatchesTenantPurposeAndScope(t *testing.T) {
	record := &captchadomain.CaptchaRecord{
		AppID: 7, Purpose: captchadomain.PurposeLogin, Scope: captchadomain.ScopeUser,
	}
	valid := captchadomain.VerifyRequest{
		ExpectedAppID: 7, ExpectedPurpose: captchadomain.PurposeLogin, ExpectedScope: captchadomain.ScopeUser,
	}
	if !captchaRecordMatchesRequest(record, valid) {
		t.Fatal("matching captcha context must be accepted")
	}
	for name, req := range map[string]captchadomain.VerifyRequest{
		"tenant":  {ExpectedAppID: 8},
		"purpose": {ExpectedPurpose: captchadomain.PurposeRegister},
		"scope":   {ExpectedScope: captchadomain.ScopeAdmin},
	} {
		if captchaRecordMatchesRequest(record, req) {
			t.Fatalf("mismatched %s must be rejected", name)
		}
	}
}
