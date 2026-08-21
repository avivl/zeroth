package resilience

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/timeout"
)

func TestRunRetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	opts := Options{MaxRetries: 2, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond, Timeout: time.Second}
	exec := NewExecutor[struct{}](opts)
	err := Run(t.Context(), exec, func(context.Context) error {
		if n.Add(1) < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := n.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestRunTimeoutCancelsAttempt(t *testing.T) {
	t.Parallel()
	opts := Options{MaxRetries: 0, Timeout: 30 * time.Millisecond}
	exec := NewExecutor[struct{}](opts)
	err := Run(t.Context(), exec, func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
			return errors.New("should have been canceled")
		}
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !errors.Is(err, timeout.ErrExceeded) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want timeout or cancel", err)
	}
}

func TestCircuitBreakerOpens(t *testing.T) {
	t.Parallel()
	opts := Options{
		MaxRetries:       0,
		Timeout:          time.Second,
		FailureThreshold: 2,
		BreakerDelay:     time.Hour,
	}
	br := NewBreaker[struct{}](opts)
	exec := NewExecutor(opts, br)
	boom := errors.New("boom")
	for i := 0; i < 2; i++ {
		err := Run(t.Context(), exec, func(context.Context) error { return boom })
		if !errors.Is(err, boom) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	err := Run(t.Context(), exec, func(context.Context) error {
		t.Fatal("fn must not run while open")
		return nil
	})
	if !errors.Is(err, circuitbreaker.ErrOpen) {
		t.Fatalf("open err = %v, want %v", err, circuitbreaker.ErrOpen)
	}
}

func TestGetReturnsValue(t *testing.T) {
	t.Parallel()
	exec := NewExecutor[string](Options{MaxRetries: 0, Timeout: time.Second})
	got, err := Get(t.Context(), exec, func(context.Context) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "ok" {
		t.Fatalf("Get = %q, want ok", got)
	}
}

func TestDialUnixSuccess(t *testing.T) {
	t.Parallel()
	socket := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}()

	err = DialUnix(t.Context(), socket, Options{MaxRetries: 1, Timeout: time.Second})
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("accept did not finish")
	}
}

func TestDialUnixMissingRetries(t *testing.T) {
	t.Parallel()
	socket := filepath.Join(t.TempDir(), "missing.sock")
	err := DialUnix(t.Context(), socket, Options{
		MaxRetries: 1,
		MinBackoff: time.Millisecond,
		MaxBackoff: time.Millisecond,
		Timeout:    50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDialUnixEmpty(t *testing.T) {
	t.Parallel()
	if err := DialUnix(t.Context(), "  ", Defaults()); err == nil {
		t.Fatal("expected error")
	}
}
