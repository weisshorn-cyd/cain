package utils

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/slok/kubewebhook/v2/pkg/log"
)

type logger struct {
	*slog.Logger
}

func NewLogger(sloglogger *slog.Logger) log.Logger { //nolint:ireturn // kwh expects the interface
	return logger{sloglogger}
}

func (l logger) Infof(format string, args ...interface{}) {
	l.Info(fmt.Sprintf(format, args...))
}

func (l logger) Warningf(format string, args ...interface{}) {
	l.Info(fmt.Sprintf(format, args...))
}

func (l logger) Errorf(format string, args ...interface{}) {
	l.Error(fmt.Sprintf(format, args...))
}

func (l logger) Debugf(format string, args ...interface{}) {
	l.Debug(fmt.Sprintf(format, args...))
}

func (l logger) WithValues(values map[string]interface{}) log.Logger { //nolint:ireturn // kwh expects the interface
	args := make([]any, 0, len(values))

	for k, v := range values {
		args = append(args, k, v)
	}

	return NewLogger(l.With(args...))
}

func (l logger) WithCtxValues(ctx context.Context) log.Logger { //nolint:ireturn // kwh expects the interface
	return l.WithValues(log.ValuesFromCtx(ctx))
}

func (l logger) SetValuesOnCtx(parent context.Context, values log.Kv) context.Context {
	return log.CtxWithValues(parent, values)
}
