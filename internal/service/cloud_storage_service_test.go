package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	cloudstorage "aegis/internal/domain/cloudstorage"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 云存储对客户端可见的每一个错误码都必须登记在网关错误目录里。
//
// 目录随 /config 下发，客户端靠它决定「这个错能不能重试、该给用户看什么」；
// 漏登记的码在客户端只是一个不认识的数字，SDK 会把它当成通用业务错误。
func TestCloudErrorCodesAreInGatewayCatalog(t *testing.T) {
	t.Parallel()

	registered := map[int]bool{}
	for _, item := range gatewayErrors {
		registered[item.Code] = true
	}
	for _, code := range []int{
		errCodeCloudDisabled, errCodeCloudFrozen, errCodeCloudNamespaceDenied,
		errCodeCloudItemNotFound, errCodeCloudRevisionNotFound, errCodeCloudRevisionConflict,
		errCodeCloudNotInTrash, errCodeCloudQuotaExceeded, errCodeCloudItemTooLarge,
		errCodeCloudItemLimit, errCodeCloudInvalidNamespace, errCodeCloudInvalidKey,
		errCodeCloudInvalidContent, errCodeCloudInvalidMetadata, errCodeCloudContentUnreadable,
	} {
		if !registered[code] {
			t.Errorf("云存储错误码 %d 没有登记在网关错误目录（auth_protocol_catalog.go）里", code)
		}
	}
}

func TestMapCloudErrorTranslatesRepositoryErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err    error
		code   int
		status int
	}{
		{pgrepo.ErrCloudItemNotFound, errCodeCloudItemNotFound, http.StatusNotFound},
		{pgrepo.ErrCloudRevisionNotFound, errCodeCloudRevisionNotFound, http.StatusNotFound},
		{pgrepo.ErrCloudFrozen, errCodeCloudFrozen, http.StatusForbidden},
		{pgrepo.ErrCloudItemLimit, errCodeCloudItemLimit, http.StatusRequestEntityTooLarge},
		{pgrepo.ErrCloudNotInTrash, errCodeCloudNotInTrash, http.StatusConflict},
		{&pgrepo.CloudConflictError{Current: 3, Exists: true}, errCodeCloudRevisionConflict, http.StatusConflict},
		{&pgrepo.CloudQuotaError{Used: 10, Quota: 20, Need: 15}, errCodeCloudQuotaExceeded, http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		mapped := mapCloudError(tc.err)
		appErr, ok := errors.AsType[*apperrors.AppError](mapped)
		if !ok {
			t.Errorf("%v 应当被翻译成 AppError，得到 %T", tc.err, mapped)
			continue
		}
		if appErr.Code != tc.code || appErr.HTTPStatus != tc.status {
			t.Errorf("%v → %d/%d，期望 %d/%d", tc.err, appErr.Code, appErr.HTTPStatus, tc.code, tc.status)
		}
	}
	// 不认识的错误原样透传，不能被吞成某个业务码。
	plain := errors.New("boom")
	if mapCloudError(plain) != plain {
		t.Error("未知错误应当原样返回")
	}
}

func TestLimitsCapJSONWritesAtItemLimit(t *testing.T) {
	t.Parallel()

	cfg := cloudstorage.DefaultConfig(1)
	limits := limitsOf(cfg, cfg.QuotaBytes)
	if limits.JSONWriteBytes != cfg.MaxItemBytes {
		t.Errorf("单条目上限小于 JSON 写入上限时应取前者，得到 %d", limits.JSONWriteBytes)
	}
	cfg.MaxItemBytes = cloudstorage.MaxItemBytesCap
	if got := limitsOf(cfg, cfg.QuotaBytes).JSONWriteBytes; got != cloudstorage.JSONWriteLimit {
		t.Errorf("JSON 写入不能超过网关请求体能装下的上限，得到 %d", got)
	}
}

func TestDecorateItemComputesPurgeTime(t *testing.T) {
	t.Parallel()

	cfg := cloudstorage.DefaultConfig(1)
	cfg.TrashRetentionDays = 7
	deletedAt := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	item := &cloudstorage.Item{DeletedAt: &deletedAt}
	decorateItem(item, cfg)
	if item.PurgeAt == nil || !item.PurgeAt.Equal(deletedAt.AddDate(0, 0, 7)) {
		t.Fatalf("回收站条目的清除时间应为删除时间加保留天数，得到 %v", item.PurgeAt)
	}
	live := &cloudstorage.Item{}
	decorateItem(live, cfg)
	if live.PurgeAt != nil {
		t.Error("不在回收站里的条目不应有清除时间")
	}
}

func TestValidateCloudMetadataLimit(t *testing.T) {
	t.Parallel()

	if err := validateCloudMetadata(map[string]any{"device": "pixel", "count": 3}); err != nil {
		t.Fatalf("小元数据应当通过：%v", err)
	}
	large := map[string]any{"blob": string(make([]byte, cloudMetadataLimit))}
	if err := validateCloudMetadata(large); err == nil {
		t.Fatal("超过上限的元数据应当被拒绝")
	}
}
