package middleware

import "testing"

// TestSkipsBodyInspectionOnlyForAIRelay 只有 /api/ai/v1/ 免请求体检查，其余路径照常。
func TestSkipsBodyInspectionOnlyForAIRelay(t *testing.T) {
	for path, want := range map[string]bool{
		"/api/ai/v1/chat/completions":        true,
		"/api/ai/v1/messages":                true,
		"/api/ai/v1x/chat":                   false,
		"/api/apps/k/ai/v1/chat/completions": false,
		"/api/v1/apps/k/auth/login":          false,
	} {
		if got := skipsBodyInspection(path); got != want {
			t.Errorf("skipsBodyInspection(%q) = %v，想要 %v", path, got, want)
		}
	}
}
