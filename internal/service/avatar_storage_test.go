package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	avatardomain "aegis/internal/domain/avatar"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// 头像取图链路：数据库副本优先、对象存储兜底并补写副本、读失败时的降级。
//
// 这几条钉的都是「头像过一阵子就没了」的成因：对象存储的字节一丢（容器重建清空
// 本地存储）或读失败一次，此前的实现会把默认图带着这张头像的版本发出去，
// 并被客户端以 immutable 缓存一年。

type fakeAvatarObjects struct {
	objects map[string][]byte
	// failures 前几次读取返回错误，用来模拟偶发失败。
	failures int
	reads    int
}

func (f *fakeAvatarObjects) ReadObjectBytes(_ context.Context, _ int64, key string, _ int64) ([]byte, string, error) {
	f.reads++
	if f.failures > 0 {
		f.failures--
		return nil, "", apperrors.New(40481, http.StatusNotFound, "资源不可用")
	}
	data, ok := f.objects[key]
	if !ok {
		return nil, "", apperrors.New(40481, http.StatusNotFound, "资源不可用")
	}
	return data, "image/jpeg", nil
}

type fakeAvatarBlobs struct {
	blobs map[[2]int64][]byte
	saved int
}

func (f *fakeAvatarBlobs) GetAvatarBlob(_ context.Context, assetID int64, size int) (*avatardomain.Blob, error) {
	data, ok := f.blobs[[2]int64{assetID, int64(size)}]
	if !ok {
		return nil, nil
	}
	return &avatardomain.Blob{Size: size, ContentType: "image/jpeg", Data: data}, nil
}

func (f *fakeAvatarBlobs) SaveAvatarBlob(_ context.Context, assetID int64, blob avatardomain.Blob) error {
	key := [2]int64{assetID, int64(blob.Size)}
	if _, ok := f.blobs[key]; !ok {
		f.blobs[key] = blob.Data
		f.saved++
	}
	return nil
}

func newTestAvatarService(objects *fakeAvatarObjects, blobs *fakeAvatarBlobs) *AvatarService {
	svc := NewAvatarService(zap.NewNop(), nil, nil, nil, nil, nil, "test", AvatarSettings{SigningKey: "k"})
	svc.objects = objects
	svc.blobs = blobs
	return svc
}

func testAvatarTarget() avatarTarget {
	asset := &avatardomain.Asset{
		ID:          7,
		ConfigID:    3,
		BaseKey:     "avatars/apps/1/users/2/2026/10/10_avatar.jpg",
		ContentType: "image/jpeg",
		Checksum:    "0123456789abcdef0123456789abcdef",
		Variants: []avatardomain.Variant{
			{Size: 128, Key: "avatars/apps/1/users/2/2026/10/10_avatar_128.jpg", ContentType: "image/jpeg"},
			{Size: 256, Key: "avatars/apps/1/users/2/2026/10/10_avatar_256.jpg", ContentType: "image/jpeg"},
		},
	}
	return avatarTarget{
		Kind:     avatardomain.KindCustom,
		Asset:    asset,
		ConfigID: asset.ConfigID,
		Key:      asset.BaseKey,
		Version:  customAvatarVersion(asset, ""),
		Label:    "Alice",
	}
}

var testOwner = avatardomain.Owner{Type: avatardomain.OwnerUser, AppID: 1, ID: 2}

// 对象存储的字节没了（本地存储被重新部署清空），数据库副本照样把头像给出来。
func TestStoredAvatarIsServedFromDatabaseCopy(t *testing.T) {
	objects := &fakeAvatarObjects{objects: map[string][]byte{}}
	blobs := &fakeAvatarBlobs{blobs: map[[2]int64][]byte{{7, 256}: []byte("db-256")}}
	svc := newTestAvatarService(objects, blobs)

	image, err := svc.openTarget(context.Background(), testOwner, testAvatarTarget(), 256, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(image.Data) != "db-256" || image.Degraded {
		t.Fatalf("应从数据库副本取到 256 档，得到 %q（降级=%v）", image.Data, image.Degraded)
	}
	if objects.reads != 0 {
		t.Fatalf("有副本时不该去对象存储，读了 %d 次", objects.reads)
	}
}

// 本次改动之前上传、还没有副本的头像：从对象存储读到一次就补一份。
func TestStoredAvatarBackfillsDatabaseCopy(t *testing.T) {
	target := testAvatarTarget()
	objects := &fakeAvatarObjects{objects: map[string][]byte{target.Asset.Variants[1].Key: []byte("obj-256")}}
	blobs := &fakeAvatarBlobs{blobs: map[[2]int64][]byte{}}
	svc := newTestAvatarService(objects, blobs)

	image, err := svc.openTarget(context.Background(), testOwner, target, 256, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(image.Data) != "obj-256" {
		t.Fatalf("应从对象存储取到 256 档，得到 %q", image.Data)
	}
	if got := string(blobs.blobs[[2]int64{7, 256}]); got != "obj-256" || blobs.saved != 1 {
		t.Fatalf("读到之后应补写 256 档的副本，得到 %q（补写 %d 次）", got, blobs.saved)
	}
}

// 变体对象丢了不等于整张头像丢了：退到原图，而不是默认图。
func TestMissingVariantFallsBackToOriginal(t *testing.T) {
	target := testAvatarTarget()
	objects := &fakeAvatarObjects{objects: map[string][]byte{}}
	blobs := &fakeAvatarBlobs{blobs: map[[2]int64][]byte{{7, 0}: []byte("db-original")}}
	svc := newTestAvatarService(objects, blobs)

	image, err := svc.openTarget(context.Background(), testOwner, target, 128, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(image.Data) != "db-original" || image.Degraded {
		t.Fatalf("变体取不到时应退回原图，得到 %q（降级=%v）", image.Data, image.Degraded)
	}
}

// 偶发失败再试一次就过了，不该为此给出默认图。
func TestTransientObjectFailureIsRetried(t *testing.T) {
	target := testAvatarTarget()
	objects := &fakeAvatarObjects{failures: 1, objects: map[string][]byte{target.Asset.Variants[1].Key: []byte("obj-256")}}
	svc := newTestAvatarService(objects, &fakeAvatarBlobs{blobs: map[[2]int64][]byte{}})

	image, err := svc.openTarget(context.Background(), testOwner, target, 256, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(image.Data) != "obj-256" || image.Degraded {
		t.Fatalf("重试后应取到真正的头像，得到 %q（降级=%v）", image.Data, image.Degraded)
	}
}

// 实在取不到时给默认图，但它必须是降级的：没有 ETag，传输层据此禁止缓存。
// 此前它带着这张头像的版本与 ETag，被客户端以 immutable 缓存一年。
func TestUnavailableAvatarDegradesWithoutCacheableIdentity(t *testing.T) {
	svc := newTestAvatarService(&fakeAvatarObjects{objects: map[string][]byte{}}, &fakeAvatarBlobs{blobs: map[[2]int64][]byte{}})
	target := testAvatarTarget()

	image, err := svc.openTarget(context.Background(), testOwner, target, 256, "")
	if err != nil {
		t.Fatal(err)
	}
	if !image.Degraded || image.ETag != "" || len(image.Data) == 0 {
		t.Fatalf("应返回无 ETag 的降级默认图，得到 降级=%v ETag=%q 字节=%d", image.Degraded, image.ETag, len(image.Data))
	}

	// 降级图从来不带 ETag，所以手上拿着真版本 ETag 的客户端仍走条件请求 —— 它缓存的是真头像。
	cached, err := svc.openTarget(context.Background(), testOwner, target, 256, avatarETag(target.Version, 256))
	if err != nil {
		t.Fatal(err)
	}
	if !cached.NotModified {
		t.Fatal("ETag 与当前版本一致时应返回 304")
	}
}

func TestAvatarPicksPreferVariantThenOriginal(t *testing.T) {
	target := testAvatarTarget()

	picks := avatarPicks(target, 200)
	if len(picks) != 2 || picks[0].BlobSize != 256 || picks[1].BlobSize != 0 || picks[1].Key != target.Asset.BaseKey {
		t.Fatalf("应先取 256 档再退原图，得到 %+v", picks)
	}

	target.Asset.Animated = true
	if picks := avatarPicks(target, 128); len(picks) != 1 || picks[0].BlobSize != 0 {
		t.Fatalf("动图只取原图，得到 %+v", picks)
	}

	legacy := avatarTarget{Kind: avatardomain.KindCustom, ConfigID: 3, Key: "avatars/legacy.jpg"}
	if picks := avatarPicks(legacy, 128); len(picks) != 1 || picks[0].Key != "avatars/legacy.jpg" {
		t.Fatalf("没有资产记录时只取引用指向的那一个，得到 %+v", picks)
	}
}

// 自定义头像的版本串带缓存纪元：地址整体换一遍，冲掉已被错误缓存住的默认图。
func TestCustomAvatarVersionCarriesCacheEpoch(t *testing.T) {
	asset := &avatardomain.Asset{Checksum: "0123456789abcdef"}
	if got := customAvatarVersion(asset, "storage://3/a.jpg"); got != avatarCacheEpoch+".0123456789ab" {
		t.Fatalf("有内容摘要时取摘要前 12 位并加纪元，得到 %q", got)
	}
	legacy := customAvatarVersion(nil, "storage://3/a.jpg")
	if !strings.HasPrefix(legacy, avatarCacheEpoch+".") || legacy == avatarCacheEpoch+"." {
		t.Fatalf("没有资产记录时由引用派生并加纪元，得到 %q", legacy)
	}
	if customAvatarVersion(nil, "") != "" {
		t.Fatal("没有任何线索时不出版本")
	}
}

func TestAvatarBlobsKeepOnlyStoredVariants(t *testing.T) {
	processed := &processedAvatar{
		Base: renderedAvatarImage{ContentType: "image/png", Data: []byte("base")},
		Variants: []renderedAvatarImage{
			{Size: 64, ContentType: "image/png", Data: []byte("64")},
			{Size: 128, ContentType: "image/png", Data: []byte("128")},
		},
	}
	blobs := avatarBlobsOf(processed, []avatardomain.Variant{{Size: 128}})

	if len(blobs) != 2 || blobs[0].Size != 0 || blobs[1].Size != 128 {
		t.Fatalf("应存原图与成功入库的 128 档，得到 %+v", blobs)
	}
}

func TestErrCodeHelperStillRecognisesAppErrors(t *testing.T) {
	err := apperrors.New(41381, http.StatusRequestEntityTooLarge, "too large")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != 41381 {
		t.Fatal("读取上限错误应能被识别，读取重试据此不再重试")
	}
}
