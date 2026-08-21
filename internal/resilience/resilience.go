package resilience

import (
	"context"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
	"github.com/failsafe-go/failsafe-go/timeout"
)

// Options configure the reference Failsafe-go policies. Zero Timeout omits the
// timeout policy. Breaker fields apply only to NewBreaker.
type Options struct {
	MaxRetries       int
	MinBackoff       time.Duration
	MaxBackoff       time.Duration
	Timeout          time.Duration
	FailureThreshold uint
	BreakerDelay     time.Duration
}

// Defaults is the stage-1 policy for calls that leave the process. Drivers
// should start here and tighten per call site, not invent a parallel scheme.
func Defaults() Options {
	return Options{
		MaxRetries:       3,
		MinBackoff:       100 * time.Millisecond,
		MaxBackoff:       2 * time.Second,
		Timeout:          5 * time.Second,
		FailureThreshold: 5,
		BreakerDelay:     30 * time.Second,
	}
}

// NewRetry returns a retry policy with exponential backoff.
func NewRetry[R any](opts Options) retrypolicy.RetryPolicy[R] {
	b := retrypolicy.NewBuilder[R]().WithMaxRetries(opts.MaxRetries)
	if opts.MinBackoff > 0 && opts.MaxBackoff >= opts.MinBackoff {
		b = b.WithBackoff(opts.MinBackoff, opts.MaxBackoff)
	}
	return b.Build()
}

// NewBreaker returns a count-based circuit breaker. Callers own the instance
// and pass it into NewExecutor so state is shared across calls to the same
// remote. Do not store one in a package-level global.
func NewBreaker[R any](opts Options) circuitbreaker.CircuitBreaker[R] {
	threshold := opts.FailureThreshold
	if threshold == 0 {
		threshold = 5
	}
	delay := opts.BreakerDelay
	if delay == 0 {
		delay = 30 * time.Second
	}
	return circuitbreaker.NewBuilder[R]().
		WithFailureThreshold(threshold).
		WithDelay(delay).
		Build()
}

// NewExecutor composes Retry(extra...)(Timeout(fn)). extra is where a circuit
// breaker belongs: Retry(Breaker(Timeout(fn))).
func NewExecutor[R any](opts Options, extra ...failsafe.Policy[R]) failsafe.Executor[R] {
	policies := make([]failsafe.Policy[R], 0, 2+len(extra))
	policies = append(policies, NewRetry[R](opts))
	policies = append(policies, extra...)
	if opts.Timeout > 0 {
		policies = append(policies, timeout.New[R](opts.Timeout))
	}
	return failsafe.With(policies...)
}

// Run executes fn under exec, passing the attempt context so timeouts cancel
// the in-flight call.
func Run(ctx context.Context, exec failsafe.Executor[struct{}], fn func(context.Context) error) error {
	return exec.WithContext(ctx).RunWithExecution(func(e failsafe.Execution[struct{}]) error {
		return fn(e.Context())
	})
}

// Get is Run for a result-returning call. Tracker and harness drivers should
// use this instead of a manual retry loop.
func Get[R any](ctx context.Context, exec failsafe.Executor[R], fn func(context.Context) (R, error)) (R, error) {
	return exec.WithContext(ctx).GetWithExecution(func(e failsafe.Execution[R]) (R, error) {
		return fn(e.Context())
	})
}
