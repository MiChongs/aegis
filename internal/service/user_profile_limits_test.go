package service

import (
	"strings"
	"testing"

	userdomain "aegis/internal/domain/user"
	apperrors "aegis/pkg/errors"
)

// 超长输入必须在服务层被挡下，而不是走到数据库那一层报
// `value too long for type character varying(N)`（22001）——
// 那句话既说不出是哪一项超了，也不该出现在用户面前。
func TestValidateProfileUpdateRejectsOverlongFields(t *testing.T) {
	cases := []struct {
		name  string
		input userdomain.ProfileUpdate
		want  string
	}{
		{
			name:  "昵称",
			input: userdomain.ProfileUpdate{Nickname: strings.Repeat("名", profileNicknameMaxLen+1)},
			want:  "昵称最多 128 个字符",
		},
		{
			name:  "邮箱",
			input: userdomain.ProfileUpdate{Email: strings.Repeat("a", profileEmailMaxLen+1)},
			want:  "邮箱最多 255 个字符",
		},
		{
			name:  "手机号",
			input: userdomain.ProfileUpdate{Phone: strings.Repeat("1", profilePhoneMaxLen+1)},
			want:  "手机号最多 32 个字符",
		},
		{
			name:  "头像地址",
			input: userdomain.ProfileUpdate{Avatar: strings.Repeat("u", profileAvatarMaxLen+1)},
			want:  "头像地址最多 1024 个字符",
		},
		{
			name:  "个人简介",
			input: userdomain.ProfileUpdate{Bio: strings.Repeat("字", profileBioMaxLen+1)},
			want:  "个人简介最多 2000 个字符",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProfileUpdate(tc.input)
			if err == nil {
				t.Fatalf("%s 超限时应当被拒绝", tc.name)
			}
			appErr, ok := err.(*apperrors.AppError)
			if !ok {
				t.Fatalf("应当返回业务错误，实际 %T", err)
			}
			if appErr.Code != 40000 {
				t.Fatalf("错误码应当是 40000，实际 %d", appErr.Code)
			}
			if appErr.Message != tc.want {
				t.Fatalf("错误文案应当点名字段与上限：want %q, got %q", tc.want, appErr.Message)
			}
		})
	}
}

// 上限按字符数而不是字节数：VARCHAR(n) 数的是码点，
// 按字节判会让 43 个汉字的昵称被误判成超限（129 字节）。
func TestValidateProfileUpdateCountsRunesNotBytes(t *testing.T) {
	input := userdomain.ProfileUpdate{Nickname: strings.Repeat("名", profileNicknameMaxLen)}

	if err := validateProfileUpdate(input); err != nil {
		t.Fatalf("正好等于上限的昵称应当通过，实际 %v", err)
	}
	if len(input.Nickname) <= profileNicknameMaxLen {
		t.Fatalf("这条用例的前提是字节数远大于字符数，实际字节数 %d", len(input.Nickname))
	}
}

func TestValidateProfileUpdateBoundsContacts(t *testing.T) {
	tooMany := make([]userdomain.ContactInfo, profileContactMaxCount+1)
	if err := validateProfileUpdate(userdomain.ProfileUpdate{Contacts: tooMany}); err == nil {
		t.Fatal("联系方式条数超限时应当被拒绝")
	}

	longValue := userdomain.ProfileUpdate{
		Contacts: []userdomain.ContactInfo{
			{Platform: "qq", Value: strings.Repeat("9", profileContactMaxLen+1)},
		},
	}
	err := validateProfileUpdate(longValue)
	if err == nil {
		t.Fatal("联系方式字段超限时应当被拒绝")
	}
	// 报错要指到具体哪一条，否则用户面对一屏联系方式无从下手。
	if !strings.Contains(err.Error(), "第 1 条") {
		t.Fatalf("错误文案应当点名是第几条，实际 %q", err.Error())
	}
}

func TestValidateProfileUpdateAcceptsOrdinaryInput(t *testing.T) {
	input := userdomain.ProfileUpdate{
		Nickname: "张三",
		Email:    "zhangsan@example.com",
		Phone:    "13800000000",
		Bio:      "这个人很懒，什么都没写。",
		Contacts: []userdomain.ContactInfo{{Platform: "wechat", Value: "zhangsan", Label: "工作"}},
	}

	if err := validateProfileUpdate(input); err != nil {
		t.Fatalf("常规输入不该被拒绝，实际 %v", err)
	}
}
