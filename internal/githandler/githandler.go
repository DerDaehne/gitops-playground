// Package githandler is the Go port of
// com.cloudogu.gitops.application.orchestration.GitHandler.
//
// Unlike a Feature, GitHandler is a small *service* that other features ask
// for a *git.Repo working copy for a (namespace, name) pair on either the
// tenant or central SCM. It owns:
//
//   - The SCM Provider used to create the remote repository on demand and to
//     resolve clone URLs.
//   - A registry of *git.Repo instances keyed by "<namespace>/<name>"
//     so the same working copy is reused across features (the Groovy code
//     went through GitRepoFactory, which kept the same map).
//   - The git.Service used to clone repos lazily.
//
// Differences vs. the Groovy original:
//
//   - The "tenant vs central" split is exposed via two distinct provider
//     fields. ResourcesScm prefers central, falls back to tenant – matching
//     GitHandler.getResourcesScm().
//   - Setup (creating the cluster-resources repo and configuring the
//     SCM-Manager URLs) is NOT done in this package. That belonged to
//     GitHandler.enable() in Groovy, which conflated handler initialisation
//     with the orchestration phase. In the Go port the runner wires this up
//     explicitly via the SCM Provider's CreateRepository before features
//     run.
//   - GitHandler exposes Close so callers can clean up the on-disk
//     working copies it created. This matches the implicit
//     `repoTmpDir.deleteDir()` calls scattered across the Groovy code.
package githandler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cloudogu/gitops-playground/go/internal/git"
	"github.com/cloudogu/gitops-playground/go/internal/scm"
)

// ClusterResourcesRepo is the well-known target name for the cluster-resources
// repo. The Groovy code hardcodes it in GitHandler.setupRepos.
const ClusterResourcesRepo = "argocd/cluster-resources"

// Handler manages git working copies on top of one or two SCM providers.
//
// Zero-value is NOT usable; construct via New().
type Handler struct {
	// Tenant is the per-tenant SCM provider; required.
	Tenant scm.Provider

	// Central is the (optional) shared SCM provider used in multi-tenant
	// setups. ResourcesScm prefers Central over Tenant when set.
	Central scm.Provider

	// Git is the Git service used for clone operations. Required.
	Git git.Service

	// Auth carries the credentials used for clone/push operations.
	Auth git.Auth

	// Author becomes the commit author on every working copy.
	Author git.Identity

	// Insecure, when true, disables TLS verification on all clones/pushes.
	Insecure bool

	// WorkRoot is the directory where lazy clones are placed. Each repo
	// gets a subdirectory "<namespace>__<name>". A fresh temporary
	// directory is created when WorkRoot is empty.
	WorkRoot string

	mu    sync.Mutex
	repos map[string]*git.Repo
	owned []string // directories created under WorkRoot; cleared by Close
}

// New constructs a Handler with the given tenant provider and Git service.
// Optional fields (Central, Auth, Author, Insecure, WorkRoot) can be set
// directly on the returned value before the first Get call.
func New(tenant scm.Provider, gitSvc git.Service) (*Handler, error) {
	if tenant == nil {
		return nil, errors.New("githandler: tenant provider is required")
	}
	if gitSvc == nil {
		return nil, errors.New("githandler: git service is required")
	}
	return &Handler{
		Tenant: tenant,
		Git:    gitSvc,
		repos:  make(map[string]*git.Repo),
	}, nil
}

// Get returns a working copy of namespace/name on the tenant SCM. On the
// first call for a given pair the repository is created on the SCM (if it
// does not exist) and cloned into WorkRoot; subsequent calls return the
// cached *git.Repo.
//
// The clone honours ctx.Done(); a cancelled context aborts both the
// CreateRepository call and the clone.
func (h *Handler) Get(ctx context.Context, namespace, name string) (*git.Repo, error) {
	return h.get(ctx, h.Tenant, namespace, name)
}

// ResourcesScm returns a working copy of the cluster-resources repository
// on the SCM dedicated to shared resources (central when configured, tenant
// otherwise). Matches GitHandler.getResourcesScm in the Groovy original.
func (h *Handler) ResourcesScm(ctx context.Context) (*git.Repo, error) {
	provider := h.Central
	if provider == nil {
		provider = h.Tenant
	}
	if provider == nil {
		return nil, errors.New("githandler: no SCM provider configured")
	}
	ns, name, err := scm.SplitRepoTarget(ClusterResourcesRepo)
	if err != nil {
		return nil, err
	}
	return h.get(ctx, provider, ns, name)
}

// Close removes every working directory created by this handler. It is
// safe to call multiple times; subsequent calls are no-ops.
func (h *Handler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var firstErr error
	for _, p := range h.owned {
		if err := os.RemoveAll(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	h.owned = nil
	h.repos = make(map[string]*git.Repo)
	return firstErr
}

// get is the shared implementation behind Get / ResourcesScm.
func (h *Handler) get(ctx context.Context, provider scm.Provider, namespace, name string) (*git.Repo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, errors.New("githandler: nil SCM provider")
	}
	key := namespace + "/" + name

	h.mu.Lock()
	if r, ok := h.repos[key]; ok {
		h.mu.Unlock()
		return r, nil
	}
	h.mu.Unlock()

	// Lazy-create the remote repository. The Groovy code passes an empty
	// description and init=false; we mirror that.
	if _, err := provider.CreateRepository(ctx, namespace, name, "", false); err != nil {
		return nil, fmt.Errorf("githandler: create %s: %w", key, err)
	}

	cloneURL := provider.RepoURL(namespace, name, scm.RepoURLInCluster)
	if cloneURL == "" {
		return nil, fmt.Errorf("githandler: provider %s returned empty clone URL for %s", provider.Name(), key)
	}

	dir, err := h.ensureWorkDir(namespace, name)
	if err != nil {
		return nil, err
	}

	repo, err := h.Git.Clone(ctx, cloneURL, git.CloneOptions{
		Dir:      dir,
		Auth:     h.Auth,
		Author:   h.Author,
		Insecure: h.Insecure,
	})
	if err != nil {
		return nil, fmt.Errorf("githandler: clone %s: %w", key, err)
	}

	h.mu.Lock()
	h.repos[key] = repo
	h.mu.Unlock()
	return repo, nil
}

// ensureWorkDir picks a per-repo subdirectory below WorkRoot, creating
// WorkRoot lazily when it is empty.
func (h *Handler) ensureWorkDir(namespace, name string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.WorkRoot == "" {
		tmp, err := os.MkdirTemp("", "gop-githandler-")
		if err != nil {
			return "", fmt.Errorf("githandler: create work root: %w", err)
		}
		h.WorkRoot = tmp
		h.owned = append(h.owned, tmp)
	}
	dir := filepath.Join(h.WorkRoot, namespace+"__"+name)
	// go-git's PlainClone requires a non-existent (or empty) directory.
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("githandler: prepare %s: %w", dir, err)
	}
	h.owned = append(h.owned, dir)
	return dir, nil
}
