// Package argocd is the Go port of com.cloudogu.gitops.tools.core.argocd.ArgoCD.
//
// The Groovy ArgoCD class spans three concerns:
//
//  1. Bootstrap the cluster-resources Git repository (clone, copy the
//     template tree, replace freemarker placeholders, commit + push). This
//     is the ArgoCDRepoSetup / RepoLayout / RepoInitializationAction trio.
//     Ported here in setup.go.
//
//  2. Compose the helm-values for the umbrella `argo-cd` chart used in the
//     non-operator code path. The Groovy template lives at
//     argocd/cluster-resources/apps/argocd/argocd/values.ftl.yaml; we
//     mirror it programmatically in values.go (see argoCDServerValues,
//     controllerValues, repoServerValues, notificationsValues,
//     operatorValues).
//
//  3. Deploy ArgoCD itself, branching between operator mode (apply the
//     ArgoCD CR + RBAC and wait for the CR to become Available) and helm
//     mode (umbrella chart upgrade). The runner-level k8s actions (apply,
//     patch, wait) are intentionally still left as interfaces here; phase
//     3 wires them into internal/k8s.
//
// Why no kubernetes calls in Install yet:
//
//   - The Groovy enable() touches K8sClient.patch / applyYaml / delete /
//     waitForResourcePhase. Those operations don't live behind a single
//     interface in the Go port yet (see internal/k8s/*), and the runner is
//     the place where they get wired up.
//   - The Feature struct exposes the required collaborators (Helm, Git,
//     SCM, Deploy, Images). The runner in phase 3b/3c will wrap them and
//     drive the k8s-side actions; this file owns the shape and the
//     branch logic only.
package argocd

import (
	"context"
	"fmt"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/deployment"
	"github.com/cloudogu/gitops-playground/go/internal/feature"
	"github.com/cloudogu/gitops-playground/go/internal/git"
	"github.com/cloudogu/gitops-playground/go/internal/helm"
	"github.com/cloudogu/gitops-playground/go/internal/scm"
)

const (
	// releaseName is the Helm release name for the umbrella chart in
	// non-operator mode. Matches the Groovy helmClient.upgrade("argocd", ...).
	releaseName = "argocd"

	// defaultNamespace is the Groovy fallback for
	// config.features.argocd.namespace when the user leaves it unset –
	// see Config.ArgoCDSchema in the original source.
	defaultNamespace = "argocd"
)

// Feature implements feature.Feature for ArgoCD.
//
// Helm is a *helm.Client (concrete) so we can invoke `helm repo add` and
// `helm dependency build` against the cloned umbrella chart in
// deployWithHelm – those calls have no analogue on the generic
// deployment.Strategy interface.
//
// Deploy is the generic strategy used by other features so that registry
// state stays consistent. ArgoCD's own install path bypasses it (the
// non-operator branch goes straight through Helm).
//
// K8s is the narrow Kubernetes surface used for the post-install
// argocd-secret patch (see install.go). It is an interface to keep
// argocd.Feature testable without a live cluster; the production binding
// is *internal/k8s.Client.
type Feature struct {
	Deploy deployment.Strategy
	Helm   *helm.Client
	Git    git.Service
	SCM    scm.Provider
	Images feature.ImagePullSecretCreator
	K8s    K8s
}

// Name implements feature.Feature.
func (Feature) Name() string { return "argocd" }

// Order matches the Groovy @Order(100).
func (Feature) Order() int { return 100 }

// IsEnabled implements feature.Feature.
func (Feature) IsEnabled(cfg *config.Config) bool { return cfg.Features.ArgoCD.Active }

// Namespace returns "<namePrefix><features.argocd.namespace>". When the
// user leaves features.argocd.namespace empty we fall back to "argocd"
// to match the documented default.
func (Feature) Namespace(cfg *config.Config) string {
	ns := cfg.Features.ArgoCD.Namespace
	if ns == "" {
		ns = defaultNamespace
	}
	return cfg.Application.NamePrefix + ns
}

// Disable is a no-op. Mirrors the Groovy tool: it has no destructive
// uninstall path; the runner deletes the namespace when the user opts in.
func (Feature) Disable(_ context.Context, _ *config.Config) error { return nil }

// PostConfigInit validates features.argocd.env when the operator code
// path is active. The Groovy original walks the list and rejects entries
// that are not a Map with both "name" and "value" keys. Our config
// schema already types Env as []map[string]string, but a user may have
// supplied a partially-filled map via YAML – so we re-validate per entry.
func (Feature) PostConfigInit(cfg *config.Config) error {
	if !cfg.Features.ArgoCD.Operator || len(cfg.Features.ArgoCD.Env) == 0 {
		return nil
	}
	for i, m := range cfg.Features.ArgoCD.Env {
		if m == nil {
			return fmt.Errorf("features.argocd.env[%d]: entry must be a map with 'name' and 'value'", i)
		}
		name, hasName := m["name"]
		value, hasValue := m["value"]
		if !hasName || !hasValue {
			return fmt.Errorf("features.argocd.env[%d]: each env variable must contain both 'name' and 'value' keys (got %v)", i, m)
		}
		if name == "" {
			return fmt.Errorf("features.argocd.env[%d]: 'name' must be non-empty", i)
		}
		// 'value' may legitimately be empty (e.g. to clear an upstream
		// default), so we only insist on the key being present.
		_ = value
	}
	return nil
}

// Install runs the bootstrap. The two code paths in the Groovy enable()
// are preserved verbatim:
//
//	if config.features.argocd.operator:
//	  - prepare the cluster-resources repo (without the helm umbrella tree)
//	  - the runner applies operator/argocd.yaml + waits + patches secrets
//	    + applies RBAC
//	else:
//	  - prepare the cluster-resources repo (without the operator tree)
//	  - render programmatic helm values
//	  - helm repo add / dependency build / upgrade the umbrella chart
//	  - patch the bcrypt'd admin password into argocd-secret.
//
// Operator mode never renders helm values; the runner applies the CR
// imperatively after Install returns.
func (f Feature) Install(ctx context.Context, cfg *config.Config) error {
	ns := f.Namespace(cfg)

	if f.Images != nil {
		if err := feature.EnsureProxyRegistryPullSecret(ctx, f.Images, cfg, ns); err != nil {
			return err
		}
	}

	setup, err := NewRepoSetup(cfg, f.Git, f.SCM)
	if err != nil {
		return fmt.Errorf("argocd: build repo setup: %w", err)
	}

	if err := setup.InitLocalRepos(ctx); err != nil {
		return fmt.Errorf("argocd: init local repos: %w", err)
	}
	if err := setup.PrepareClusterResourcesRepo(); err != nil {
		return fmt.Errorf("argocd: prepare cluster-resources repo: %w", err)
	}
	if err := setup.CommitAndPushAll(ctx, "Initial Commit"); err != nil {
		return fmt.Errorf("argocd: commit/push: %w", err)
	}

	if cfg.Features.ArgoCD.Operator {
		// Operator mode: do NOT helm-install argo-cd. The CR + RBAC
		// were just pushed to the cluster-resources repo. The runner
		// will apply them imperatively (so the cluster bootstraps
		// before ArgoCD itself is up to reconcile its own manifests).
		return nil
	}

	// Non-operator mode: render umbrella values, then `helm repo add` +
	// `helm dependency build` + `helm upgrade -i argocd <chart>`. We
	// don't go through f.Deploy here because the umbrella chart is the
	// one we just pushed to git – it has to be installed imperatively,
	// the same way Registry.groovy bypasses the deployer.
	values := buildValues(cfg)
	valuesPath, cleanup, err := deployment.RenderHelmValues(cfg, deployment.HelmValuesRequest{
		InlineValues: cfg.Features.ArgoCD.Values,
		ExtraValues:  values,
	})
	if err != nil {
		return fmt.Errorf("argocd: render values: %w", err)
	}
	defer cleanup()

	clusterLayout, err := setup.ClusterRepoLayout()
	if err != nil {
		return fmt.Errorf("argocd: locate umbrella chart: %w", err)
	}
	chartPath := clusterLayout.HelmDir()

	if err := installViaHelmWithValues(ctx, f.Helm, chartPath, ns, valuesPath); err != nil {
		return err
	}

	if err := applyAdminPasswordSecret(ctx, f.K8s, cfg, ns); err != nil {
		return err
	}
	return nil
}
