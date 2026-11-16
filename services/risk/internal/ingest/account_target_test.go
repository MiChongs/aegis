package ingest

import (
	"testing"

	"aegis/services/risk/internal/bundle"
)

func TestAccountListTarget(t *testing.T) {
	for in, want := range map[string][2]string{
		"#42":   {bundle.ListUser, "42"},
		"##42":  {bundle.ListAccount, "#42"},
		"alice": {bundle.ListAccount, "alice"},
	} {
		lt, v := accountListTarget(in)
		if lt != want[0] || v != want[1] {
			t.Errorf("%q → %s %q，想要 %v", in, lt, v, want)
		}
	}
}
