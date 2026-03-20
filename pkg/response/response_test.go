package response

import "testing"

func TestSanitizeMessagePreservesBusiness4xxMessage(t *testing.T) {
	message := sanitizeMessage(401, "账号或密码错误")
	if message != "账号或密码错误" {
		t.Fatalf("unexpected message: %s", message)
	}
}

func TestSanitizeMessageKeepsServerErrorSanitized(t *testing.T) {
	message := sanitizeMessage(500, "sql: no rows in result set")
	if message != "服务暂时不可用" {
		t.Fatalf("unexpected message: %s", message)
	}
}
