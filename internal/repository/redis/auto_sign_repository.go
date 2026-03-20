package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

type AutoSignEntry struct {
	UserID int64
	AppID  int64
}

type AutoSignRepository struct {
	client    *redislib.Client
	keyPrefix string
}

func NewAutoSignRepository(client *redislib.Client, keyPrefix string) *AutoSignRepository {
	return &AutoSignRepository{client: client, keyPrefix: keyPrefix}
}

func (r *AutoSignRepository) Schedule(ctx context.Context, appID int64, userID int64, dueAt time.Time) error {
	return r.client.ZAdd(ctx, r.scheduleKey(), redislib.Z{
		Score:  float64(dueAt.UnixMilli()),
		Member: r.member(appID, userID),
	}).Err()
}

func (r *AutoSignRepository) Remove(ctx context.Context, appID int64, userID int64) error {
	return r.client.ZRem(ctx, r.scheduleKey(), r.member(appID, userID)).Err()
}

func (r *AutoSignRepository) GetDue(ctx context.Context, now time.Time, limit int64) ([]AutoSignEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	values, err := r.client.ZRangeByScore(ctx, r.scheduleKey(), &redislib.ZRangeBy{
		Min:    "-inf",
		Max:    strconv.FormatInt(now.UnixMilli(), 10),
		Offset: 0,
		Count:  limit,
	}).Result()
	if err != nil {
		return nil, err
	}
	items := make([]AutoSignEntry, 0, len(values))
	for _, value := range values {
		entry, ok := parseAutoSignMember(value)
		if !ok {
			continue
		}
		items = append(items, entry)
	}
	return items, nil
}

func (r *AutoSignRepository) Count(ctx context.Context) (int64, error) {
	return r.client.ZCard(ctx, r.scheduleKey()).Result()
}

func (r *AutoSignRepository) scheduleKey() string {
	return fmt.Sprintf("%s:autosign:schedule", r.keyPrefix)
}

func (r *AutoSignRepository) member(appID int64, userID int64) string {
	return fmt.Sprintf("%d:%d", appID, userID)
}

func parseAutoSignMember(value string) (AutoSignEntry, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return AutoSignEntry{}, false
	}
	appID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return AutoSignEntry{}, false
	}
	userID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return AutoSignEntry{}, false
	}
	return AutoSignEntry{AppID: appID, UserID: userID}, true
}
