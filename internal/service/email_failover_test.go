package service

import (
	"errors"
	"net/http"
	"testing"

	emaildomain "aegis/internal/domain/email"
	apperrors "aegis/pkg/errors"
)

func failoverConfigs(ids ...int64) []*emaildomain.Config {
	out := make([]*emaildomain.Config, 0, len(ids))
	for _, id := range ids {
		out = append(out, &emaildomain.Config{ID: id})
	}
	return out
}

func TestDeliverAcrossPrimarySucceedsWithoutLookingUpFallbacks(t *testing.T) {
	lookedUp := false
	id, err := deliverAcross(&emaildomain.Config{ID: 1}, func() []*emaildomain.Config {
		lookedUp = true
		return nil
	}, func(config *emaildomain.Config) (string, bool, error) {
		return "m-1", false, nil
	}, nil)
	if err != nil || id != "m-1" {
		t.Fatalf("got %q, %v", id, err)
	}
	if lookedUp {
		t.Fatal("备用通道只应在主通道失败后才查询")
	}
}

func TestDeliverAcrossFailsOverOnChannelFailure(t *testing.T) {
	var tried []int64
	quota := apperrors.New(42974, http.StatusTooManyRequests, "Zeabur 邮件配额已用尽")
	id, err := deliverAcross(&emaildomain.Config{ID: 1}, func() []*emaildomain.Config { return failoverConfigs(2, 3) },
		func(config *emaildomain.Config) (string, bool, error) {
			tried = append(tried, config.ID)
			if config.ID == 3 {
				return "m-3", false, nil
			}
			return "", true, quota
		}, nil)
	if err != nil || id != "m-3" {
		t.Fatalf("got %q, %v", id, err)
	}
	if len(tried) != 3 || tried[0] != 1 || tried[1] != 2 || tried[2] != 3 {
		t.Fatalf("尝试顺序 = %v", tried)
	}
}

func TestDeliverAcrossStopsOnNonChannelFailure(t *testing.T) {
	suppressed := apperrors.New(42290, http.StatusUnprocessableEntity, "该邮箱已被暂停投递")
	calls := 0
	_, err := deliverAcross(&emaildomain.Config{ID: 1}, func() []*emaildomain.Config { return failoverConfigs(2) },
		func(config *emaildomain.Config) (string, bool, error) {
			calls++
			return "", false, suppressed
		}, nil)
	if !errors.Is(err, suppressed) {
		t.Fatalf("与通道无关的错误应原样返回，got %v", err)
	}
	if calls != 1 {
		t.Fatalf("不该切换通道，calls = %d", calls)
	}
}

// 2026-11 的事故：唯一一条通道配额用尽，用户看到的是「请求过于频繁」。
// 全部通道失败时必须报 50316 / 503，文案可公开，且不能再是 429。
func TestDeliverAcrossAllChannelsDownReportsDeliveryUnavailable(t *testing.T) {
	quota := apperrors.New(42974, http.StatusTooManyRequests, "Zeabur 邮件配额已用尽：quota exceeded")
	var lastFlags []bool
	_, err := deliverAcross(&emaildomain.Config{ID: 1}, nil,
		func(config *emaildomain.Config) (string, bool, error) { return "", true, quota },
		func(config *emaildomain.Config, err error, last bool) { lastFlags = append(lastFlags, last) })
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("got %T %v", err, err)
	}
	if appErr.Code != errCodeEmailDeliveryUnavailable || appErr.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("got code=%d status=%d", appErr.Code, appErr.HTTPStatus)
	}
	if !appErr.PublicMessage {
		t.Fatal("文案应标记为可公开，否则会被响应层换成通用句")
	}
	if len(lastFlags) != 1 || !lastFlags[0] {
		t.Fatalf("唯一通道失败时应回调一次且 last=true，got %v", lastFlags)
	}
}
