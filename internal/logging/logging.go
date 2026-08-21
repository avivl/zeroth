package logging

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ctxKey struct{}

// Options configure a Zap logger. Empty Level defaults to info. Empty Encoding
// defaults to console (local dev). Use json in CI and production.
type Options struct {
	Level    string
	Encoding string
	// Writer is the log sink. Nil means stderr.
	Writer io.Writer
}

// New returns a Zap logger. The caller owns Sync.
func New(opts Options) (*zap.Logger, error) {
	level, err := ParseLevel(opts.Level)
	if err != nil {
		return nil, err
	}
	encoding, err := ParseEncoding(opts.Encoding)
	if err != nil {
		return nil, err
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	var enc zapcore.Encoder
	switch encoding {
	case "json":
		enc = zapcore.NewJSONEncoder(encCfg)
	default:
		encCfg.EncodeLevel = zapcore.CapitalLevelEncoder
		enc = zapcore.NewConsoleEncoder(encCfg)
	}

	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}
	core := zapcore.NewCore(enc, zapcore.AddSync(w), level)
	return zap.New(core), nil
}

// ParseLevel accepts debug, info, warn, or error. Empty is info.
func ParseLevel(s string) (zapcore.Level, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return zapcore.InfoLevel, nil
	}
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("logging level %q: %w", s, err)
	}
	switch lvl {
	case zapcore.DebugLevel, zapcore.InfoLevel, zapcore.WarnLevel, zapcore.ErrorLevel:
		return lvl, nil
	default:
		return 0, fmt.Errorf("logging level %q: must be debug, info, warn, or error", s)
	}
}

// ParseEncoding accepts console or json. Empty is console.
func ParseEncoding(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "console", nil
	}
	switch s {
	case "console", "json":
		return s, nil
	default:
		return "", fmt.Errorf("logging encoding %q: must be console or json", s)
	}
}

// WithLogger stores log in ctx for CLI wiring. Do not read this from
// internal/policy.
func WithLogger(ctx context.Context, log *zap.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxKey{}, log)
}

// FromContext returns the logger stored by WithLogger, or a no-op logger.
func FromContext(ctx context.Context) *zap.Logger {
	if ctx != nil {
		if log, ok := ctx.Value(ctxKey{}).(*zap.Logger); ok && log != nil {
			return log
		}
	}
	return zap.NewNop()
}
