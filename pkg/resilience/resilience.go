package resilience

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"aegis/pkg/circuitbreaker"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"golang.org/x/time/rate"
)

const (
	defaultMaxRetries = 2
	defaultRatePerSec = 5
	defaultBurst      = 10
)

var (
	defaultTimeout     = timeutil.Seconds(10)
	defaultBaseBackoff = timeutil.Milliseconds(200)
	defaultMaxBackoff  = timeutil.Seconds(2)
)

type Options struct {
	Timeout     time.Duration
	MaxRetries  int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	RatePerSec  rate.Limit
	Burst       int
	ShouldRetry func(error) bool
}

type limiterRegistry struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
}

var registry = &limiterRegistry{limiters: make(map[string]*rate.Limiter)}

func Execute[T any](ctx context.Context, name string, options Options, fn func(context.Context) (T, error)) (T, error) {
	options = normalizeOptions(options)
	limiter := registry.limiter(name, options)

	var lastErr error
	for attempt := 0; attempt <= options.MaxRetries; attempt++ {
		if err := limiter.Wait(ctx); err != nil {
			var zero T
			return zero, err
		}

		attemptCtx := ctx
		var cancel context.CancelFunc
		if options.Timeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, options.Timeout)
		}

		value, err := circuitbreaker.Execute(name, func() (T, error) {
			return fn(attemptCtx)
		})
		if cancel != nil {
			cancel()
		}
		if err == nil {
			return value, nil
		}
		lastErr = err

		if circuitbreaker.IsOpenError(err) || !options.ShouldRetry(err) || attempt == options.MaxRetries {
			var zero T
			return zero, err
		}

		if sleepErr := sleep(ctx, backoffForAttempt(options, attempt)); sleepErr != nil {
			var zero T
			return zero, errors.Join(lastErr, sleepErr)
		}
	}

	var zero T
	return zero, lastErr
}

func RetryableError(err error) bool {
	if err == nil {
		return false
	}
	if circuitbreaker.IsOpenError(err) || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
	}

	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.HTTPStatus == http.StatusTooManyRequests || appErr.HTTPStatus >= http.StatusBadGateway
	}

	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "timeout"),
		strings.Contains(lower, "tempor"),
		strings.Contains(lower, "connection reset"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "broken pipe"),
		strings.Contains(lower, "eof"),
		strings.Contains(lower, "429"),
		strings.Contains(lower, "502"),
		strings.Contains(lower, "503"),
		strings.Contains(lower, "504"):
		return true
	default:
		return false
	}
}

func normalizeOptions(options Options) Options {
	if options.Timeout <= 0 {
		options.Timeout = defaultTimeout
	}
	if options.MaxRetries < 0 {
		options.MaxRetries = 0
	}
	if options.BaseBackoff <= 0 {
		options.BaseBackoff = defaultBaseBackoff
	}
	if options.MaxBackoff <= 0 {
		options.MaxBackoff = defaultMaxBackoff
	}
	if options.RatePerSec <= 0 {
		options.RatePerSec = defaultRatePerSec
	}
	if options.Burst <= 0 {
		options.Burst = defaultBurst
	}
	if options.ShouldRetry == nil {
		options.ShouldRetry = RetryableError
	}
	return options
}

func (r *limiterRegistry) limiter(name string, options Options) *rate.Limiter {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "default"
	}

	r.mu.RLock()
	limiter := r.limiters[name]
	r.mu.RUnlock()
	if limiter != nil {
		return limiter
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if limiter = r.limiters[name]; limiter != nil {
		return limiter
	}
	limiter = rate.NewLimiter(options.RatePerSec, options.Burst)
	r.limiters[name] = limiter
	return limiter
}

func backoffForAttempt(options Options, attempt int) time.Duration {
	backoff := options.BaseBackoff
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if backoff >= options.MaxBackoff {
			backoff = options.MaxBackoff
			break
		}
	}
	if backoff <= 0 {
		return 0
	}
	jitterMax := backoff / 5
	if jitterMax <= 0 {
		return backoff
	}
	return backoff + time.Duration(rand.Int63n(int64(jitterMax)))
}

func sleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := timeutil.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
