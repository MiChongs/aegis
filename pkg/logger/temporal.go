package logger

import (
	"fmt"
	"time"

	temporallog "go.temporal.io/sdk/log"
	"go.uber.org/zap"
)

type temporalZapLogger struct {
	sugar *zap.SugaredLogger
}

func NewTemporalLogger(base *zap.Logger) temporallog.Logger {
	if base == nil {
		base = zap.NewNop()
	}
	return &temporalZapLogger{
		sugar: base.Sugar(),
	}
}

func (l *temporalZapLogger) Debug(msg string, keyvals ...interface{}) {
	l.sugar.Debugw(msg, normalizeTemporalKeyvals(keyvals)...)
}

func (l *temporalZapLogger) Info(msg string, keyvals ...interface{}) {
	l.sugar.Infow(msg, normalizeTemporalKeyvals(keyvals)...)
}

func (l *temporalZapLogger) Warn(msg string, keyvals ...interface{}) {
	l.sugar.Warnw(msg, normalizeTemporalKeyvals(keyvals)...)
}

func (l *temporalZapLogger) Error(msg string, keyvals ...interface{}) {
	l.sugar.Errorw(msg, normalizeTemporalKeyvals(keyvals)...)
}

func (l *temporalZapLogger) With(keyvals ...interface{}) temporallog.Logger {
	return &temporalZapLogger{
		sugar: l.sugar.With(normalizeTemporalKeyvals(keyvals)...),
	}
}

func (l *temporalZapLogger) WithCallerSkip(depth int) temporallog.Logger {
	return &temporalZapLogger{
		sugar: l.sugar.Desugar().WithOptions(zap.AddCallerSkip(depth)).Sugar(),
	}
}

func normalizeTemporalKeyvals(keyvals []interface{}) []interface{} {
	if len(keyvals) == 0 {
		return nil
	}

	normalized := make([]interface{}, 0, len(keyvals)+(len(keyvals)%2))
	for i := 0; i < len(keyvals); i += 2 {
		key := fmt.Sprintf("arg_%d", i)
		if i < len(keyvals) && keyvals[i] != nil {
			key = fmt.Sprint(keyvals[i])
		}

		var value interface{} = "(missing)"
		if i+1 < len(keyvals) {
			value = keyvals[i+1]
			if duration, ok := value.(time.Duration); ok {
				value = duration.String()
			}
		}

		normalized = append(normalized, key, value)
	}

	return normalized
}
