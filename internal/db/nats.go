package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"aegis/internal/config"
	"github.com/nats-io/nats.go"
)

func NewNATS(_ context.Context, cfg config.NATSConfig) (*nats.Conn, nats.JetStreamContext, error) {
	conn, err := nats.Connect(cfg.URL, nats.Name("aegis"), nats.Timeout(5*time.Second))
	if err != nil {
		return nil, nil, err
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	streamCfg := &nats.StreamConfig{
		Name:      cfg.StreamName,
		Subjects:  []string{"auth.>", "user.>", "firewall.>"},
		Retention: nats.LimitsPolicy,
		Storage:   nats.FileStorage,
	}
	if _, addErr := js.AddStream(streamCfg); addErr != nil {
		// 已存在是唯一可以忽略的失败：此时按新的 subjects 更新即可。
		//
		// 其余失败必须当场报出来。此前这里把错误整个吞掉，于是
		// 「JetStream 未对该账户启用」这类配置问题不会在连接阶段暴露，
		// 而是等到几十行之后 QueueSubscribe 时炸成
		// `nats: no stream matches subject` —— 那条消息既不提 JetStream、
		// 也不提账户，排查会从业务代码一路找回来。
		if !errors.Is(addErr, nats.ErrStreamNameAlreadyInUse) {
			conn.Close()
			return nil, nil, fmt.Errorf("创建 JetStream 流 %q 失败（订阅将无流可用）：%w", cfg.StreamName, addErr)
		}
		if _, updateErr := js.UpdateStream(streamCfg); updateErr != nil {
			conn.Close()
			return nil, nil, fmt.Errorf("更新 JetStream 流 %q 的主题失败：%w", cfg.StreamName, updateErr)
		}
	}
	return conn, js, nil
}
