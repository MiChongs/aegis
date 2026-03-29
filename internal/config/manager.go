package config

import (
	"sync"
	"time"

	"aegis/pkg/timeutil"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

type ChangeEvent struct {
	Path      string
	Previous  Config
	Current   Config
	Err       error
	ChangedAt time.Time
}

type ChangeHandler func(ChangeEvent)

type Manager struct {
	mu         sync.RWMutex
	reloadMu   sync.Mutex
	startOnce  sync.Once
	v          *viper.Viper
	configFile string
	current    Config
	handlers   []ChangeHandler
}

func NewManager() (*Manager, error) {
	v, configFile, err := newConfiguredViper()
	if err != nil {
		return nil, err
	}
	current, err := loadWithViper(v)
	if err != nil {
		return nil, err
	}
	return &Manager{
		v:          v,
		configFile: configFile,
		current:    current,
	}, nil
}

func (m *Manager) Current() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

func (m *Manager) ConfigFile() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.configFile
}

func (m *Manager) OnChange(handler ChangeHandler) {
	if handler == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append(m.handlers, handler)
}

func (m *Manager) Start() bool {
	if m == nil || m.v == nil || m.ConfigFile() == "" {
		return false
	}
	started := false
	m.startOnce.Do(func() {
		started = true
		m.v.OnConfigChange(func(event fsnotify.Event) {
			m.reload(event)
		})
		m.v.WatchConfig()
	})
	return started
}

func (m *Manager) reload(event fsnotify.Event) {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	previous := m.Current()
	next, err := loadWithViper(m.v)
	change := ChangeEvent{
		Path:      event.Name,
		Previous:  previous,
		Current:   previous,
		Err:       err,
		ChangedAt: timeutil.NowUTC(),
	}
	if err == nil {
		m.mu.Lock()
		m.current = next
		handlers := append([]ChangeHandler(nil), m.handlers...)
		m.mu.Unlock()
		change.Current = next
		for _, handler := range handlers {
			handler(change)
		}
		return
	}

	m.mu.RLock()
	handlers := append([]ChangeHandler(nil), m.handlers...)
	m.mu.RUnlock()
	for _, handler := range handlers {
		handler(change)
	}
}
