package certmanager

import (
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

func TestBuildValuesMinimal(t *testing.T) {
	cfg := config.New()
	cfg.Features.CertManager.Issuer = "letsencrypt-staging"

	v := buildValues(cfg)

	shim, ok := v["ingressShim"].(map[string]any)
	if !ok {
		t.Fatalf("ingressShim missing or wrong type")
	}
	if got, want := shim["defaultIssuerName"], "letsencrypt-staging"; got != want {
		t.Errorf("defaultIssuerName: got %v, want %v", got, want)
	}
	if got, want := shim["defaultIssuerKind"], "ClusterIssuer"; got != want {
		t.Errorf("defaultIssuerKind: got %v, want %v", got, want)
	}
	if got, want := shim["defaultIssuerGroup"], "cert-manager.io"; got != want {
		t.Errorf("defaultIssuerGroup: got %v, want %v", got, want)
	}

	// CRDs default to enabled (SkipCRDs == false).
	crds, ok := v["crds"].(map[string]any)
	if !ok {
		t.Fatalf("crds missing when SkipCRDs=false")
	}
	if crds["enabled"] != true {
		t.Errorf("crds.enabled: %v", crds["enabled"])
	}

	if _, ok := v["global"]; ok {
		t.Errorf("global must be absent when createImagePullSecrets disabled")
	}
	if _, ok := v["resources"]; ok {
		t.Errorf("resources must be absent when podResources disabled")
	}
	if _, ok := v["image"]; ok {
		t.Errorf("image must be absent when helm.image is empty")
	}
	for _, k := range []string{"webhook", "cainjector", "acmesolver", "startupapicheck"} {
		if _, ok := v[k]; ok {
			t.Errorf("%s must be absent without podResources or component image", k)
		}
	}
}

func TestBuildValuesSkipCRDs(t *testing.T) {
	cfg := config.New()
	cfg.Application.SkipCRDs = true
	v := buildValues(cfg)
	if _, ok := v["crds"]; ok {
		t.Errorf("crds must be absent when SkipCRDs=true")
	}
}

func TestBuildValuesPullSecret(t *testing.T) {
	cfg := config.New()
	cfg.Registry.CreateImagePullSecrets = true
	v := buildValues(cfg)

	g, ok := v["global"].(map[string]any)
	if !ok {
		t.Fatalf("global missing")
	}
	secrets, ok := g["imagePullSecrets"].([]any)
	if !ok || len(secrets) != 1 {
		t.Fatalf("imagePullSecrets shape: %v", g["imagePullSecrets"])
	}
	if name := secrets[0].(map[string]any)["name"]; name != "proxy-registry" {
		t.Errorf("pull secret name: %v", name)
	}
}

func TestBuildValuesPodResources(t *testing.T) {
	cfg := config.New()
	cfg.Application.PodResources = true
	v := buildValues(cfg)

	res, ok := v["resources"].(map[string]any)
	if !ok {
		t.Fatalf("resources missing")
	}
	if mem := res["limits"].(map[string]any)["memory"]; mem != "400Mi" {
		t.Errorf("controller memory limit: %v", mem)
	}

	// All four component blocks must exist with resources but no image.
	for _, name := range []string{"webhook", "cainjector", "acmesolver", "startupapicheck"} {
		block, ok := v[name].(map[string]any)
		if !ok {
			t.Fatalf("%s missing when podResources=true", name)
		}
		if _, ok := block["resources"]; !ok {
			t.Errorf("%s.resources missing", name)
		}
		if _, ok := block["image"]; ok {
			t.Errorf("%s.image must be absent when no image configured", name)
		}
	}

	// Webhook gets the smaller block (30Mi), the others get 400Mi.
	if mem := v["webhook"].(map[string]any)["resources"].(map[string]any)["limits"].(map[string]any)["memory"]; mem != "30Mi" {
		t.Errorf("webhook memory limit: got %v, want 30Mi", mem)
	}
	if mem := v["cainjector"].(map[string]any)["resources"].(map[string]any)["limits"].(map[string]any)["memory"]; mem != "400Mi" {
		t.Errorf("cainjector memory limit: got %v, want 400Mi", mem)
	}
}

func TestBuildValuesMainImage(t *testing.T) {
	cfg := config.New()
	cfg.Features.CertManager.Helm.Image = "quay.io/jetstack/cert-manager-controller:v1.14.0"
	v := buildValues(cfg)

	img, ok := v["image"].(map[string]any)
	if !ok {
		t.Fatalf("image missing")
	}
	if img["repository"] != "quay.io/jetstack/cert-manager-controller" {
		t.Errorf("repository: %v", img["repository"])
	}
	if img["tag"] != "v1.14.0" {
		t.Errorf("tag: %v", img["tag"])
	}
}

func TestBuildValuesComponentImagesOnly(t *testing.T) {
	cfg := config.New()
	cfg.Features.CertManager.Helm.WebhookImage = "quay.io/jetstack/cert-manager-webhook:v1.14.0"
	cfg.Features.CertManager.Helm.CAInjectorImage = "quay.io/jetstack/cert-manager-cainjector:v1.14.0"
	cfg.Features.CertManager.Helm.AcmeSolverImage = "quay.io/jetstack/cert-manager-acmesolver:v1.14.0"
	cfg.Features.CertManager.Helm.StartupAPICheckImage = "quay.io/jetstack/cert-manager-startupapicheck:v1.14.0"

	v := buildValues(cfg)

	cases := map[string]string{
		"webhook":         "quay.io/jetstack/cert-manager-webhook",
		"cainjector":      "quay.io/jetstack/cert-manager-cainjector",
		"acmesolver":      "quay.io/jetstack/cert-manager-acmesolver",
		"startupapicheck": "quay.io/jetstack/cert-manager-startupapicheck",
	}
	for k, wantRepo := range cases {
		block, ok := v[k].(map[string]any)
		if !ok {
			t.Fatalf("%s missing", k)
		}
		if _, ok := block["resources"]; ok {
			t.Errorf("%s.resources must be absent without podResources", k)
		}
		img, ok := block["image"].(map[string]any)
		if !ok {
			t.Fatalf("%s.image missing", k)
		}
		if img["repository"] != wantRepo {
			t.Errorf("%s.image.repository: got %v, want %v", k, img["repository"], wantRepo)
		}
		if img["tag"] != "v1.14.0" {
			t.Errorf("%s.image.tag: %v", k, img["tag"])
		}
	}
}

func TestNamespaceUsesNamePrefix(t *testing.T) {
	cfg := config.New()
	cfg.Application.NamePrefix = "tenant-"
	if got, want := (Feature{}).Namespace(cfg), "tenant-cert-manager"; got != want {
		t.Errorf("Namespace: got %v, want %v", got, want)
	}
}

func TestOrderAndName(t *testing.T) {
	f := Feature{}
	if f.Order() != 160 {
		t.Errorf("Order: %d", f.Order())
	}
	if f.Name() != "cert-manager" {
		t.Errorf("Name: %s", f.Name())
	}
}

func TestIsEnabled(t *testing.T) {
	cfg := config.New()
	if (Feature{}).IsEnabled(cfg) {
		t.Errorf("IsEnabled must default to false")
	}
	cfg.Features.CertManager.Active = true
	if !(Feature{}).IsEnabled(cfg) {
		t.Errorf("IsEnabled must follow CertManager.Active")
	}
}

func TestParseImage(t *testing.T) {
	cases := []struct {
		in, repo, tag string
	}{
		{"cert-manager:v1.14.0", "cert-manager", "v1.14.0"},
		{"cert-manager", "cert-manager", "latest"},
		{"quay.io/jetstack/cert-manager-controller:v1.14.0", "quay.io/jetstack/cert-manager-controller", "v1.14.0"},
		{"localhost:5000/cert-manager:dev", "localhost:5000/cert-manager", "dev"},
	}
	for _, c := range cases {
		repo, tag := parseImage(c.in)
		if repo != c.repo || tag != c.tag {
			t.Errorf("parseImage(%q)=(%q,%q), want (%q,%q)", c.in, repo, tag, c.repo, c.tag)
		}
	}
}
