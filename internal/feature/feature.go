package feature

import "context"

// ID uniquely identifies a feature.
type ID string

// Feature is the core abstraction for anything GOP installs on the cluster.
// Features declare their dependencies via DependsOn() — the pipeline executor
// uses this to build a DAG and run independent features in parallel.
type Feature interface {
	// ID returns the unique identifier for this feature.
	ID() ID

	// DependsOn returns the IDs of features that must be installed before this one.
	// Return nil or an empty slice for features with no dependencies.
	DependsOn() []ID

	// Enabled reports whether this feature should be installed based on Config.
	Enabled() bool

	// Validate checks the configuration before any installation begins.
	// Called for all features before Install() is called for any.
	Validate(ctx context.Context) error

	// Install performs the feature installation (idempotent).
	Install(ctx context.Context) error

	// Uninstall removes the feature from the cluster.
	Uninstall(ctx context.Context) error
}
