package plugin

import "time"

// 内置（原生 Go）插件。与 expr / wasm 插件不同，内置插件随二进制发布，
// 由插件宿主管理生命周期（启动、停止、重启），状态持久化在 native_plugins 表。

// NativeManifest 内置插件的自描述。
type NativeManifest struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	Author       string   `json:"author"`
	Category     string   `json:"category"`
	Capabilities []string `json:"capabilities"`
	// DefaultEnabled 数据库里没有记录时是否启动。
	DefaultEnabled bool `json:"-"`
}

// 内置插件运行状态。
const (
	NativeStateRunning  = "running"
	NativeStateStopped  = "stopped"
	NativeStateStarting = "starting"
	NativeStateStopping = "stopping"
	NativeStateFailed   = "failed"
)

// NativeRecord native_plugins 表的一行。
type NativeRecord struct {
	ID        string
	Enabled   bool
	UpdatedBy *int64
	UpdatedAt time.Time
}

// NativeView 管理端看到的内置插件。
type NativeView struct {
	NativeManifest
	Builtin   bool           `json:"builtin"`
	Enabled   bool           `json:"enabled"`
	State     string         `json:"state"`
	Since     *time.Time     `json:"since,omitempty"`
	Error     string         `json:"error,omitempty"`
	Health    map[string]any `json:"health,omitempty"`
	UpdatedAt *time.Time     `json:"updatedAt,omitempty"`
	UpdatedBy *int64         `json:"updatedBy,omitempty"`
}
