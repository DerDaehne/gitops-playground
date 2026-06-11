package k8s

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoll_SucceedsOnFirstAttempt(t *testing.T) {
	ctx := context.Background()
	got, err := Poll(ctx, PollOptions{Interval: 10 * time.Millisecond, Timeout: time.Second, Description: "x"},
		func(context.Context) (string, bool, error) { return "value", true, nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "value" {
		t.Fatalf("want %q, got %q", "value", got)
	}
}

func TestPoll_SucceedsAfterRetries(t *testing.T) {
	ctx := context.Background()
	var calls int32
	got, err := Poll(ctx,
		PollOptions{Interval: 5 * time.Millisecond, Timeout: time.Second, Description: "x"},
		func(context.Context) (int32, bool, error) {
			n := atomic.AddInt32(&calls, 1)
			if n < 3 {
				return 0, false, nil
			}
			return n, true, nil
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 3 {
		t.Fatalf("want 3, got %d", got)
	}
}

func TestPoll_TransientErrorsAreRetried(t *testing.T) {
	ctx := context.Background()
	var calls int32
	got, err := Poll(ctx,
		PollOptions{Interval: 5 * time.Millisecond, Timeout: time.Second, Description: "x"},
		func(context.Context) (string, bool, error) {
			n := atomic.AddInt32(&calls, 1)
			if n < 2 {
				return "", false, errors.New("transient")
			}
			return "ok", true, nil
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("want ok, got %q", got)
	}
}

func TestPoll_TimeoutWrapsErrPollTimeout(t *testing.T) {
	ctx := context.Background()
	_, err := Poll(ctx,
		PollOptions{Interval: 5 * time.Millisecond, Timeout: 30 * time.Millisecond, Description: "never"},
		func(context.Context) (string, bool, error) { return "", false, nil })
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !errors.Is(err, ErrPollTimeout) {
		t.Fatalf("expected ErrPollTimeout, got %v", err)
	}
}

func TestPoll_ContextCancellationIsReported(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()
	_, err := Poll(ctx,
		PollOptions{Interval: 5 * time.Millisecond, Description: "cancelled"},
		func(context.Context) (string, bool, error) { return "", false, nil })
	if err == nil {
		t.Fatalf("expected error")
	}
	if errors.Is(err, ErrPollTimeout) {
		t.Fatalf("expected cancellation error, got timeout: %v", err)
	}
}

func TestDefaultPollOptions(t *testing.T) {
	opts := DefaultPollOptions("desc")
	if opts.Interval != time.Second {
		t.Errorf("Interval: want 1s, got %v", opts.Interval)
	}
	if opts.Timeout != 120*time.Second {
		t.Errorf("Timeout: want 120s, got %v", opts.Timeout)
	}
	if opts.Description != "desc" {
		t.Errorf("Description: want desc, got %q", opts.Description)
	}
}
