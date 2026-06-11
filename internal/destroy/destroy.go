// Package destroy is the Go port of com.cloudogu.gitops.destroy.
//
// The Groovy code expresses every cleanup step as a DestructionHandler
// with @Order annotations. We keep that contract but make the ordering
// explicit on the registry, which matches the install-side runner.Runner.
package destroy

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

// Handler is implemented by every destroy step. The Order method drives
// the execution sequence (lower runs first), matching the install side.
type Handler interface {
	Name() string
	Order() int
	Destroy(ctx context.Context, cfg *config.Config) error
}

// Destroyer runs all registered handlers in order. Stops at the first
// error (matches the Groovy Destroyer behaviour).
type Destroyer struct {
	handlers []Handler
}

// New returns an empty Destroyer.
func New() *Destroyer { return &Destroyer{} }

// Register adds one or more handlers.
func (d *Destroyer) Register(hs ...Handler) { d.handlers = append(d.handlers, hs...) }

// Destroy runs every handler in ascending Order(). Disabled handlers
// (filter set by the caller) must not be added in the first place – the
// registry intentionally has no "enabled?" predicate to keep the logic
// linear.
func (d *Destroyer) Destroy(ctx context.Context, cfg *config.Config) error {
	sorted := make([]Handler, len(d.handlers))
	copy(sorted, d.handlers)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Order() < sorted[j].Order() })

	for _, h := range sorted {
		slog.Info("destroy: running handler", "name", h.Name())
		if err := h.Destroy(ctx, cfg); err != nil {
			return fmt.Errorf("destroy %s: %w", h.Name(), err)
		}
	}
	return nil
}
