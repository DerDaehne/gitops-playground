package argocd

// install.go ports the non-operator install path of
// ArgoCD.groovy:deployWithHelm and the matching `installArgoCdSecret`
// post-install patch.
//
// The Groovy original does four things after the cluster-resources repo
// has been pushed:
//
//   1. helmClient.addRepo("argo", <repo from Chart.yaml deps[0]>)
//   2. helmClient.dependencyBuild(<umbrella chart path>)
//   3. helmClient.upgrade("argocd", <umbrella chart path>,
//                         [namespace: namespace])
//   4. patch the argocd-secret with bcrypt(admin password) under
//      data["admin.password"] (base64-encoded) + a fresh
//      data["admin.passwordMtime"] ISO timestamp.
//
// Step (4) is what makes the admin login work after the helm install:
// the upstream chart seeds argocd-secret with a randomly generated
// password; we replace it with a deterministic one the runner can hand
// the user.
//
// Why a hard-coded fallback for the argo helm repo URL:
//
//   The Groovy code reads `dependencies[0].repository` out of Chart.yaml
//   to keep one source of truth. We mirror that by parsing Chart.lock
//   (deterministic format, easier to consume than the comments-heavy
//   Chart.yaml). If parsing fails — e.g. a future Chart.lock format
//   change — we fall back to the canonical upstream
//   `https://argoproj.github.io/argo-helm`. That URL has been stable for
//   the entire life of the project and is what every documented install
//   uses; the fallback is a belt-and-braces guarantee that a parser bug
//   never blocks the install.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/helm"
	"github.com/cloudogu/gitops-playground/go/internal/k8s"
)

// argoCDSecret is the name of the upstream chart's argocd secret that
// holds the bcrypted admin password. Constant so tests can refer to it.
const argoCDSecret = "argocd-secret"

// argoHelmRepoName is the local helm repo alias the umbrella chart's
// Chart.lock references.
const argoHelmRepoName = "argo"

// argoHelmRepoURLFallback is used when Chart.lock cannot be parsed. The
// upstream Argo CD chart has lived at this URL for the entire lifetime
// of the project.
const argoHelmRepoURLFallback = "https://argoproj.github.io/argo-helm"

// secretsGVR is the GVR for core/v1 Secret. Used by the Patch helper;
// Patch insists on an explicit GVR (see internal/k8s/apply.go).
var secretsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}

// K8s is the narrow Kubernetes surface argocd.Feature needs at install
// time. It is defined here (the consumer side) per the cross-package
// dependency rule in AGENTS.md §4.9. The production implementation is
// *internal/k8s.Client; tests can pass a stub built on a fake clientset.
type K8s interface {
	GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error)
	Patch(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, patchType k8s.PatchType, body []byte) error
}

// installViaHelmWithValues runs the three post-push helm steps from
// ArgoCD.groovy:deployWithHelm. It assumes the umbrella chart has
// already been materialised to chartPath (the cluster-resources clone
// dir). valuesPath is optional; the empty string skips the
// --values flag.
func installViaHelmWithValues(ctx context.Context, h *helm.Client, chartPath, namespace, valuesPath string) error {
	if h == nil {
		return errors.New("argocd: helm client is nil")
	}
	repoURL, err := readArgoHelmRepoURL(chartPath)
	if err != nil {
		// Parsing problems are not fatal; the canonical URL is what
		// the upstream chart references anyway.
		slog.Debug("argocd: falling back to canonical argo helm repo url", "err", err, "url", argoHelmRepoURLFallback)
		repoURL = argoHelmRepoURLFallback
	}
	if err := h.AddRepo(ctx, argoHelmRepoName, repoURL); err != nil {
		return fmt.Errorf("argocd: helm repo add: %w", err)
	}
	if err := h.DependencyBuild(ctx, chartPath); err != nil {
		return fmt.Errorf("argocd: helm dependency build: %w", err)
	}
	opts := helm.UpgradeOptions{
		Namespace: namespace,
		CreateNS:  true,
	}
	if valuesPath != "" {
		opts.Values = []string{valuesPath}
	}
	if err := h.Upgrade(ctx, releaseName, chartPath, opts); err != nil {
		return fmt.Errorf("argocd: helm upgrade: %w", err)
	}
	return nil
}

// applyAdminPasswordSecret bcrypts cfg.Application.Password and merge-
// patches the resulting hash into argocd-secret.data["admin.password"]
// + sets data["admin.passwordMtime"] to the current UTC ISO timestamp.
//
// The secret itself is created by the umbrella helm chart, so we wait
// for it to appear before patching. The wait uses the standard
// k8s.Poll so a slow cluster doesn't fail the install spuriously.
func applyAdminPasswordSecret(ctx context.Context, kc K8s, cfg *config.Config, namespace string) error {
	if kc == nil {
		return errors.New("argocd: K8s client is nil")
	}
	if cfg == nil {
		return errors.New("argocd: config is nil")
	}
	if cfg.Application.Password == "" {
		return errors.New("argocd: cfg.Application.Password is empty; cannot set admin password")
	}

	hash, err := HashAdminPassword(cfg.Application.Password)
	if err != nil {
		return err
	}

	if err := waitForSecret(ctx, kc, namespace, argoCDSecret); err != nil {
		return fmt.Errorf("argocd: wait for %s/%s: %w", namespace, argoCDSecret, err)
	}

	// The Groovy code uses `kubectl patch --type=merge` with
	//   stringData: { "admin.password": <bcrypt> }
	// stringData is server-side base64-encoded into data; we patch
	// data["admin.password"] directly with the base64 of the hash so
	// the merge-patch body works against the API server's typed Secret.
	mtime := time.Now().UTC().Format(time.RFC3339)
	patch := map[string]any{
		"data": map[string]string{
			"admin.password":      base64.StdEncoding.EncodeToString([]byte(hash)),
			"admin.passwordMtime": base64.StdEncoding.EncodeToString([]byte(mtime)),
		},
	}
	// JSON-merge patches must be JSON. encoding/json is enough; the body
	// is small and stable.
	body, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("argocd: marshal secret patch: %w", err)
	}
	if err := kc.Patch(ctx, secretsGVR, namespace, argoCDSecret, k8s.PatchJSONMerge, body); err != nil {
		return fmt.Errorf("argocd: patch %s/%s: %w", namespace, argoCDSecret, err)
	}
	return nil
}

// waitForSecret blocks until the named secret exists in namespace or the
// context is cancelled. Uses k8s.Poll so the runner's context deadline
// is honoured.
func waitForSecret(ctx context.Context, kc K8s, namespace, name string) error {
	opts := k8s.DefaultPollOptions(fmt.Sprintf("secret %s/%s", namespace, name))
	_, err := k8s.Poll(ctx, opts, func(ctx context.Context) (*corev1.Secret, bool, error) {
		sec, err := kc.GetSecret(ctx, namespace, name)
		if err != nil {
			// IsNotFound is expected while we wait for the chart to
			// create the secret; everything else is a transient
			// transport error and Poll already retries.
			if apierrors.IsNotFound(err) {
				return nil, false, nil
			}
			return nil, false, err
		}
		return sec, true, nil
	})
	return err
}

// readArgoHelmRepoURL parses Chart.lock and returns the repository URL
// of the first dependency. The umbrella chart has exactly one
// dependency (`argo-cd`), so deps[0] is the right entry; matches the
// Groovy `helmDependencies[0].repository` lookup.
//
// Chart.lock is YAML with a `dependencies:` list of {name, repository,
// version} objects. We deliberately do not parse Chart.yaml here:
// Chart.yaml carries inline comments that have tripped up generic
// parsers in the past, while Chart.lock is helm-generated and
// canonical.
func readArgoHelmRepoURL(chartPath string) (string, error) {
	lockPath := filepath.Join(chartPath, "Chart.lock")
	body, err := os.ReadFile(lockPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", lockPath, err)
	}
	var lock struct {
		Dependencies []struct {
			Name       string `yaml:"name"`
			Repository string `yaml:"repository"`
		} `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(body, &lock); err != nil {
		return "", fmt.Errorf("parse %s: %w", lockPath, err)
	}
	if len(lock.Dependencies) == 0 {
		return "", fmt.Errorf("%s: no dependencies", lockPath)
	}
	if lock.Dependencies[0].Repository == "" {
		return "", fmt.Errorf("%s: first dependency has no repository", lockPath)
	}
	return lock.Dependencies[0].Repository, nil
}
