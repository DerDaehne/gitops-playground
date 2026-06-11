package destroy

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

type stub struct {
	name  string
	order int
	err   error
	calls *[]string
}

func (s stub) Name() string  { return s.name }
func (s stub) Order() int    { return s.order }
func (s stub) Destroy(_ context.Context, _ *config.Config) error {
	*s.calls = append(*s.calls, s.name)
	return s.err
}

func TestDestroyerRunsInOrder(t *testing.T) {
	var calls []string
	d := New()
	d.Register(
		stub{name: "second", order: 200, calls: &calls},
		stub{name: "first", order: 100, calls: &calls},
		stub{name: "third", order: 300, calls: &calls},
	)
	if err := d.Destroy(context.Background(), config.New()); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	got := []string{"first", "second", "third"}
	for i := range got {
		if calls[i] != got[i] {
			t.Errorf("at %d: got %q, want %q", i, calls[i], got[i])
		}
	}
}

func TestDestroyerStopsOnError(t *testing.T) {
	var calls []string
	boom := errors.New("boom")
	d := New()
	d.Register(
		stub{name: "ok", order: 1, calls: &calls},
		stub{name: "bad", order: 2, err: boom, calls: &calls},
		stub{name: "skipped", order: 3, calls: &calls},
	)
	err := d.Destroy(context.Background(), config.New())
	if !errors.Is(err, boom) {
		t.Errorf("expected wrapped boom, got %v", err)
	}
	if len(calls) != 2 {
		t.Errorf("expected 2 calls, got %v", calls)
	}
}
