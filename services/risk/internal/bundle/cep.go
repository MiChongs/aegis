package bundle

import (
	"errors"
	"fmt"
)

// CEPPattern 行为序列模式（Flink CEP 作业从 risk-admin 拉取，Java 侧 Pattern.java 与此一一对应）
type CEPPattern struct {
	Key           string      `json:"key"`
	Name          string      `json:"name"`
	Description   string      `json:"description,omitempty"`
	Enabled       bool        `json:"enabled"`
	KeyBy         string      `json:"keyBy"` // user / device / ip / account
	AppIDs        []int64     `json:"appIds,omitempty"`
	Steps         []CEPStep   `json:"steps"`
	WithinSeconds int         `json:"withinSeconds"`
	Severity      string      `json:"severity"` // low / medium / high / critical
	Actions       []CEPAction `json:"actions"`
}

// CEPStep 模式里的一步
type CEPStep struct {
	Name       string      `json:"name"`
	Events     []string    `json:"events"`               // 事件类型（任一）
	Scenes     []string    `json:"scenes,omitempty"`     // 决策事件限定场景
	Actions    []string    `json:"actions,omitempty"`    // 决策事件限定处置
	Conditions []Condition `json:"conditions,omitempty"` // 字段条件（同流式特征）
	Times      int         `json:"times"`                // 至少发生几次（≥1）
	Contiguous bool        `json:"contiguous,omitempty"` // 与上一步之间不允许夹别的事件
	Distinct   string      `json:"distinct,omitempty"`   // times>1 时要求这个字段各不相同（如不同设备）
	Absent     bool        `json:"absent,omitempty"`     // 这一步在窗口内「没有发生」（notFollowedBy）
}

// CEPAction 命中后的处置
type CEPAction struct {
	Kind            string `json:"kind"`             // list_add / case
	Target          string `json:"target,omitempty"` // device / ip / account / user
	ListKind        string `json:"listKind,omitempty"`
	DurationSeconds int64  `json:"durationSeconds,omitempty"`
}

// Validate 保存前校验
func (p CEPPattern) Validate() error {
	if p.Key == "" || p.Name == "" {
		return errors.New("模式要有键和名称")
	}
	switch p.KeyBy {
	case EntityUser, EntityDevice, EntityIP, EntityAccount:
	default:
		return fmt.Errorf("按 %q 分组不支持", p.KeyBy)
	}
	if len(p.Steps) == 0 || len(p.Steps) > 8 {
		return errors.New("模式要有 1-8 步")
	}
	if p.WithinSeconds <= 0 || p.WithinSeconds > 30*86400 {
		return errors.New("时间窗要在 1 秒到 30 天之间")
	}
	for i, s := range p.Steps {
		if len(s.Events) == 0 {
			return fmt.Errorf("第 %d 步没有选事件", i+1)
		}
		if s.Times < 1 || s.Times > 1000 {
			return fmt.Errorf("第 %d 步的次数要在 1-1000 之间", i+1)
		}
		if s.Absent && (i == 0 || i != len(p.Steps)-1) {
			return fmt.Errorf("「没有发生」只能放在最后一步")
		}
	}
	for _, a := range p.Actions {
		switch a.Kind {
		case "case":
		case "list_add":
			switch a.Target {
			case "device", "ip", "account", "user":
			default:
				return fmt.Errorf("名单处置的对象 %q 不合法", a.Target)
			}
			if a.ListKind != KindBlock && a.ListKind != KindWatch {
				return errors.New("模式只能加黑名单或观察名单")
			}
		default:
			return fmt.Errorf("未知处置 %q", a.Kind)
		}
	}
	return nil
}
