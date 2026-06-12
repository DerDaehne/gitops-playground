package argocd

// setup.go ports the trio of Groovy classes that handle the
// cluster-resources repo bootstrap:
//
//   - RepoLayout                     – path helpers for the on-disk repo
//   - RepoInitializationAction       – clone + copy the template tree +
//                                       run replaceTemplates + filter the
//                                       subdirs that get copied
//   - ArgoCDRepoSetup                – top-level orchestrator that decides
//                                       which repos exist (single instance
//                                       vs dedicated multi-tenant instance)
//
// The Go port keeps the same names so cross-referencing the Groovy
// source stays cheap, but uses internal/git.Service + internal/scm.Provider
// rather than the Groovy GitRepoFactory/GitProvider duo.
//
// Limitations vs. Groovy (stubbed and tagged with TODO):
//
//   - replaceTemplates: the Groovy code wires a Freemarker model through
//     GitRepo.replaceTemplates which walks the tree and rewrites every
//     `.ftl` file. The Go runner uses internal/template (text/template)
//     instead, but the FTL→tmpl translation is a separate workstream.
//     We expose a hook here and stop at "copy the tree". Phase 3c will
//     wire the templating engine.
//
//   - Multi-tenant dedicated-instance branch: the Groovy code clones two
//     repos (tenant + central). We model this with two RepoInitAction
//     objects, but the config schema still treats MultiTenant as opaque
//     (cfg.MultiTenant.Raw). We read `useDedicatedInstance` from the Raw
//     map and stop the multi-tenant code path there until the typed
//     schema lands.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/git"
	"github.com/cloudogu/gitops-playground/go/internal/scm"
)

// -----------------------------------------------------------------------------
// RepoLayout – path helpers (port of RepoLayout.groovy).
// -----------------------------------------------------------------------------

const (
	appsMonitoringDir  = "apps/monitoring"
	appsSecretsDir     = "apps/external-secrets"
	appsVaultDir       = "apps/vault"
	appsCertManagerDir = "apps/cert-manager"
	appsJenkinsDir     = "apps/jenkins"
	appsIngressDir     = "apps/ingress"
	appsScmManagerDir  = "apps/scm-manager"
	appsArgoCDDir      = "apps/argocd"

	operatorSubDir    = "operator"
	multiTenantSubDir = "multiTenant"
	applicationsDir   = "applications"
	projectsDir       = "projects"
	helmSubDir        = "argocd" // argocd/argocd
	netpolYAML        = "templates/allow-namespaces.yaml"
)

// RepoLayout is a thin wrapper around the repo root directory that
// computes well-known paths.
type RepoLayout struct {
	RepoRootDir string
}

// RootDir returns the repo root.
func (l RepoLayout) RootDir() string { return l.RepoRootDir }

// ArgoCDRoot returns "<root>/apps/argocd".
func (l RepoLayout) ArgoCDRoot() string {
	return filepath.Join(l.RepoRootDir, appsArgoCDDir)
}

// OperatorDir returns "<root>/apps/argocd/operator".
func (l RepoLayout) OperatorDir() string { return filepath.Join(l.ArgoCDRoot(), operatorSubDir) }

// OperatorRbacDir returns "<root>/apps/argocd/operator/rbac".
func (l RepoLayout) OperatorRbacDir() string { return filepath.Join(l.OperatorDir(), "rbac") }

// OperatorConfigFile returns "<root>/apps/argocd/operator/argocd.yaml".
func (l RepoLayout) OperatorConfigFile() string {
	return filepath.Join(l.OperatorDir(), "argocd.yaml")
}

// MultiTenantDir returns "<root>/apps/argocd/multiTenant".
func (l RepoLayout) MultiTenantDir() string {
	return filepath.Join(l.ArgoCDRoot(), multiTenantSubDir)
}

// ApplicationsDir returns "<root>/apps/argocd/applications".
func (l RepoLayout) ApplicationsDir() string {
	return filepath.Join(l.ArgoCDRoot(), applicationsDir)
}

// ProjectsDir returns "<root>/apps/argocd/projects".
func (l RepoLayout) ProjectsDir() string {
	return filepath.Join(l.ArgoCDRoot(), projectsDir)
}

// HelmDir returns "<root>/apps/argocd/argocd" (the umbrella chart).
func (l RepoLayout) HelmDir() string { return filepath.Join(l.ArgoCDRoot(), helmSubDir) }

// HelmValuesFile returns "<root>/apps/argocd/argocd/values.yaml".
func (l RepoLayout) HelmValuesFile() string { return filepath.Join(l.HelmDir(), "values.yaml") }

// ChartYAML returns "<root>/apps/argocd/argocd/Chart.yaml".
func (l RepoLayout) ChartYAML() string { return filepath.Join(l.HelmDir(), "Chart.yaml") }

// NetpolFile returns the path to the umbrella chart's NetworkPolicy template.
func (l RepoLayout) NetpolFile() string { return filepath.Join(l.HelmDir(), netpolYAML) }

// MonitoringDir returns "<root>/apps/monitoring".
func (l RepoLayout) MonitoringDir() string { return filepath.Join(l.RepoRootDir, appsMonitoringDir) }

// VaultDir returns "<root>/apps/vault".
func (l RepoLayout) VaultDir() string { return filepath.Join(l.RepoRootDir, appsVaultDir) }

// argocdSubdirRel returns the constant relative path for the argocd app –
// used by ArgoCDRepoSetup.determineClusterResourceSubDirs.
func argocdSubdirRel() string      { return appsArgoCDDir }
func certManagerSubdirRel() string { return appsCertManagerDir }
func ingressSubdirRel() string     { return appsIngressDir }
func jenkinsSubdirRel() string     { return appsJenkinsDir }
func monitoringSubdirRel() string  { return appsMonitoringDir }
func scmManagerSubdirRel() string  { return appsScmManagerDir }
func secretsSubdirRel() string     { return appsSecretsDir }
func vaultSubdirRel() string       { return appsVaultDir }

// OperatorRbacSubfolder returns "argocd/operator/rbac" – the relative path
// used by the (not-yet-ported) RBAC generator.
func OperatorRbacSubfolder() string {
	return appsArgoCDDir + "/" + operatorSubDir + "/rbac"
}

// OperatorRbacTenantSubfolder returns "argocd/operator/rbac/tenant".
func OperatorRbacTenantSubfolder() string { return OperatorRbacSubfolder() + "/tenant" }

// -----------------------------------------------------------------------------
// RepoInitializationAction – port of RepoInitializationAction.groovy.
// -----------------------------------------------------------------------------

// RepoInitializationAction clones a single repo (via scm.Provider +
// git.Service), copies the configured subdirectories from the embedded
// template tree and runs replaceTemplates.
//
// templateFS / copyFromDir replace the Groovy "working dir + relative
// path" pair: templateFS is the embedded fs.FS produced by
// ClusterResourcesFS, and copyFromDir is the subtree inside it that
// should be projected over the cloned repo. Two values are special:
//
//   - "" or "." means "copy the whole cluster-resources tree" (this is
//     the single-instance + dedicated-cluster repo case).
//   - "apps/argocd/multiTenant/tenant" is what the dedicated multi-
//     tenant code path uses for the tenant bootstrap repo.
//
// The on-disk read path that lived here in phase 3a is gone; the
// runner no longer needs a working directory that mirrors
// retired/argocd/cluster-resources.
type RepoInitializationAction struct {
	cfg           *config.Config
	git           git.Service
	provider      scm.Provider
	repoTarget    string // "argocd/cluster-resources"
	templateFS    fs.FS  // embedded cluster-resources tree (see assets.go)
	copyFromDir   string // sub-path inside templateFS, "" or "." = whole tree
	subDirsToCopy map[string]struct{}
	cloneDir      string    // populated after initLocalRepo
	repo          *git.Repo // populated after initLocalRepo
}

// SetSubDirsToCopy stores the prefix list. Pass relative paths like
// "apps/monitoring"; "" / "/" entries are skipped.
func (r *RepoInitializationAction) SetSubDirsToCopy(subs []string) {
	r.subDirsToCopy = make(map[string]struct{}, len(subs))
	for _, s := range subs {
		s = strings.Trim(s, "/")
		if s == "" {
			continue
		}
		r.subDirsToCopy[s] = struct{}{}
	}
}

// Repo returns the underlying *git.Repo. Nil until InitLocalRepo runs.
func (r *RepoInitializationAction) Repo() *git.Repo { return r.repo }

// RepoTarget returns the "namespace/name" string.
func (r *RepoInitializationAction) RepoTarget() string { return r.repoTarget }

// scmAuth pulls the username/password the configurator placed under
// cfg.Scm.Raw["scmManager"] so git Clone/Push can authenticate against
// the bootstrap SCM. Empty when neither field is set.
func (r *RepoInitializationAction) scmAuth() git.Auth {
	if r.cfg == nil || r.cfg.Scm.Raw == nil {
		return git.Auth{}
	}
	scmm, _ := r.cfg.Scm.Raw["scmManager"].(map[string]any)
	if scmm == nil {
		return git.Auth{}
	}
	user, _ := scmm["username"].(string)
	pass, _ := scmm["password"].(string)
	return git.Auth{Username: user, Password: pass}
}

// InitLocalRepo clones the SCM-side repo into a fresh temp dir and copies
// the configured subdirectories from copyFromDir on top of it. Returns the
// clone dir path.
func (r *RepoInitializationAction) InitLocalRepo(ctx context.Context) error {
	ns, name, err := scm.SplitRepoTarget(r.repoTarget)
	if err != nil {
		return err
	}
	// Ensure the SCM repo exists before cloning. CreateRepository is
	// idempotent: it returns (false, nil) when the repo already exists.
	if _, err := r.provider.CreateRepository(ctx, ns, name, "ArgoCD cluster-resources repo", true); err != nil {
		return fmt.Errorf("argocd: ensure scm repo %s/%s: %w", ns, name, err)
	}

	dir, err := os.MkdirTemp("", "argocd-clone-")
	if err != nil {
		return fmt.Errorf("argocd: temp dir: %w", err)
	}
	// go-git's PlainCloneContext errors out if the target already exists,
	// so MkdirTemp+rmdir is the canonical pattern.
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("argocd: clear temp dir: %w", err)
	}

	url := r.provider.RepoURL(ns, name, scm.RepoURLClient)
	repo, err := r.git.Clone(ctx, url, git.CloneOptions{
		Dir:  dir,
		Auth: r.scmAuth(),
		Author: git.Identity{
			Name:  r.cfg.Application.GitName,
			Email: r.cfg.Application.GitEmail,
		},
		Insecure: r.cfg.Application.Insecure,
	})
	if err != nil {
		return fmt.Errorf("argocd: clone %s: %w", url, err)
	}
	r.repo = repo
	r.cloneDir = dir

	if err := r.copyTree(); err != nil {
		return fmt.Errorf("argocd: copy template tree: %w", err)
	}
	if err := r.replaceTemplates(); err != nil {
		return fmt.Errorf("argocd: replace templates: %w", err)
	}
	return nil
}

// copyTree walks copyFromDir inside templateFS and copies the files
// that pass the prefix filter into r.cloneDir. The filter mirrors the
// Groovy RepoInitializationAction.createSubdirFilter logic:
//
//   - always copy the root entry
//   - never copy anything under apps/<feature>/templates/** EXCEPT the
//     argocd chart's own templates (apps/argocd/argocd/templates/**)
//   - when subDirsToCopy is non-empty, only files inside one of those
//     prefixes are copied (their parent dirs are kept for structure)
//
// The on-disk version used filepath.WalkDir; this one walks the
// embedded fs.FS with fs.WalkDir. The semantics are identical because
// the same prefix filter runs over the same relative paths.
func (r *RepoInitializationAction) copyTree() error {
	if r.templateFS == nil {
		return errors.New("argocd: templateFS not set on RepoInitializationAction")
	}
	srcRoot := r.copyFromDir
	if srcRoot == "" {
		srcRoot = "."
	}
	// fs.FS uses forward slashes everywhere; reject Windows-style
	// separators a future caller might pass in.
	srcRoot = strings.TrimPrefix(filepath.ToSlash(srcRoot), "./")
	if srcRoot == "" {
		srcRoot = "."
	}

	info, err := fs.Stat(r.templateFS, srcRoot)
	if err != nil {
		return fmt.Errorf("argocd: stat template source %q: %w", srcRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("argocd: template source %q is not a directory", srcRoot)
	}

	// Pre-build the relative copy prefixes once.
	prefixes := make([]string, 0, len(r.subDirsToCopy))
	for s := range r.subDirsToCopy {
		prefixes = append(prefixes, filepath.ToSlash(s)+"/")
	}
	hasPrefixes := len(prefixes) > 0
	templateInclude := "apps/argocd/argocd/templates/"

	return fs.WalkDir(r.templateFS, srcRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// fs.WalkDir hands us paths with forward slashes already.
		var relSlash string
		if srcRoot == "." {
			relSlash = path
		} else {
			relSlash = strings.TrimPrefix(path, srcRoot+"/")
			if path == srcRoot {
				relSlash = "."
			}
		}
		isDir := d.IsDir()

		// Always copy the root.
		if relSlash == "" || relSlash == "." {
			return os.MkdirAll(r.cloneDir, 0o755)
		}

		relDir := relSlash
		if !strings.HasSuffix(relDir, "/") {
			relDir += "/"
		}

		// Exception: keep the argocd chart's own templates.
		probe := relSlash
		if isDir {
			probe = relDir
		}
		keepTemplate := strings.HasPrefix(probe, templateInclude)

		// Global excludes for apps/<feature>/templates/**.
		if !keepTemplate && strings.HasPrefix(relSlash, "apps/") && strings.Contains(relDir, "/templates/") {
			if isDir {
				return fs.SkipDir
			}
			return nil
		}

		// Subdir filter.
		if hasPrefixes && !keepTemplate {
			if isDir {
				keep := false
				for _, p := range prefixes {
					if relDir == p || strings.HasPrefix(relDir, p) || strings.HasPrefix(p, relDir) {
						keep = true
						break
					}
				}
				if !keep {
					return fs.SkipDir
				}
			} else {
				keep := false
				for _, p := range prefixes {
					if strings.HasPrefix(relSlash, p) {
						keep = true
						break
					}
				}
				if !keep {
					return nil
				}
			}
		}

		// rel for OS paths: convert forward to filepath separators.
		dest := filepath.Join(r.cloneDir, filepath.FromSlash(relSlash))
		if isDir {
			return os.MkdirAll(dest, 0o755)
		}
		return copyEmbeddedFile(r.templateFS, path, dest)
	})
}

// replaceTemplates is intentionally a stub: the Groovy code drives a
// Freemarker engine over the cloned tree, but the Go port uses
// text/template instead, and the FTL→tmpl translation is its own
// workstream (see internal/template). For phase 3a we copy the .ftl
// files verbatim and rely on the runner to either translate them or have
// the user run the original Groovy generator once.
//
// The hook stays so the runner can replace it later without touching
// every caller.
func (r *RepoInitializationAction) replaceTemplates() error {
	// TODO(phase 3c): walk r.cloneDir, find every "*.ftl*" file, run it
	// through internal/template.RenderFile with buildTemplateValues, and
	// write the result back without the ".ftl" suffix. The template
	// model is buildTemplateValues(r.cfg).
	return nil
}

// buildTemplateValues mirrors the Groovy buildTemplateValues. Kept here
// because replaceTemplates will use it once the FTL translation lands.
//
//nolint:unused // wired up by replaceTemplates once templating is in.
func (r *RepoInitializationAction) buildTemplateValues() map[string]any {
	cfg := r.cfg
	host := argocdHost(cfg.Features.ArgoCD.URL)

	ns, name, _ := scm.SplitRepoTarget(r.repoTarget)
	repoURL := r.provider.RepoURL(ns, name, scm.RepoURLClient)

	return map[string]any{
		"tenantName": cfg.Application.TenantName(),
		"argocd":     map[string]any{"host": host},
		"scm": map[string]any{
			"repoUrl":       repoURL,
			"centralScmUrl": centralSCMURL(cfg),
		},
		"config": cfg,
	}
}

// -----------------------------------------------------------------------------
// ArgoCDRepoSetup – port of ArgoCDRepoSetup.groovy.
// -----------------------------------------------------------------------------

// ArgoCDRepoSetup decides which repos to initialise and runs the
// bootstrap actions over them. The Groovy class has two static factories
// (multi-tenant vs. single-instance); here we collapse them into
// NewRepoSetup which inspects cfg.MultiTenant.Raw["useDedicatedInstance"].
type ArgoCDRepoSetup struct {
	cfg              *config.Config
	clusterResources *RepoInitializationAction
	tenantBootstrap  *RepoInitializationAction // nil in single-instance mode
	all              []*RepoInitializationAction
}

// NewRepoSetup builds an ArgoCDRepoSetup. The template source is the
// embedded cluster-resources tree (see assets.go); the runner no
// longer has to mount a working directory that shadows
// retired/argocd/cluster-resources.
func NewRepoSetup(cfg *config.Config, gitSvc git.Service, prov scm.Provider) (*ArgoCDRepoSetup, error) {
	if cfg == nil {
		return nil, errors.New("argocd: NewRepoSetup requires cfg")
	}
	if gitSvc == nil {
		return nil, errors.New("argocd: NewRepoSetup requires git.Service")
	}
	if prov == nil {
		return nil, errors.New("argocd: NewRepoSetup requires scm.Provider")
	}

	tmplFS, err := ClusterResourcesFS()
	if err != nil {
		return nil, fmt.Errorf("argocd: load embedded cluster-resources: %w", err)
	}

	dedicated := isDedicated(cfg)

	all := []*RepoInitializationAction{}
	var tenant *RepoInitializationAction
	var cluster *RepoInitializationAction

	if dedicated {
		tenant = &RepoInitializationAction{
			cfg:         cfg,
			git:         gitSvc,
			provider:    prov,
			repoTarget:  "argocd/cluster-resources",
			templateFS:  tmplFS,
			copyFromDir: "apps/argocd/multiTenant/tenant",
		}
		cluster = &RepoInitializationAction{
			cfg:         cfg,
			git:         gitSvc,
			provider:    prov,
			repoTarget:  "argocd/cluster-resources",
			templateFS:  tmplFS,
			copyFromDir: ".",
		}
		all = append(all, tenant, cluster)
	} else {
		cluster = &RepoInitializationAction{
			cfg:         cfg,
			git:         gitSvc,
			provider:    prov,
			repoTarget:  "argocd/cluster-resources",
			templateFS:  tmplFS,
			copyFromDir: ".",
		}
		all = append(all, cluster)
	}

	cluster.SetSubDirsToCopy(determineClusterResourceSubDirs(cfg))

	return &ArgoCDRepoSetup{
		cfg:              cfg,
		clusterResources: cluster,
		tenantBootstrap:  tenant,
		all:              all,
	}, nil
}

// ClusterRepoLayout returns the layout helper for the cluster-resources
// repo. Mirrors ArgoCDRepoSetup.clusterRepoLayout in Groovy. Requires
// InitLocalRepos to have run.
func (s *ArgoCDRepoSetup) ClusterRepoLayout() (RepoLayout, error) {
	if s.clusterResources.cloneDir == "" {
		return RepoLayout{}, errors.New("argocd: clusterResources repo not initialised yet")
	}
	return RepoLayout{RepoRootDir: s.clusterResources.cloneDir}, nil
}

// TenantRepoLayout mirrors ArgoCDRepoSetup.tenantRepoLayout. Returns an
// error in single-instance mode.
func (s *ArgoCDRepoSetup) TenantRepoLayout() (RepoLayout, error) {
	if s.tenantBootstrap == nil {
		return RepoLayout{}, errors.New("argocd: tenantBootstrap repo is not initialised (single-instance mode)")
	}
	if s.tenantBootstrap.cloneDir == "" {
		return RepoLayout{}, errors.New("argocd: tenantBootstrap repo not cloned yet")
	}
	return RepoLayout{RepoRootDir: s.tenantBootstrap.cloneDir}, nil
}

// ClusterResources returns the cluster-resources init action (read-only).
func (s *ArgoCDRepoSetup) ClusterResources() *RepoInitializationAction { return s.clusterResources }

// TenantBootstrap returns the tenant-bootstrap init action, or nil.
func (s *ArgoCDRepoSetup) TenantBootstrap() *RepoInitializationAction { return s.tenantBootstrap }

// InitLocalRepos runs InitLocalRepo on every action in registration
// order. Mirrors ArgoCDRepoSetup.initLocalRepos.
func (s *ArgoCDRepoSetup) InitLocalRepos(ctx context.Context) error {
	for _, a := range s.all {
		if err := a.InitLocalRepo(ctx); err != nil {
			return err
		}
	}
	return nil
}

// PrepareClusterResourcesRepo trims the cluster-resources tree to match
// the active operator/single-tenant/netpols flags. Mirrors the same-named
// Groovy method.
func (s *ArgoCDRepoSetup) PrepareClusterResourcesRepo() error {
	layout, err := s.ClusterRepoLayout()
	if err != nil {
		return err
	}

	if s.cfg.Features.ArgoCD.Operator {
		// Operator mode owns the operator/ subtree; the umbrella helm
		// dir is dead weight.
		if err := os.RemoveAll(layout.HelmDir()); err != nil {
			return fmt.Errorf("argocd: remove helm dir: %w", err)
		}
	} else {
		if err := os.RemoveAll(layout.OperatorDir()); err != nil {
			return fmt.Errorf("argocd: remove operator dir: %w", err)
		}
	}

	if isDedicated(s.cfg) {
		// Dedicated multi-tenant: replace the central/* tree with what
		// lives under multiTenant/central and drop the rest.
		if err := os.RemoveAll(layout.ApplicationsDir()); err != nil {
			return fmt.Errorf("argocd: remove applications dir: %w", err)
		}
		if err := os.RemoveAll(layout.ProjectsDir()); err != nil {
			return fmt.Errorf("argocd: remove projects dir: %w", err)
		}
		central := filepath.Join(layout.MultiTenantDir(), "central")
		if err := moveDirMerge(central, layout.ArgoCDRoot()); err != nil {
			return fmt.Errorf("argocd: move central tree: %w", err)
		}
		if err := os.RemoveAll(layout.MultiTenantDir()); err != nil {
			return fmt.Errorf("argocd: remove multiTenant dir: %w", err)
		}
	} else {
		if err := os.RemoveAll(layout.MultiTenantDir()); err != nil {
			return fmt.Errorf("argocd: remove multiTenant dir: %w", err)
		}
	}

	if !s.cfg.Application.NetPols {
		if err := os.Remove(layout.NetpolFile()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("argocd: remove netpol file: %w", err)
		}
	}
	return nil
}

// CommitAndPushAll commits and pushes every repo. Mirrors
// ArgoCDRepoSetup.commitAndPushAll.
func (s *ArgoCDRepoSetup) CommitAndPushAll(ctx context.Context, message string) error {
	for _, a := range s.all {
		if a.repo == nil {
			return errors.New("argocd: commit before InitLocalRepos")
		}
		if err := s.commitAndPush(ctx, a, message); err != nil {
			return err
		}
	}
	return nil
}

func (s *ArgoCDRepoSetup) commitAndPush(ctx context.Context, a *RepoInitializationAction, message string) error {
	if err := a.git.Commit(a.repo, message, git.CommitOptions{}); err != nil {
		if errors.Is(err, git.ErrNothingToCommit) {
			return nil
		}
		return fmt.Errorf("argocd: commit %s: %w", a.repoTarget, err)
	}
	if err := a.git.Push(ctx, a.repo, git.PushOptions{}); err != nil {
		return fmt.Errorf("argocd: push %s: %w", a.repoTarget, err)
	}
	return nil
}

// determineClusterResourceSubDirs mirrors the same-named static method.
// The order of insertion is preserved by walking a fixed sequence – Go
// maps would scramble it.
func determineClusterResourceSubDirs(cfg *config.Config) []string {
	out := []string{argocdSubdirRel()}
	if cfg.Features.CertManager.Active {
		out = append(out, certManagerSubdirRel())
	}
	if cfg.Features.Ingress.Active {
		out = append(out, ingressSubdirRel())
	}
	if cfg.Jenkins.Active {
		out = append(out, jenkinsSubdirRel())
	}
	if cfg.Features.Monitoring.Active {
		out = append(out, monitoringSubdirRel())
	}
	if u := scmManagerURL(cfg); u != "" {
		out = append(out, scmManagerSubdirRel())
	}
	if cfg.Features.Secrets.Active {
		out = append(out, secretsSubdirRel(), vaultSubdirRel())
	}
	return out
}

// isDedicated reads cfg.MultiTenant.Raw["useDedicatedInstance"]. Until
// the typed schema lands this remains the canonical access point.
func isDedicated(cfg *config.Config) bool {
	if cfg.MultiTenant.Raw == nil {
		return false
	}
	b, _ := cfg.MultiTenant.Raw["useDedicatedInstance"].(bool)
	return b
}

// scmManagerURL reads cfg.Scm.Raw["scmManager"]["url"] without crashing
// on missing/empty intermediate maps.
func scmManagerURL(cfg *config.Config) string {
	if cfg.Scm.Raw == nil {
		return ""
	}
	scmm, _ := cfg.Scm.Raw["scmManager"].(map[string]any)
	if scmm == nil {
		return ""
	}
	u, _ := scmm["url"].(string)
	return u
}

// centralSCMURL returns the multi-tenant "central" repo URL prefix, or
// "" when not in dedicated mode. The Groovy buildTemplateValues asks
// gitHandler.central?.repoPrefix(); we don't have a GitHandler in Go
// yet, so we surface the raw URL from the multiTenant config.
//
//nolint:unused // Consumed once GitHandler.central is wired (REMAINING P1).
func centralSCMURL(cfg *config.Config) string {
	if !isDedicated(cfg) {
		return ""
	}
	if cfg.MultiTenant.Raw == nil {
		return ""
	}
	c, _ := cfg.MultiTenant.Raw["central"].(map[string]any)
	if c == nil {
		return ""
	}
	u, _ := c["url"].(string)
	return u
}

// -----------------------------------------------------------------------------
// Small file-system helpers. Kept private; the runner has its own fsutil.
// -----------------------------------------------------------------------------

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// copyEmbeddedFile streams an entry out of an fs.FS (typically the
// embedded cluster-resources tree) onto disk. The destination's parent
// directory is created lazily so callers don't have to pre-walk.
func copyEmbeddedFile(src fs.FS, srcPath, dst string) error {
	in, err := src.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// moveDirMerge mirrors fileSystemUtils.moveDirectoryMergeOverwrite. It
// recursively moves src into dst, overwriting files as it goes. When
// src does not exist this is a no-op.
func moveDirMerge(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		// Remove any existing dest, then move (overwrite semantics).
		_ = os.Remove(target)
		if err := os.Rename(path, target); err != nil {
			// Cross-device renames fail with EXDEV; fall back to copy.
			if cerr := copyFile(path, target); cerr != nil {
				return cerr
			}
			return os.Remove(path)
		}
		return nil
	})
}
