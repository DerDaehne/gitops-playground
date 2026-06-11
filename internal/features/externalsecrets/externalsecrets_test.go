package externalsecrets

import (
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

func TestIsEnabled(t *testing.T) {
	cfg := config.New()
	if (Feature{}).IsEnabled(cfg) {
		t.Errorf("disabled by default")
	}
	cfg.Features.Secrets.Active = true
	if !(Feature{}).IsEnabled(cfg) {
		t.Errorf("enabled when secrets.active=true")
	}
}

func TestNamespace(t *testing.T) {
	cfg := config.New()
	cfg.Application.NamePrefix = "tenant-"
	if got, want := (Feature{}).Namespace(cfg), "tenant-secrets"; got != want {
		t.Errorf("Namespace=%q want %q", got, want)
	}
}

func TestOrder(t *testing.T) {
	if got, want := (Feature{}).Order(), 120; got != want {
		t.Errorf("Order=%d want %d", got, want)
	}
}

func TestNameAndDisable(t *testing.T) {
	if (Feature{}).Name() != "external-secrets" {
		t.Errorf("Name wrong")
	}
	if err := (Feature{}).Disable(nil, config.New()); err != nil {
		t.Errorf("Disable returned %v", err)
	}
}

func TestBuildValuesMinimal(t *testing.T) {
	cfg := config.New()
	v := buildValues(cfg)
	if len(v) != 0 {
		t.Errorf("expected empty values map by default, got %v", v)
	}
}

func TestBuildValuesSkipCRDs(t *testing.T) {
	cfg := config.New()
	cfg.Application.SkipCRDs = true
	v := buildValues(cfg)
	if v["installCRDs"] != false {
		t.Errorf("installCRDs=%v want false", v["installCRDs"])
	}
}

func TestBuildValuesPodResources(t *testing.T) {
	cfg := config.New()
	cfg.Application.PodResources = true
	v := buildValues(cfg)

	res, ok := v["resources"].(map[string]any)
	if !ok {
		t.Fatalf("top-level resources missing")
	}
	if res["limits"].(map[string]any)["memory"] != "80Mi" {
		t.Errorf("top-level limits.memory=%v", res["limits"])
	}

	cc, ok := v["certController"].(map[string]any)
	if !ok {
		t.Fatalf("certController missing")
	}
	if cc["resources"].(map[string]any)["limits"].(map[string]any)["memory"] != "110Mi" {
		t.Errorf("certController limits wrong")
	}

	wh, ok := v["webhook"].(map[string]any)
	if !ok {
		t.Fatalf("webhook missing")
	}
	if wh["resources"].(map[string]any)["limits"].(map[string]any)["memory"] != "50Mi" {
		t.Errorf("webhook limits wrong")
	}
}

func TestBuildValuesImagePullSecrets(t *testing.T) {
	cfg := config.New()
	cfg.Registry.CreateImagePullSecrets = true
	v := buildValues(cfg)
	ips, ok := v["imagePullSecrets"].([]any)
	if !ok || len(ips) != 1 {
		t.Fatalf("top imagePullSecrets missing: %v", v["imagePullSecrets"])
	}
	if ips[0].(map[string]any)["name"] != "proxy-registry" {
		t.Errorf("wrong name")
	}
}

func TestBuildValuesTopImage(t *testing.T) {
	cfg := config.New()
	cfg.Features.Secrets.ExternalSecrets.Helm.Image = "ghcr.io/external-secrets/external-secrets:v0.10.0"
	v := buildValues(cfg)
	img, ok := v["image"].(map[string]any)
	if !ok {
		t.Fatalf("image missing")
	}
	if img["repository"] != "ghcr.io/external-secrets/external-secrets" {
		t.Errorf("image repo=%v", img["repository"])
	}
	if img["tag"] != "v0.10.0" {
		t.Errorf("image tag=%v", img["tag"])
	}
}

func TestBuildValuesCertControllerImageNoPullSecrets(t *testing.T) {
	cfg := config.New()
	cfg.Features.Secrets.ExternalSecrets.Helm.CertControllerImage = "ghcr.io/eso/cert:1"
	v := buildValues(cfg)
	cc, ok := v["certController"].(map[string]any)
	if !ok {
		t.Fatalf("certController missing")
	}
	if _, ok := cc["resources"]; ok {
		t.Errorf("certController.resources must be absent without podResources")
	}
	if _, ok := cc["imagePullSecrets"]; ok {
		t.Errorf("certController.imagePullSecrets must be absent without createImagePullSecrets")
	}
	img := cc["image"].(map[string]any)
	if img["tag"] != "1" {
		t.Errorf("certController image tag=%v", img["tag"])
	}
}

func TestBuildValuesCertControllerImageWithPullSecrets(t *testing.T) {
	cfg := config.New()
	cfg.Features.Secrets.ExternalSecrets.Helm.CertControllerImage = "ghcr.io/eso/cert:1"
	cfg.Registry.CreateImagePullSecrets = true
	v := buildValues(cfg)
	cc := v["certController"].(map[string]any)
	ips := cc["imagePullSecrets"].([]any)
	if ips[0].(map[string]any)["name"] != "proxy-registry" {
		t.Errorf("certController.imagePullSecrets wrong: %v", ips)
	}
}

func TestBuildValuesWebhookImageWithPullSecrets(t *testing.T) {
	cfg := config.New()
	cfg.Features.Secrets.ExternalSecrets.Helm.WebhookImage = "ghcr.io/eso/webhook:2"
	cfg.Registry.CreateImagePullSecrets = true
	v := buildValues(cfg)
	wh := v["webhook"].(map[string]any)
	ips := wh["imagePullSecrets"].([]any)
	if ips[0].(map[string]any)["name"] != "proxy-registry" {
		t.Errorf("webhook.imagePullSecrets wrong: %v", ips)
	}
	img := wh["image"].(map[string]any)
	if img["tag"] != "2" {
		t.Errorf("webhook image tag=%v", img["tag"])
	}
}

func TestBuildValuesAllTogether(t *testing.T) {
	cfg := config.New()
	cfg.Application.SkipCRDs = true
	cfg.Application.PodResources = true
	cfg.Registry.CreateImagePullSecrets = true
	cfg.Features.Secrets.ExternalSecrets.Helm.Image = "img:1"
	cfg.Features.Secrets.ExternalSecrets.Helm.CertControllerImage = "cert:2"
	cfg.Features.Secrets.ExternalSecrets.Helm.WebhookImage = "wh:3"

	v := buildValues(cfg)

	if v["installCRDs"] != false {
		t.Errorf("installCRDs wrong")
	}
	if _, ok := v["resources"]; !ok {
		t.Errorf("resources missing")
	}
	if _, ok := v["imagePullSecrets"]; !ok {
		t.Errorf("top imagePullSecrets missing")
	}
	cc := v["certController"].(map[string]any)
	if _, ok := cc["resources"]; !ok {
		t.Errorf("cc resources missing")
	}
	if _, ok := cc["image"]; !ok {
		t.Errorf("cc image missing")
	}
	if _, ok := cc["imagePullSecrets"]; !ok {
		t.Errorf("cc imagePullSecrets missing")
	}
	wh := v["webhook"].(map[string]any)
	if _, ok := wh["resources"]; !ok {
		t.Errorf("wh resources missing")
	}
	if _, ok := wh["image"]; !ok {
		t.Errorf("wh image missing")
	}
	if _, ok := wh["imagePullSecrets"]; !ok {
		t.Errorf("wh imagePullSecrets missing")
	}
}

func TestParseImage(t *testing.T) {
	cases := []struct{ in, repo, tag string }{
		{"image:tag", "image", "tag"},
		{"image", "image", "latest"},
		{"localhost:5000/repo:tag", "localhost:5000/repo", "tag"},
	}
	for _, c := range cases {
		repo, tag := parseImage(c.in)
		if repo != c.repo || tag != c.tag {
			t.Errorf("parseImage(%q)=(%q,%q) want (%q,%q)", c.in, repo, tag, c.repo, c.tag)
		}
	}
}
