package scmmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	scmmclient "github.com/cloudogu/gitops-playground/go/internal/scm/scmmanager"
)

// withInternalScm returns a config with the bare-minimum scm.scmManager
// block populated so isInternal/buildValues have something to read.
func withInternalScm(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.New()
	cfg.Scm.Raw = map[string]any{
		"scmManager": map[string]any{
			"url":      "",
			"username": "admin",
			"password": "admin",
		},
	}
	return cfg
}

func TestIsEnabled_InternalDefaultsToTrue(t *testing.T) {
	cfg := config.New()
	// No scm.Raw at all → treated as internal (default).
	if !isInternal(cfg) {
		t.Fatalf("isInternal should default to true when no scm block is set")
	}
	if !(Feature{}).IsEnabled(cfg) {
		t.Fatalf("IsEnabled should return true for a default config")
	}
}

func TestIsEnabled_EmptyURLIsInternal(t *testing.T) {
	cfg := withInternalScm(t)
	if !isInternal(cfg) {
		t.Fatalf("empty url should mean internal")
	}
	if !(Feature{}).IsEnabled(cfg) {
		t.Fatalf("IsEnabled should be true when url == \"\"")
	}
}

func TestIsEnabled_ExternalURLDisablesFeature(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Scm.Raw["scmManager"].(map[string]any)["url"] = "https://scmm.example.org"
	if isInternal(cfg) {
		t.Fatalf("non-empty url should switch off internal mode")
	}
	if (Feature{}).IsEnabled(cfg) {
		t.Fatalf("IsEnabled should be false when an external URL is set")
	}
}

func TestNamespace_UsesPrefixAndOverride(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Application.NamePrefix = "tenant-"
	if got, want := (Feature{}).Namespace(cfg), "tenant-scm-manager"; got != want {
		t.Errorf("Namespace = %q, want %q", got, want)
	}

	cfg.Scm.Raw["scmManager"].(map[string]any)["namespace"] = "scmm-custom"
	if got, want := (Feature{}).Namespace(cfg), "tenant-scmm-custom"; got != want {
		t.Errorf("Namespace (override) = %q, want %q", got, want)
	}
}

func TestValidate_RequiresCredentials(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Scm.Raw["scmManager"].(map[string]any)["password"] = ""
	if err := (Feature{}).Validate(context.Background(), cfg); err == nil {
		t.Fatalf("Validate should error on empty password")
	}
}

func TestValidate_ExternalSkipsChecks(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Scm.Raw["scmManager"].(map[string]any)["url"] = "https://scmm.example.org"
	cfg.Scm.Raw["scmManager"].(map[string]any)["password"] = ""
	if err := (Feature{}).Validate(context.Background(), cfg); err != nil {
		t.Fatalf("Validate should be a no-op when external, got %v", err)
	}
}

func TestValidate_RequiresUrlForJenkinsWhenActive(t *testing.T) {
	cfg := withInternalScm(t)
	f := Feature{JenkinsActive: func(*config.Config) bool { return true }}
	if err := f.Validate(context.Background(), cfg); err == nil {
		t.Fatalf("Validate should error when jenkins is active and urlForJenkins is empty")
	}
	cfg.Scm.Raw["scmManager"].(map[string]any)["urlForJenkins"] = "http://jenkins.example/"
	if err := f.Validate(context.Background(), cfg); err != nil {
		t.Fatalf("Validate should pass once urlForJenkins is set, got %v", err)
	}
}

func TestBuildValues_BaseShape(t *testing.T) {
	cfg := withInternalScm(t)
	v := buildValues(cfg)

	if got := v["fullnameOverride"]; got != releaseName {
		t.Errorf("fullnameOverride = %v, want %q", got, releaseName)
	}
	svc, ok := v["service"].(map[string]any)
	if !ok || svc["type"] != "NodePort" {
		t.Errorf("service = %+v", v["service"])
	}
	if _, ok := v["ingress"]; ok {
		t.Errorf("ingress must be absent when scmManager.ingress is empty")
	}
	env, _ := v["extraEnv"].(string)
	if !strings.Contains(env, `value: "admin"`) {
		t.Errorf("extraEnv should embed the configured user/password: %q", env)
	}
}

func TestBuildValues_IngressWithCertManager(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Scm.Raw["scmManager"].(map[string]any)["ingress"] = "scmm.example.org"
	cfg.Features.CertManager.Active = true
	cfg.Features.CertManager.Issuer = "letsencrypt"

	v := buildValues(cfg)
	ing, ok := v["ingress"].(map[string]any)
	if !ok {
		t.Fatalf("ingress missing: %+v", v)
	}
	if ing["enabled"] != true || ing["path"] != "/" {
		t.Errorf("ingress base = %+v", ing)
	}
	hosts, _ := ing["hosts"].([]any)
	if len(hosts) != 1 || hosts[0] != "scmm.example.org" {
		t.Errorf("ingress.hosts = %+v", hosts)
	}
	ann, _ := ing["annotations"].(map[string]any)
	if ann["cert-manager.io/cluster-issuer"] != "letsencrypt" {
		t.Errorf("annotations = %+v", ann)
	}
	tls, _ := ing["tls"].([]any)
	if len(tls) != 1 {
		t.Fatalf("tls block missing: %+v", ing)
	}
	tlsEntry, _ := tls[0].(map[string]any)
	if tlsEntry["secretName"] != "scm-manager-tls" {
		t.Errorf("tls secretName = %v", tlsEntry["secretName"])
	}
}

func TestBuildValues_IngressWithoutCertManager(t *testing.T) {
	cfg := withInternalScm(t)
	cfg.Scm.Raw["scmManager"].(map[string]any)["ingress"] = "scmm.example.org"

	v := buildValues(cfg)
	ing, _ := v["ingress"].(map[string]any)
	if _, ok := ing["annotations"]; ok {
		t.Errorf("annotations must be absent when certManager is off")
	}
	if _, ok := ing["tls"]; ok {
		t.Errorf("tls must be absent when certManager is off")
	}
}

func newWaitFeature(t *testing.T, handler http.HandlerFunc) (Feature, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := scmmclient.New(scmmclient.Config{
		APIBase: srv.URL + "/scm/api/",
	}, srv.Client())
	return Feature{Client: client, PollInterval: 20 * time.Millisecond}, srv
}

func TestWaitForAvailable_Success(t *testing.T) {
	var calls int32
	f, _ := newWaitFeature(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := f.WaitForAvailable(ctx, time.Second); err != nil {
		t.Fatalf("WaitForAvailable: %v", err)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Errorf("expected at least two polls, got %d", atomic.LoadInt32(&calls))
	}
}

func TestWaitForAvailable_Timeout(t *testing.T) {
	f, _ := newWaitFeature(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	start := time.Now()
	err := f.WaitForAvailable(context.Background(), 150*time.Millisecond)
	if err == nil {
		t.Fatalf("WaitForAvailable should time out")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error should mention timeout: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("WaitForAvailable should return promptly after timeout, took %s", time.Since(start))
	}
}

func TestWaitForAvailable_NoClient(t *testing.T) {
	if err := (Feature{}).WaitForAvailable(context.Background(), 10*time.Millisecond); err == nil {
		t.Errorf("WaitForAvailable without a client should error")
	}
}
