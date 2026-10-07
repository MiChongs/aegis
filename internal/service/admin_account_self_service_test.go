package service

import (
	"testing"

	admindomain "aegis/internal/domain/admin"
)

func TestValidateAdminAccountName(t *testing.T) {
	cases := map[string]error{
		"alice":                             nil,
		"Alice_01":                          nil,
		"a.b-c":                             nil,
		"ab":                                errAdminAccountInvalid, // 太短
		"1alice":                            errAdminAccountInvalid, // 数字开头
		"alice bob":                         errAdminAccountInvalid, // 空格
		"张三丰":                               errAdminAccountInvalid, // 非 ASCII
		"a234567890123456789012345678901x":  nil,                    // 32 位
		"a2345678901234567890123456789012x": errAdminAccountInvalid, // 33 位
		"Admin":                             errAdminAccountReserved,
		"ROOT":                              errAdminAccountReserved,
	}
	for input, want := range cases {
		if got := validateAdminAccountName(input); got != want {
			t.Errorf("validateAdminAccountName(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestValidateAdminPasswordStrength(t *testing.T) {
	cases := []struct {
		account, password string
		want              error
	}{
		{"alice", "correct9horse", nil},
		{"alice", "onlyletters", errAdminPasswordWeak},
		{"alice", "1234567890", errAdminPasswordWeak},
		{"alice", "xxALICE123", errAdminPasswordHasAccount},
		{"bob", "short1", nil}, // 长度错误由 validateAdminPassword 返回，下面单独断言
	}
	for _, tc := range cases {
		got := validateAdminPasswordStrength(tc.account, tc.password)
		if tc.password == "short1" {
			if got == nil {
				t.Errorf("过短的密码应被拒绝")
			}
			continue
		}
		if got != tc.want {
			t.Errorf("validateAdminPasswordStrength(%q, %q) = %v, want %v", tc.account, tc.password, got, tc.want)
		}
	}
}

func TestFillSelfServiceFlags(t *testing.T) {
	local := admindomain.Account{AuthSource: "password"}
	local.FillSelfServiceFlags()
	if !local.CanChangeAccount || !local.CanChangePassword {
		t.Fatalf("从未改名的本地账号应可改名、可改密码: %+v", local)
	}

	renamed := admindomain.Account{AuthSource: "password"}
	renamed.AccountChangedAt = &local.CreatedAt
	renamed.FillSelfServiceFlags()
	if renamed.CanChangeAccount || !renamed.CanChangePassword {
		t.Fatalf("改过名的账号不能再改名: %+v", renamed)
	}

	external := admindomain.Account{AuthSource: "ldap"}
	external.FillSelfServiceFlags()
	if external.CanChangeAccount || external.CanChangePassword {
		t.Fatalf("外部身份源账号两者都不能改: %+v", external)
	}
}
