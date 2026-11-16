package contract

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// 流与主题。
const (
	// StreamEvents 客户端事件与推荐结果。按时间保留，消费落地后 ClickHouse 是权威存储。
	StreamEvents = "MUSIC"
	// StreamJobs 训练任务（工作队列：一条任务只被一个 trainer 取走）。
	StreamJobs = "MUSIC_JOBS"

	SubjectEventPrefix  = "music.event."  // + app_id
	SubjectServedPrefix = "music.served." // + app_id
	SubjectDeadPrefix   = "music.dead."   // + app_id，投递多次仍失败的事件
	SubjectTrainPrefix  = "music.job.train."

	// SubjectModelPublished 模型发布通知（普通 NATS 发布订阅，不进流）：music-rec 收到后热加载。
	SubjectModelPublished = "music.model.published"
	// SubjectConfigChanged 应用配置改了（music-rec 刷新开关与参数缓存）。
	SubjectConfigChanged = "music.config.changed"

	// ConsumerIngest 落地消费者（持久、多实例分摊）。
	ConsumerIngest = "music-ingest"
	// ConsumerTrainer 训练消费者。
	ConsumerTrainer = "music-trainer"

	// Duplicates 发布去重窗口：客户端整批重传落在窗口内的被 JetStream 丢弃。
	Duplicates = 10 * time.Minute
)

// EventSubject 某应用的事件主题。
func EventSubject(appID int64) string { return SubjectEventPrefix + strconv.FormatInt(appID, 10) }

// ServedSubject 某应用的推荐结果主题。
func ServedSubject(appID int64) string { return SubjectServedPrefix + strconv.FormatInt(appID, 10) }

// TrainSubject 某应用的训练任务主题。
func TrainSubject(appID int64) string { return SubjectTrainPrefix + strconv.FormatInt(appID, 10) }

// MsgID 事件的发布去重键。
func MsgID(appID, userID int64, eventID string) string {
	return strconv.FormatInt(appID, 10) + ":" + strconv.FormatInt(userID, 10) + ":" + eventID
}

// TrainJob 训练任务。
type TrainJob struct {
	RunID int64 `json:"run_id"`
	AppID int64 `json:"app_id"`
}

// ModelPublished 发布通知。
type ModelPublished struct {
	AppID int64 `json:"app_id"`
	RunID int64 `json:"run_id"`
}

// EnsureStreams 建流（可重复执行；配置变了就更新）。主服务与音乐服务启动时都会调，两边配置必须一致，
// 所以只能放在这里。
func EnsureStreams(ctx context.Context, js jetstream.JetStream) error {
	cfgs := []jetstream.StreamConfig{
		{
			Name:        StreamEvents,
			Description: "音乐个性化：客户端行为事件与推荐结果",
			Subjects:    []string{SubjectEventPrefix + ">", SubjectServedPrefix + ">", SubjectDeadPrefix + ">"},
			Retention:   jetstream.LimitsPolicy,
			Storage:     jetstream.FileStorage,
			MaxAge:      7 * 24 * time.Hour,
			MaxBytes:    8 << 30,
			Discard:     jetstream.DiscardOld,
			Duplicates:  Duplicates,
			Replicas:    1,
		},
		{
			Name:        StreamJobs,
			Description: "音乐个性化：训练任务",
			Subjects:    []string{SubjectTrainPrefix + ">"},
			Retention:   jetstream.WorkQueuePolicy,
			Storage:     jetstream.FileStorage,
			MaxAge:      24 * time.Hour,
			Duplicates:  time.Hour,
			Replicas:    1,
		},
	}
	for _, cfg := range cfgs {
		if _, err := js.CreateOrUpdateStream(ctx, cfg); err != nil {
			return fmt.Errorf("建流 %s 失败：%w", cfg.Name, err)
		}
	}
	return nil
}

// ErrNoStream 流不存在（音乐服务从没启动过、主服务也没建成功）。
var ErrNoStream = errors.New("music stream not found")
