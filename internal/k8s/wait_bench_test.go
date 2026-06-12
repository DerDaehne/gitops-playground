package k8s

// Benchmarks for the consolidated Poll helper in wait.go.
//
// The benchmark function names follow the WP-A4 brief
// (BenchmarkPollImmediateUntil_*). They exercise Poll, which is the
// single retry primitive every WaitFor* method in this package builds
// on, so a regression in Poll shows up here before it spreads to
// pods.go / deployments.go callers.
//
// Run them with:
//
//	go test -bench=. -benchtime=2x -run='^$' ./internal/k8s/...
//
// The -benchtime=2x cap keeps CI snappy: each benchmark uses
// sub-millisecond poll intervals, so two iterations are enough to
// detect an order-of-magnitude regression without sleeping.

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// BenchmarkPollImmediateUntil_HitFirst measures the hot path: the
// condition returns true on the very first call, so Poll never starts
// its ticker. This is the cost we pay on every successful "is it
// ready yet?" check once the resource is up.
func BenchmarkPollImmediateUntil_HitFirst(b *testing.B) {
	ctx := context.Background()
	opts := PollOptions{
		Interval:    time.Millisecond,
		Timeout:     50 * time.Millisecond,
		Description: "hit-first",
	}
	fetch := func(context.Context) (struct{}, bool, error) {
		return struct{}{}, true, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Poll(ctx, opts, fetch); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkPollImmediateUntil_HitTenth measures the steady-state
// retry path: the condition flips to true on the tenth call, so Poll
// goes through nine ticker waits. With a 100us interval each
// iteration takes roughly 1 ms — well under the 200 ms budget.
func BenchmarkPollImmediateUntil_HitTenth(b *testing.B) {
	ctx := context.Background()
	opts := PollOptions{
		Interval:    100 * time.Microsecond,
		Timeout:     50 * time.Millisecond,
		Description: "hit-tenth",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calls := 0
		fetch := func(context.Context) (struct{}, bool, error) {
			calls++
			return struct{}{}, calls >= 10, nil
		}
		if _, err := Poll(ctx, opts, fetch); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkPollImmediateUntil_Cancellation measures the
// cancellation path and guards against goroutine leaks. The parent
// context is cancelled while Poll is still waiting for its ticker;
// Poll must return promptly without leaving any background goroutine
// behind. We sample runtime.NumGoroutine before and after the loop
// and fail the benchmark if the count grew.
func BenchmarkPollImmediateUntil_Cancellation(b *testing.B) {
	opts := PollOptions{
		Interval:    500 * time.Microsecond,
		Timeout:     0, // rely on context cancellation
		Description: "cancellation",
	}
	fetch := func(context.Context) (struct{}, bool, error) {
		return struct{}{}, false, nil
	}

	// Let any startup goroutines settle before we sample.
	runtime.GC()
	before := runtime.NumGoroutine()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		// Cancel right after Poll runs its synchronous first attempt,
		// so we exercise the ticker branch + ctx.Done() race.
		time.AfterFunc(200*time.Microsecond, cancel)
		if _, err := Poll(ctx, opts, fetch); err == nil {
			b.Fatalf("expected cancellation error, got nil")
		}
		cancel()
	}
	b.StopTimer()

	// Allow time.AfterFunc helpers and the cancelled pollCtx to drain.
	runtime.GC()
	after := runtime.NumGoroutine()
	if after > before+1 { // +1 for the testing framework's own slack
		b.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}
