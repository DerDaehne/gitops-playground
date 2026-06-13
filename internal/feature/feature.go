// Package feature defines the Feature contract that each GOP tool
// implements. It is the Go counterpart of com.cloudogu.gitops.tools.common.Tool
// plus the optional hook interfaces from GitopsPlaygroundCli.runHook().
//
// Differences vs. the Groovy original:
//
//   - No reflection: optional hooks (PreConfigInit/PostConfigInit) are
//     plain interfaces. A type implements them or it doesn't; mismatched
//     signatures fail at compile time rather than silently being ignored.
//   - Namespace() returns a string instead of relying on a
//     this.metaClass.hasProperty('namespace') reflection probe.
//   - Order is exposed via Order() int; the runner sorts ascending.
//     Groovy used Micronaut's @Order annotation – here it's data.
package feature

import (
	"context"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

// Feature is the contract every tool ports to. Methods take a context so
// long-running operations (waiting for a deployment to be ready, polling
// a remote API) can be cancelled.
type Feature interface {
	// Name is a short, stable identifier; used in logs.
	Name() string

	// Order controls the install order; lower runs first. Defaults to
	// 100 if implementers return zero – callers should not rely on the
	// exact magnitude, only on relative ordering.
	Order() int

	// IsEnabled reports whether the feature should run at all. Disabled
	// features still get Disable() called so they can clean up.
	IsEnabled(cfg *config.Config) bool

	// Namespace returns the Kubernetes namespace the feature owns, or ""
	// if it does not own a dedicated namespace.
	Namespace(cfg *config.Config) string

	// Install installs/updates the feature. Only called when IsEnabled.
	Install(ctx context.Context, cfg *config.Config) error

	// Disable is called when IsEnabled returns false. Most features
	// can implement it as a no-op. Provided so destructive cleanup
	// stays optional and explicit.
	Disable(ctx context.Context, cfg *config.Config) error
}

// Validator is an optional interface. Features that need to fail-fast on
// invalid config implement it; the runner calls Validate before any
// install starts.
type Validator interface {
	Validate(ctx context.Context, cfg *config.Config) error
}

// PreConfigInit is an optional hook fired before
// ApplicationConfigurator.Initialise. Mirrors Tool.preConfigInit.
type PreConfigInit interface {
	PreConfigInit(cfg *config.Config) error
}

// PostConfigInit is an optional hook fired after
// ApplicationConfigurator.Initialise.
type PostConfigInit interface {
	PostConfigInit(cfg *config.Config) error
}

// ExternalConfigurator is implemented by features that have a
// post-deploy configuration step which must run even when the
// feature itself is externally provided (and therefore IsEnabled
// returns false).
//
// The runner fires ConfigureExternal in the same disabled-branch where
// it would otherwise only call Disable, after Validate. Implementers
// should no-op when the external pre-conditions (e.g. an API client,
// a base URL) are not met, so they stay safe to call unconditionally.
type ExternalConfigurator interface {
	ConfigureExternal(ctx context.Context, cfg *config.Config) error
}

// MaybeConfigureExternal invokes ConfigureExternal on f if it
// implements ExternalConfigurator and returns true; otherwise it
// returns false with a nil error. Keeps the type assertion in one
// place so callers (the runner, tests) do not duplicate it.
func MaybeConfigureExternal(ctx context.Context, f Feature, cfg *config.Config) (bool, error) {
	ec, ok := f.(ExternalConfigurator)
	if !ok {
		return false, nil
	}
	return true, ec.ConfigureExternal(ctx, cfg)
}
