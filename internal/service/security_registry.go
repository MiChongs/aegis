package service

import (
	"sync"

	securitydomain "aegis/internal/domain/security"
)

type SecurityModule interface {
	Status() securitydomain.ModuleStatus
}

type StaticSecurityModule struct {
	status securitydomain.ModuleStatus
}

func NewStaticSecurityModule(status securitydomain.ModuleStatus) StaticSecurityModule {
	return StaticSecurityModule{status: status}
}

func (m StaticSecurityModule) Status() securitydomain.ModuleStatus {
	return m.status
}

type SecurityRegistry struct {
	mu      sync.RWMutex
	order   []string
	modules map[string]SecurityModule
}

func NewSecurityRegistry() *SecurityRegistry {
	return &SecurityRegistry{modules: make(map[string]SecurityModule)}
}

func (r *SecurityRegistry) Register(module SecurityModule) {
	if r == nil || module == nil {
		return
	}
	status := module.Status()
	if status.Key == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.modules[status.Key]; !ok {
		r.order = append(r.order, status.Key)
	}
	r.modules[status.Key] = module
}

func (r *SecurityRegistry) Replace(modules []SecurityModule) {
	if r == nil {
		return
	}

	nextModules := make(map[string]SecurityModule, len(modules))
	nextOrder := make([]string, 0, len(modules))
	for _, module := range modules {
		if module == nil {
			continue
		}
		status := module.Status()
		if status.Key == "" {
			continue
		}
		if _, exists := nextModules[status.Key]; exists {
			continue
		}
		nextModules[status.Key] = module
		nextOrder = append(nextOrder, status.Key)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.modules = nextModules
	r.order = nextOrder
}

func (r *SecurityRegistry) Unregister(key string) {
	if r == nil || key == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.modules[key]; !ok {
		return
	}
	delete(r.modules, key)
	nextOrder := make([]string, 0, len(r.order))
	for _, item := range r.order {
		if item != key {
			nextOrder = append(nextOrder, item)
		}
	}
	r.order = nextOrder
}

func (r *SecurityRegistry) Statuses() []securitydomain.ModuleStatus {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]securitydomain.ModuleStatus, 0, len(r.order))
	for _, key := range r.order {
		module, ok := r.modules[key]
		if !ok || module == nil {
			continue
		}
		items = append(items, module.Status())
	}
	return items
}

func (r *SecurityRegistry) Status(key string) (securitydomain.ModuleStatus, bool) {
	if r == nil {
		return securitydomain.ModuleStatus{}, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	module, ok := r.modules[key]
	if !ok || module == nil {
		return securitydomain.ModuleStatus{}, false
	}
	return module.Status(), true
}
