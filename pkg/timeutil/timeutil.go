package timeutil

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
	Since(time.Time) time.Duration
	Until(time.Time) time.Duration
}

type systemClock struct{}

func (systemClock) Now() time.Time                  { return time.Now() }
func (systemClock) Since(t time.Time) time.Duration { return time.Since(t) }
func (systemClock) Until(t time.Time) time.Duration { return time.Until(t) }

var (
	globalMu       sync.RWMutex
	globalClock    Clock = systemClock{}
	defaultLocName       = "Asia/Shanghai"
	defaultLoc     *time.Location
)

func init() {
	loc, err := time.LoadLocation(defaultLocName)
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	defaultLoc = loc
}

func Init(defaultTimezone string) error {
	loc, err := resolveLocation(defaultTimezone)
	if err != nil {
		return err
	}
	globalMu.Lock()
	defer globalMu.Unlock()
	defaultLocName = loc.String()
	defaultLoc = loc
	return nil
}

func DefaultLocation() *time.Location {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return defaultLoc
}

func DefaultTimezone() string {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return defaultLocName
}

func SetClockForTest(clock Clock) {
	globalMu.Lock()
	defer globalMu.Unlock()
	if clock == nil {
		globalClock = systemClock{}
		return
	}
	globalClock = clock
}

func ResetClockForTest() { SetClockForTest(nil) }

func Now() time.Time {
	globalMu.RLock()
	clock := globalClock
	globalMu.RUnlock()
	return clock.Now()
}

func NowUTC() time.Time { return Now().UTC() }

func Since(t time.Time) time.Duration {
	globalMu.RLock()
	clock := globalClock
	globalMu.RUnlock()
	return clock.Since(t)
}

func Until(t time.Time) time.Duration {
	globalMu.RLock()
	clock := globalClock
	globalMu.RUnlock()
	return clock.Until(t)
}

func NormalizeUTC(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.UTC()
}

func EqualInstant(a, b time.Time) bool { return a.Equal(b) }

func FormatRFC3339UTC(t time.Time) string {
	return NormalizeUTC(t).Format(time.RFC3339)
}

func ParseRFC3339Strict(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("datetime is required")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	if !strings.HasSuffix(value, "Z") && !hasExplicitOffset(value) {
		return time.Time{}, fmt.Errorf("datetime must include timezone offset")
	}
	return parsed.UTC(), nil
}

func LoadLocation(name string) (*time.Location, error) {
	return resolveLocation(name)
}

func MustDefaultRuleLocation(name string) *time.Location {
	loc, err := resolveLocation(name)
	if err == nil {
		return loc
	}
	return DefaultLocation()
}

type LocalTimeOfDay struct {
	Hour   int
	Minute int
	Second int
}

func ParseLocalTimeOfDay(raw string) (LocalTimeOfDay, error) {
	value := strings.TrimSpace(raw)
	for _, layout := range []string{"15:04", "15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return LocalTimeOfDay{Hour: parsed.Hour(), Minute: parsed.Minute(), Second: parsed.Second()}, nil
		}
	}
	return LocalTimeOfDay{}, fmt.Errorf("invalid local time of day")
}

type RuleSchedule struct {
	TimeOfDay LocalTimeOfDay
	Timezone  string
}

func (r RuleSchedule) Location() (*time.Location, error) {
	return resolveLocation(r.Timezone)
}

func NextOccurrence(rule RuleSchedule, fromUTC time.Time) (time.Time, error) {
	loc, err := rule.Location()
	if err != nil {
		return time.Time{}, err
	}
	reference := NormalizeUTC(fromUTC).In(loc)
	candidate := time.Date(reference.Year(), reference.Month(), reference.Day(), rule.TimeOfDay.Hour, rule.TimeOfDay.Minute, rule.TimeOfDay.Second, 0, loc)
	if !candidate.After(reference) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate.UTC(), nil
}

type FakeClock struct {
	mu  sync.RWMutex
	now time.Time
}

func NewFakeClock(now time.Time) *FakeClock {
	return &FakeClock{now: NormalizeUTC(now)}
}

func (f *FakeClock) Now() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.now
}

func (f *FakeClock) Since(t time.Time) time.Duration { return f.Now().Sub(t) }
func (f *FakeClock) Until(t time.Time) time.Duration { return t.Sub(f.Now()) }

func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func resolveLocation(name string) (*time.Location, error) {
	value := strings.TrimSpace(name)
	if value == "" {
		value = defaultLocName
	}
	loc, err := time.LoadLocation(value)
	if err != nil {
		return nil, fmt.Errorf("invalid IANA timezone %q: %w", value, err)
	}
	return loc, nil
}

func hasExplicitOffset(value string) bool {
	if len(value) < 6 {
		return false
	}
	tail := value[len(value)-6:]
	return (tail[0] == '+' || tail[0] == '-') && tail[3] == ':'
}
