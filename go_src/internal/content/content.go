// Package content is the Go port of
// com.cloudogu.gitops.application.content.ContentLoader.
//
// ContentLoader walks `config.content.repos`, pulling each source repo,
// optionally rendering its files through the templating engine, and pushing
// the result into the tenant SCM. Three repo types behave differently:
//
//   - MIRROR (this is the one that is fully implemented in this port).
//     Force-pushes the source repository (history, branches, tags) to the
//     target. Templating is not allowed and a single source/target pair is
//     resolved.
//   - COPY      (STUB). Clones the source, renders templates, then copies the
//     working tree (without .git) into the target repo and commits the
//     result.
//   - FOLDER_BASED (STUB). Same as COPY but the source contains a
//     <namespace>/<name>/ directory tree, each leaf becoming its own target
//     repository.
//
// Two pieces are deliberately left as TODO with clear markers because they
// require infrastructure that is not yet ported in this phase:
//
//   - createImagePullSecrets / deployHelmReleasesFromContent depend on the
//     yet-to-be-ported deployment.Strategy + image-pull-secret service.
//   - The FreeMarker static-class injection (statics:) is replaced by a
//     small Go-friendly Variables map; AllowedStaticsWhitelist is enforced
//     as a key whitelist on the data map (see variables.go).
//
// Order is 200 (last) because content repos may reference repositories
// created by other features and the SCM must be fully usable by then.
package content

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/git"
	"github.com/cloudogu/gitops-playground/go/internal/githandler"
	"github.com/cloudogu/gitops-playground/go/internal/scm"
)

const (
	// FeatureName matches the slog field and CLI label.
	FeatureName = "content-loader"

	// Order matches @Order(999) in ContentLoader.groovy (= last).
	Order = 200

	// Default path inside a content repo. Mirrors
	// ContentRepositorySchema.DEFAULT_PATH.
	DefaultRepoPath = "."

	// Repo types from Config.ContentRepoType.
	RepoTypeMirror      = "MIRROR"
	RepoTypeCopy        = "COPY"
	RepoTypeFolderBased = "FOLDER_BASED"

	// Overwrite modes from Config.OverwriteMode.
	OverwriteInit    = "INIT"
	OverwriteReset   = "RESET"
	OverwriteUpgrade = "UPGRADE"
)

// Feature implements feature.Feature for the content loader.
type Feature struct {
	// GitHandler hands out *git.Repo working copies for the target SCM.
	GitHandler *githandler.Handler

	// SourceGit is the git service used to clone *source* repositories
	// (i.e. the user-provided URLs). Distinct from GitHandler.Git so tests
	// can stub the two independently.
	SourceGit git.Service

	// Tenant is the SCM provider used by Feature itself (e.g. to resolve
	// URL/host/protocol for the templating context).
	Tenant scm.Provider
}

// Name implements feature.Feature.
func (Feature) Name() string { return FeatureName }

// Order implements feature.Feature.
func (Feature) Order() int { return Order }

// IsEnabled returns true when the user configured at least one content repo
// or helm release. Matches the Groovy precondition.
func (Feature) IsEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return len(cfg.Content.Repos) > 0 || len(cfg.Content.HelmReleases) > 0
}

// Namespace returns "" – content-loader has no dedicated namespace.
func (Feature) Namespace(_ *config.Config) string { return "" }

// Disable is a no-op for content-loader.
func (Feature) Disable(_ context.Context, _ *config.Config) error { return nil }

// Install iterates the configured content repos and processes each by its
// Type. Helm releases are handled afterwards.
func (f Feature) Install(ctx context.Context, cfg *config.Config) error {
	if f.GitHandler == nil {
		return fmt.Errorf("%s: GitHandler is required", FeatureName)
	}
	if f.SourceGit == nil {
		return fmt.Errorf("%s: SourceGit is required", FeatureName)
	}

	logger := slog.With("feature", FeatureName)

	// TODO: createImagePullSecrets — depends on the K8s image-pull-secret
	// service which is wired by the runner in a later phase. The Groovy
	// equivalent runs first; once the K8s service interface is stable we'll
	// inject it on Feature and call it here.

	for i, repo := range cfg.Content.Repos {
		if err := ctx.Err(); err != nil {
			return err
		}
		repoLog := logger.With("idx", i, "url", repo.URL, "type", typeOrDefault(repo.Type))

		if err := f.processRepo(ctx, cfg, repo, repoLog); err != nil {
			return fmt.Errorf("%s: repo %s: %w", FeatureName, repo.URL, err)
		}
	}

	// TODO: deployHelmReleasesFromContent — depends on deployment.Strategy
	// which is wired by the runner. See helm_release.go for the planned
	// shape of the call site.
	if len(cfg.Content.HelmReleases) > 0 {
		logger.Warn("content.helmReleases configured but deployment strategy is not yet wired in the Go port; skipping",
			"count", len(cfg.Content.HelmReleases))
	}

	return nil
}

// processRepo dispatches by repo type.
func (f Feature) processRepo(ctx context.Context, cfg *config.Config, repo config.ContentRepositorySchema, logger *slog.Logger) error {
	switch typeOrDefault(repo.Type) {
	case RepoTypeMirror:
		return f.processMirror(ctx, cfg, repo, logger)
	case RepoTypeCopy:
		// STUB
		return f.processCopy(ctx, cfg, repo, logger)
	case RepoTypeFolderBased:
		// STUB
		return f.processFolderBased(ctx, cfg, repo, logger)
	default:
		return fmt.Errorf("unknown content repo type %q", repo.Type)
	}
}

// typeOrDefault returns the repo type or the default (MIRROR) when empty.
// Mirrors ContentRepositorySchema.DEFAULT_TYPE.
func typeOrDefault(t string) string {
	if t == "" {
		return RepoTypeMirror
	}
	return t
}

// pathOrDefault returns the configured path or "." when empty.
func pathOrDefault(p string) string {
	if p == "" {
		return DefaultRepoPath
	}
	return p
}

// overwriteOrDefault returns the configured overwrite mode or INIT.
func overwriteOrDefault(m string) string {
	if m == "" {
		return OverwriteInit
	}
	return m
}
