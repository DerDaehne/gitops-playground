package ingress

import (
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

func TestBuildValuesMinimal(t *testing.T) {
	cfg := config.New()
	v := buildValues(cfg)
	dep, ok := v["deployment"].(map[string]any)
	if !ok {
		t.Fatalf("deployment missing or wrong type")
	}
	if dep["replicaCount"] != 2 {
		t.Errorf("replicaCount: %v", dep["replicaCount"])
	}
	if _, ok := v["metrics"]; ok {
		t.Errorf("metrics must be absent when monitoring disabled")
	}
	if _, ok := dep["imagePullSecrets"]; ok {
		t.Errorf("imagePullSecrets must be absent when createImagePullSecrets disabled")
	}
}

func TestBuildValuesWithMonitoringAndPrefix(t *testing.T) {
	cfg := config.New()
	cfg.Features.Monitoring.Active = true
	cfg.Application.NamePrefix = "tenant-"
	v := buildValues(cfg)
	metrics, ok := v["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("metrics missing")
	}
	sm := metrics["prometheus"].(map[string]any)["serviceMonitor"].(map[string]any)
	if got, want := sm["namespace"], "tenant-monitoring"; got != want {
		t.Errorf("serviceMonitor namespace: got %v, want %v", got, want)
	}
}

func TestBuildValuesNetpols(t *testing.T) {
	cfg := config.New()
	cfg.Application.NetPols = true
	v := buildValues(cfg)
	dep := v["deployment"].(map[string]any)
	if _, ok := dep["networkPolicy"]; !ok {
		t.Errorf("networkPolicy missing when netpols enabled")
	}
}

func TestParseImage(t *testing.T) {
	cases := []struct {
		in, repo, tag string
	}{
		{"traefik:v3", "traefik", "v3"},
		{"traefik", "traefik", "latest"},
		{"localhost:5000/repo:tag", "localhost:5000/repo", "tag"},
		{"localhost:5000/repo", "localhost:5000/repo", "latest"},
	}
	for _, c := range cases {
		repo, tag := parseImage(c.in)
		if repo != c.repo || tag != c.tag {
			t.Errorf("parseImage(%q)=(%q,%q), want (%q,%q)", c.in, repo, tag, c.repo, c.tag)
		}
	}
}
