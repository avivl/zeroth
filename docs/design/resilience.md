# Failsafe-go reference pattern

Stage 1 wraps unreliable external calls with [Failsafe-go](https://github.com/failsafe-go/failsafe-go). Sandbox, harness, and tracker drivers that land in later milestones must use this composition instead of a hand-rolled retry loop.

The code lives in `internal/resilience`. The worked call site is `resilience.DialUnix`, which `zerothd` uses to probe the Docker unix socket at startup. Copy the policy composition, not the dial, when a driver grows a real remote (Docker API, subprocess supervision, Linear HTTP).

## Composition

Outer to inner: **retry, optional circuit breaker, timeout**.

```
failsafe.With(retry, breaker, timeout).GetWithExecution(fn)
```

That is `Retry(Breaker(Timeout(fn)))`:

- Timeout is per attempt and cancels `Execution.Context()`.
- Circuit breaker counts each attempt. Own the breaker on the driver so state is shared across calls to the same remote. Do not store one in a package-level global.
- Retry is outermost so a rejected open breaker still ends the call instead of spinning.

`resilience.NewExecutor` builds that stack. Pass a breaker via the `extra` policies argument. `Run` and `Get` thread the attempt context into the function so I/O can abort.

## Defaults

`resilience.Defaults()` is the starting policy: 3 retries, 100ms to 2s backoff, 5s attempt timeout, breaker opens after 5 failures and waits 30s. Tighten per call site. Do not add a second library or a `for { retry }` loop next to this.

## Kernel

`internal/policy` does not import this package. Policy has no I/O. Callers pass facts in.
