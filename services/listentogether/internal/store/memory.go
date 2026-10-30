// Package store 房间存储：Redis（多实例共享，生产用）与进程内（单实例开发与测试用）。
package store

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"aegis/services/listentogether/internal/room"
)

// Memory 进程内存储。房间以 JSON 存放，读写与 Redis 版本同样经过序列化，行为一致。
type Memory struct {
	mu        sync.Mutex
	rooms     map[string][]byte
	codes     map[string]string
	users     map[string]string
	updated   map[string]map[string]int64
	chat      map[string][]room.ChatMessage
	instances map[string]memoryInstance
	locks     map[string]time.Time
	now       func() time.Time
}

type memoryInstance struct {
	info    room.InstanceInfo
	expires time.Time
}

// NewMemory 新建进程内存储。
func NewMemory() *Memory {
	return &Memory{
		rooms: map[string][]byte{}, codes: map[string]string{}, users: map[string]string{},
		updated: map[string]map[string]int64{}, chat: map[string][]room.ChatMessage{},
		instances: map[string]memoryInstance{}, locks: map[string]time.Time{}, now: time.Now,
	}
}

func roomKey(app, id string) string { return app + "/" + id }
func userKey(app string, userID int64) string {
	return app + "/" + itoa(userID)
}

func (m *Memory) CreateRoom(_ context.Context, r *room.Room) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	codeKey := r.AppKey + "/" + r.Code
	if _, taken := m.codes[codeKey]; taken {
		return room.ErrCodeTaken
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	m.codes[codeKey] = r.ID
	m.rooms[roomKey(r.AppKey, r.ID)] = raw
	m.touch(r)
	return nil
}

func (m *Memory) touch(r *room.Room) {
	if m.updated[r.AppKey] == nil {
		m.updated[r.AppKey] = map[string]int64{}
	}
	m.updated[r.AppKey][r.ID] = r.UpdatedAt
}

func (m *Memory) LoadRoom(_ context.Context, app, id string) (*room.Room, error) {
	m.mu.Lock()
	raw, ok := m.rooms[roomKey(app, id)]
	m.mu.Unlock()
	if !ok {
		return nil, room.ErrNotFound
	}
	var r room.Room
	return &r, json.Unmarshal(raw, &r)
}

func (m *Memory) UpdateRoom(_ context.Context, app, id string, fn func(*room.Room) error) (*room.Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.rooms[roomKey(app, id)]
	if !ok {
		return nil, room.ErrNotFound
	}
	var r room.Room
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if err := fn(&r); err != nil {
		return nil, err
	}
	next, err := json.Marshal(&r)
	if err != nil {
		return nil, err
	}
	m.rooms[roomKey(app, id)] = next
	m.touch(&r)
	return &r, nil
}

func (m *Memory) DeleteRoom(_ context.Context, r *room.Room) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, roomKey(r.AppKey, r.ID))
	if m.codes[r.AppKey+"/"+r.Code] == r.ID {
		delete(m.codes, r.AppKey+"/"+r.Code)
	}
	delete(m.chat, roomKey(r.AppKey, r.ID))
	delete(m.updated[r.AppKey], r.ID)
	if len(m.updated[r.AppKey]) == 0 {
		delete(m.updated, r.AppKey)
	}
	for _, member := range r.Members {
		if m.users[userKey(r.AppKey, member.UserID)] == r.ID {
			delete(m.users, userKey(r.AppKey, member.UserID))
		}
	}
	return nil
}

func (m *Memory) FindByCode(_ context.Context, app, code string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.codes[app+"/"+code]
	if !ok {
		return "", room.ErrNotFound
	}
	return id, nil
}

func (m *Memory) UserRoom(_ context.Context, app string, userID int64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.users[userKey(app, userID)], nil
}

func (m *Memory) SetUserRoom(_ context.Context, app string, userID int64, roomID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[userKey(app, userID)] = roomID
	return nil
}

func (m *Memory) ClearUserRoom(_ context.Context, app string, userID int64, roomID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.users[userKey(app, userID)] == roomID {
		delete(m.users, userKey(app, userID))
	}
	return nil
}

func (m *Memory) ListRooms(_ context.Context, app string, offset, limit int) ([]string, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	type entry struct {
		id string
		at int64
	}
	var entries []entry
	for id, at := range m.updated[app] {
		entries = append(entries, entry{id, at})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].at != entries[j].at {
			return entries[i].at > entries[j].at
		}
		return entries[i].id < entries[j].id
	})
	total := len(entries)
	if offset >= total {
		return []string{}, total, nil
	}
	end := min(total, offset+limit)
	ids := make([]string, 0, end-offset)
	for _, item := range entries[offset:end] {
		ids = append(ids, item.id)
	}
	return ids, total, nil
}

func (m *Memory) Apps(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	apps := make([]string, 0, len(m.updated))
	for app := range m.updated {
		apps = append(apps, app)
	}
	sort.Strings(apps)
	return apps, nil
}

func (m *Memory) AppendChat(_ context.Context, app, roomID string, msg room.ChatMessage, keep int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := roomKey(app, roomID)
	list := append(m.chat[key], msg)
	if len(list) > keep {
		list = list[len(list)-keep:]
	}
	m.chat[key] = list
	return nil
}

func (m *Memory) RecentChat(_ context.Context, app, roomID string, limit int) ([]room.ChatMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.chat[roomKey(app, roomID)]
	if len(list) > limit {
		list = list[len(list)-limit:]
	}
	return append([]room.ChatMessage(nil), list...), nil
}

func (m *Memory) Heartbeat(_ context.Context, info room.InstanceInfo, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.instances[info.ID] = memoryInstance{info: info, expires: m.now().Add(ttl)}
	return nil
}

func (m *Memory) Instances(_ context.Context) ([]room.InstanceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	out := make([]room.InstanceInfo, 0, len(m.instances))
	for id, item := range m.instances {
		if now.After(item.expires) {
			delete(m.instances, id)
			continue
		}
		out = append(out, item.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) RemoveInstance(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.instances, id)
	return nil
}

func (m *Memory) TryLock(_ context.Context, key string, ttl time.Duration) (func(), bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if expires, held := m.locks[key]; held && now.Before(expires) {
		return nil, false, nil
	}
	m.locks[key] = now.Add(ttl)
	return func() {
		m.mu.Lock()
		delete(m.locks, key)
		m.mu.Unlock()
	}, true, nil
}

var _ room.Repository = (*Memory)(nil)
