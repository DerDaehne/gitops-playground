package destroy

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	"github.com/cloudogu/gitops-playground/go/internal/helm"
	"github.com/cloudogu/gitops-playground/go/internal/k8s"
)

// ArgoCDHandler tears down Argo CD applications, the umbrella helm release
// and the credential secrets it leaves behind. Mirrors
// destroy/ArgoCDDestructionHandler.groovy.
//
// Simplifications vs. the Groovy original:
//
//   - Instead of cloning the cluster-resources repo to reinstall the
//     umbrella chart "imperatively" before uninstalling (a workaround
//     the Groovy code calls out as "a hack"), we uninstall whatever helm
//     release is already there. The reinstall-then-uninstall dance was
//     needed because the original install path used a different release
//     manager; in the Go port the install also uses helm, so a single
//     uninstall is enough.
type ArgoCDHandler struct {
	K8s  *k8s.Client
	Helm helm.Client
}

// Name implements Handler.
func (ArgoCDHandler) Name() string { return "argocd" }

// Order matches @Order(100) on the Groovy ArgoCDDestructionHandler.
func (ArgoCDHandler) Order() int { return 100 }

// Destroy walks every Argo CD Application in the cluster, removes the
// finalizers that would block bootstrap deletion, deletes the bootstrap
// pyramid in the right order, uninstalls the umbrella release and clears
// the credential secrets.
func (h ArgoCDHandler) Destroy(ctx context.Context, cfg *config.Config) error {
	if h.K8s == nil {
		return fmt.Errorf("argocd destroy: K8s client is nil")
	}
	argoNS := cfg.Application.NamePrefix + cfg.Features.ArgoCD.Namespace

	// Add the finalizer to every Application that is not part of the
	// bootstrap pyramid; without it the deletion below would orphan
	// resources.
	apps, err := h.K8s.ListCustomResources(ctx, k8s.ArgoCDApplicationGVR, "")
	if err == nil {
		patch := []byte(`{"metadata":{"finalizers":["resources-finalizer.argocd.argoproj.io"]}}`)
		for _, app := range apps.Items {
			name := app.GetName()
			if name == "bootstrap" || name == "argocd" || name == "projects" {
				continue
			}
			ns := app.GetNamespace()
			if err := h.K8s.Patch(ctx, k8s.ArgoCDApplicationGVR, ns, name, k8s.PatchJSONMerge, patch); err != nil {
				return fmt.Errorf("patch app %s/%s: %w", ns, name, err)
			}
		}
	}

	// The order matches the Groovy list: bootstrap first so it cannot
	// recreate the others.
	for _, name := range []string{"bootstrap", "cluster-resources", "example-apps"} {
		if err := h.K8s.DeleteCustomResource(ctx, k8s.ArgoCDApplicationGVR, argoNS, name); err != nil {
			return err
		}
	}

	if err := h.Helm.Uninstall(ctx, "argocd", argoNS); err != nil {
		// Already gone is acceptable; we don't crash the rest of destroy.
		// helm.Client.Uninstall returns a stderr-wrapped error; check the
		// error string for an "release not loaded" hint and degrade.
		if !isHelmAlreadyGone(err) {
			return fmt.Errorf("helm uninstall argocd: %w", err)
		}
	}

	// Tidy the remaining Argo CD AppProjects.
	projects, err := h.K8s.ListCustomResources(ctx, k8s.ArgoCDAppProjectGVR, "")
	if err == nil {
		for _, p := range projects.Items {
			if err := h.K8s.DeleteCustomResource(ctx, k8s.ArgoCDAppProjectGVR, p.GetNamespace(), p.GetName()); err != nil {
				return err
			}
		}
	}

	// Remove the leftover credential secrets the install path created.
	for _, s := range []struct{ ns, name string }{
		{"default", "jenkins-credentials"},
		{"default", "argocd-repo-creds-scm"},
	} {
		if err := h.K8s.DeleteSecret(ctx, s.ns, s.name); err != nil {
			return err
		}
	}
	return nil
}

// isHelmAlreadyGone reports whether `err` looks like a "release not found"
// response from the helm CLI. We rely on substring matches because the
// helm binary itself does not expose a stable error code.
func isHelmAlreadyGone(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, hint := range []string{"not found", "Release not loaded", "release: not found"} {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}
