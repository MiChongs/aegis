package service

import "testing"

func TestScopeMatchesPreventsCrossAppPermissionUse(t *testing.T) {
	t.Parallel()

	appA := int64(101)
	appB := int64(202)
	if !scopeMatches(&appA, &appA) {
		t.Fatal("同一应用作用域应匹配")
	}
	if scopeMatches(&appA, &appB) {
		t.Fatal("应用 A 的权限不得用于应用 B")
	}
	if scopeMatches(&appA, nil) {
		t.Fatal("应用级权限不得用于平台级操作")
	}
	if !scopeMatches(nil, &appA) {
		t.Fatal("平台级权限应覆盖应用级操作")
	}
}
