package service

import (
	"fmt"
	"testing"

	avatardomain "aegis/internal/domain/avatar"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 头像字节副本的端到端集成测试：真实 Postgres，迁移跑两遍。
//
// 走一遍「上传落副本 → 对象存储整体丢失 → 仍从副本取到 → 换头像只留最近几张的副本 →
// 存量头像从对象存储读到一次后补写副本」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestAvatarBlobIntegration
func TestAvatarBlobIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	const appID = int64(31000)

	userID := insertTestUser(t, ctx, pool, appID, "avatar-a", "0")
	owner := avatardomain.Owner{Type: avatardomain.OwnerUser, AppID: appID, ID: userID}
	if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, userID); err != nil {
		t.Fatal(err)
	}

	upload := func(n int) *avatardomain.Asset {
		t.Helper()
		key := fmt.Sprintf("avatars/apps/%d/users/%d/2026/10/%02d_avatar.jpg", appID, userID, n)
		asset, err := pg.ReplaceAvatarAssetWithBlobs(ctx, avatardomain.Asset{
			Owner: owner, ConfigID: 1, BaseKey: key, ContentType: "image/jpeg",
			Checksum: fmt.Sprintf("%064d", n), Source: avatardomain.SourceUpload,
			Variants: []avatardomain.Variant{{Size: 256, Key: key + "_256", ContentType: "image/jpeg"}},
		}, []avatardomain.Blob{
			{Size: 0, ContentType: "image/jpeg", Data: []byte(fmt.Sprintf("original-%d", n))},
			{Size: 256, ContentType: "image/jpeg", Data: []byte(fmt.Sprintf("256-%d", n))},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := pg.SetUserProfileAvatar(ctx, userID, buildStorageReference(asset.ConfigID, asset.BaseKey)); err != nil {
			t.Fatal(err)
		}
		return asset
	}

	first := upload(1)

	// 对象存储里什么都没有（容器重建清空了本地存储），头像仍从副本取到。
	svc := NewAvatarService(zap.NewNop(), nil, nil, nil, pg, nil, "test", AvatarSettings{SigningKey: "k"})
	svc.objects = &fakeAvatarObjects{objects: map[string][]byte{}}
	token := svc.signer.EncodeOwner(owner)
	image, err := svc.OpenAvatar(ctx, token, 256, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(image.Data) != "256-1" || image.Degraded {
		t.Fatalf("对象存储清空后应从副本取到头像，得到 %q（降级=%v）", image.Data, image.Degraded)
	}
	if image.Version != customAvatarVersion(first, "") {
		t.Fatalf("取图的版本应与出网地址一致，得到 %q", image.Version)
	}

	// 连换六次头像：只有最近 BlobKeepPerOwner 张留副本，当前这张一定在。
	var latest *avatardomain.Asset
	for n := 2; n <= 7; n++ {
		latest = upload(n)
	}
	var withBlobs int
	if err := pool.QueryRow(ctx, `SELECT COUNT(DISTINCT b.asset_id) FROM avatar_asset_blobs b
JOIN avatar_assets a ON a.id = b.asset_id WHERE a.owner_id = $1`, userID).Scan(&withBlobs); err != nil {
		t.Fatal(err)
	}
	if withBlobs != avatardomain.BlobKeepPerOwner {
		t.Fatalf("应只保留最近 %d 张的副本，得到 %d 张", avatardomain.BlobKeepPerOwner, withBlobs)
	}
	if blob, err := pg.GetAvatarBlob(ctx, first.ID, 256); err != nil || blob != nil {
		t.Fatalf("最早那张的副本应已清掉，得到 %v / %v", blob, err)
	}
	if blob, err := pg.GetAvatarBlob(ctx, latest.ID, 0); err != nil || blob == nil || string(blob.Data) != "original-7" {
		t.Fatalf("当前头像的原图副本应在，得到 %v / %v", blob, err)
	}

	// 「换回」一张已经没有副本的历史头像：从对象存储读到一次就补上副本。
	if _, err := pg.ActivateAvatarAsset(ctx, owner, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := pg.SetUserProfileAvatar(ctx, userID, buildStorageReference(first.ConfigID, first.BaseKey)); err != nil {
		t.Fatal(err)
	}
	svc.objects = &fakeAvatarObjects{objects: map[string][]byte{first.BaseKey + "_256": []byte("restored-256")}}
	restored, err := svc.OpenAvatar(ctx, token, 256, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(restored.Data) != "restored-256" || restored.Degraded {
		t.Fatalf("应从对象存储取到换回的那张，得到 %q（降级=%v）", restored.Data, restored.Degraded)
	}
	if blob, err := pg.GetAvatarBlob(ctx, first.ID, 256); err != nil || blob == nil || string(blob.Data) != "restored-256" {
		t.Fatalf("读到之后应补写副本，得到 %v / %v", blob, err)
	}
	// 补写是幂等的：再存一次不报错、不覆盖。
	if err := pg.SaveAvatarBlob(ctx, first.ID, avatardomain.Blob{Size: 256, Data: []byte("other")}); err != nil {
		t.Fatal(err)
	}
	if blob, _ := pg.GetAvatarBlob(ctx, first.ID, 256); blob == nil || string(blob.Data) != "restored-256" {
		t.Fatalf("同一档副本不应被覆盖，得到 %v", blob)
	}

	// 存储完全取不到、也没有副本：降级默认图，不带 ETag。
	if _, err := pool.Exec(ctx, `DELETE FROM avatar_asset_blobs WHERE asset_id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}
	svc.objects = &fakeAvatarObjects{objects: map[string][]byte{}}
	degraded, err := svc.OpenAvatar(ctx, token, 256, "")
	if err != nil {
		t.Fatal(err)
	}
	if !degraded.Degraded || degraded.ETag != "" {
		t.Fatalf("没有任何字节时应降级且不带 ETag，得到 降级=%v ETag=%q", degraded.Degraded, degraded.ETag)
	}
}
