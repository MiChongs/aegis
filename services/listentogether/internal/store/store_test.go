package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"aegis/services/listentogether/internal/room"
	"aegis/services/listentogether/internal/store"
)

// 两种实现跑同一套契约测试：内存版是 Redis 版的行为参照。
func implementations(t *testing.T) map[string]room.Repository {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return map[string]room.Repository{
		"memory": store.NewMemory(),
		"redis":  store.NewRedis(client, "lt:"),
	}
}

func sampleRoom(id, code string, members ...int64) *room.Room {
	r := &room.Room{ID: id, AppKey: "app", Code: code, HostID: members[0], UpdatedAt: time.Now().UnixMilli()}
	for _, uid := range members {
		r.Members = append(r.Members, &room.Member{UserID: uid, Role: room.RoleMember})
	}
	return r
}

func TestRepositoryContract(t *testing.T) {
	for name, repo := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			r := sampleRoom("r1", "123456", 1, 2)
			if err := repo.CreateRoom(ctx, r); err != nil {
				t.Fatal(err)
			}
			if err := repo.CreateRoom(ctx, sampleRoom("r2", "123456", 3)); !errors.Is(err, room.ErrCodeTaken) {
				t.Fatalf("duplicate code: %v", err)
			}
			if id, err := repo.FindByCode(ctx, "app", "123456"); err != nil || id != "r1" {
				t.Fatalf("find by code = %q, %v", id, err)
			}
			updated, err := repo.UpdateRoom(ctx, "app", "r1", func(r *room.Room) error {
				r.Rev = 7
				r.UpdatedAt++
				return nil
			})
			if err != nil || updated.Rev != 7 {
				t.Fatalf("update = %+v, %v", updated, err)
			}
			sentinel := errors.New("abort")
			if _, err := repo.UpdateRoom(ctx, "app", "r1", func(*room.Room) error { return sentinel }); !errors.Is(err, sentinel) {
				t.Fatalf("fn error must propagate: %v", err)
			}
			if loaded, _ := repo.LoadRoom(ctx, "app", "r1"); loaded.Rev != 7 {
				t.Fatal("aborted update must not be written")
			}
			if _, err := repo.UpdateRoom(ctx, "app", "missing", func(*room.Room) error { return nil }); !errors.Is(err, room.ErrNotFound) {
				t.Fatalf("missing room: %v", err)
			}

			_ = repo.SetUserRoom(ctx, "app", 1, "r1")
			_ = repo.SetUserRoom(ctx, "app", 2, "other")
			_ = repo.ClearUserRoom(ctx, "app", 1, "wrong")
			if id, _ := repo.UserRoom(ctx, "app", 1); id != "r1" {
				t.Fatal("clear with a different room must keep the mapping")
			}

			ids, total, err := repo.ListRooms(ctx, "app", 0, 10)
			if err != nil || total != 1 || len(ids) != 1 {
				t.Fatalf("list = %v %d %v", ids, total, err)
			}
			if apps, _ := repo.Apps(ctx); len(apps) != 1 || apps[0] != "app" {
				t.Fatalf("apps = %v", apps)
			}

			for i := 0; i < 5; i++ {
				_ = repo.AppendChat(ctx, "app", "r1", room.ChatMessage{ID: string(rune('a' + i)), Text: "x"}, 3)
			}
			chat, _ := repo.RecentChat(ctx, "app", "r1", 10)
			if len(chat) != 3 || chat[0].ID != "c" || chat[2].ID != "e" {
				t.Fatalf("chat = %+v", chat)
			}

			if err := repo.DeleteRoom(ctx, r); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.LoadRoom(ctx, "app", "r1"); !errors.Is(err, room.ErrNotFound) {
				t.Fatal("room must be gone")
			}
			if _, err := repo.FindByCode(ctx, "app", "123456"); !errors.Is(err, room.ErrNotFound) {
				t.Fatal("code must be released")
			}
			if id, _ := repo.UserRoom(ctx, "app", 1); id != "" {
				t.Fatal("member mapping must be cleared")
			}
			if id, _ := repo.UserRoom(ctx, "app", 2); id != "other" {
				t.Fatal("mapping pointing elsewhere must survive")
			}

			release, ok, err := repo.TryLock(ctx, "sweep", time.Minute)
			if err != nil || !ok {
				t.Fatalf("first lock: %v %v", ok, err)
			}
			if _, ok, _ := repo.TryLock(ctx, "sweep", time.Minute); ok {
				t.Fatal("lock must be exclusive")
			}
			release()
			if _, ok, _ := repo.TryLock(ctx, "sweep", time.Minute); !ok {
				t.Fatal("released lock must be obtainable")
			}

			if err := repo.Heartbeat(ctx, room.InstanceInfo{ID: "i1", Connections: 3}, time.Minute); err != nil {
				t.Fatal(err)
			}
			if list, _ := repo.Instances(ctx); len(list) != 1 || list[0].Connections != 3 {
				t.Fatalf("instances = %+v", list)
			}
			_ = repo.RemoveInstance(ctx, "i1")
			if list, _ := repo.Instances(ctx); len(list) != 0 {
				t.Fatal("instance must be removed")
			}
		})
	}
}

// 并发改同一个房间：乐观并发不能丢更新。
func TestRedisConcurrentUpdatesDoNotLoseWrites(t *testing.T) {
	repo := implementations(t)["redis"]
	ctx := context.Background()
	if err := repo.CreateRoom(ctx, sampleRoom("r1", "654321", 1)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if _, err := repo.UpdateRoom(ctx, "app", "r1", func(r *room.Room) error { r.Rev++; return nil }); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	r, _ := repo.LoadRoom(ctx, "app", "r1")
	if r.Rev != 80 {
		t.Fatalf("rev = %d, want 80", r.Rev)
	}
}
