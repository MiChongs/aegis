package circuitbreaker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"aegis/pkg/timeutil"
	gobreaker "github.com/sony/gobreaker/v2"
)

const (
	defaultMaxRequests        = uint32(2)
	defaultMinimumRequests    = uint32(10)
	defaultConsecutiveFailure = uint32(3)
	defaultFailureRatio       = 0.6
)

var (
	defaultInterval     = timeutil.Minutes(1)
	defaultBucketPeriod = timeutil.Seconds(10)
	defaultTimeout      = timeutil.Seconds(30)
)

type Options struct {
	MaxRequests         uint32
	MinimumRequests     uint32
	ConsecutiveFailures uint32
	FailureRatio        float64
	Interval            time.Duration
	BucketPeriod        time.Duration
	Timeout             time.Duration
}

type Manager struct {
	mu       sync.RWMutex
	breakers map[string]*gobreaker.CircuitBreaker[any]
	options  Options
}

var defaultManager = NewManager(Options{})

func NewManager(options Options) *Manager {
	return &Manager{
		breakers: make(map[string]*gobreaker.CircuitBreaker[any]),
		options:  normalizeOptions(options),
	}
}

func Execute[T any](name string, fn func() (T, error)) (T, error) {
	breaker := defaultManager.breaker(name)
	value, err := breaker.Execute(func() (any, error) {
		return fn()
	})
	if err != nil {
		var zero T
		return zero, err
	}
	typed, _ := value.(T)
	return typed, nil
}

func IsOpenError(err error) bool {
	return errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests)
}

func normalizeNamePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "default"
	}

	var builder strings.Builder
	builder.Grow(len(value))
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		case !lastDash:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	normalized := strings.Trim(builder.String(), "-")
	if normalized == "" {
		return "default"
	}
	return normalized
}

func Name(parts ...string) string {
	if len(parts) == 0 {
		return "default"
	}
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized = append(normalized, normalizeNamePart(part))
	}
	return strings.Join(normalized, ":")
}

func (m *Manager) breaker(name string) *gobreaker.CircuitBreaker[any] {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "default"
	}

	m.mu.RLock()
	breaker := m.breakers[name]
	m.mu.RUnlock()
	if breaker != nil {
		return breaker
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if breaker = m.breakers[name]; breaker != nil {
		return breaker
	}

	opts := m.options
	breaker = gobreaker.NewCircuitBreaker[any](gobreaker.Settings{
		Name:         name,
		MaxRequests:  opts.MaxRequests,
		Interval:     opts.Interval,
		BucketPeriod: opts.BucketPeriod,
		Timeout:      opts.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			if counts.ConsecutiveFailures >= opts.ConsecutiveFailures {
				return true
			}
			if counts.Requests < opts.MinimumRequests {
				return false
			}
			return float64(counts.TotalFailures)/float64(counts.Requests) >= opts.FailureRatio
		},
		IsExcluded: func(err error) bool {
			return errors.Is(err, context.Canceled)
		},
	})
	m.breakers[name] = breaker
	return breaker
}

func normalizeOptions(options Options) Options {
	if options.MaxRequests == 0 {
		options.MaxRequests = defaultMaxRequests
	}
	if options.MinimumRequests == 0 {
		options.MinimumRequests = defaultMinimumRequests
	}
	if options.ConsecutiveFailures == 0 {
		options.ConsecutiveFailures = defaultConsecutiveFailure
	}
	if options.FailureRatio <= 0 || options.FailureRatio > 1 {
		options.FailureRatio = defaultFailureRatio
	}
	if options.Interval <= 0 {
		options.Interval = defaultInterval
	}
	if options.BucketPeriod <= 0 {
		options.BucketPeriod = defaultBucketPeriod
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultTimeout
	}
	return options
}
