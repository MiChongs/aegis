package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

func TestValidateAdjustAmount(t *testing.T) {
	ok := []string{"20", "-20.5", "0.01", "100000000", "-100000000.00", "12.30"}
	bad := []string{"0.001", "-1.239", "100000000.01", "99999999999999999"}
	for _, raw := range ok {
		if err := validateAdjustAmount(decimal.RequireFromString(raw)); err != nil {
			t.Errorf("%s should pass: %v", raw, err)
		}
	}
	for _, raw := range bad {
		if err := validateAdjustAmount(decimal.RequireFromString(raw)); err == nil {
			t.Errorf("%s should be rejected", raw)
		}
	}
}

// 数据库溢出必须翻成业务错误，不能把 SQL 报错原样给到前端。
func TestTranslateWalletErrorNumericOverflow(t *testing.T) {
	overflow := fmt.Errorf("apply wallet change: %w", &pgconn.PgError{Code: "22003", Message: "numeric field overflow"})
	_, err := (&WalletService{}).translateWalletError(nil, overflow)
	if err == nil || err.Error() == overflow.Error() {
		t.Fatalf("overflow should be translated, got %v", err)
	}
	if !isNumericOverflow(overflow) || isNumericOverflow(errors.New("other")) {
		t.Fatal("isNumericOverflow misclassified")
	}
}
