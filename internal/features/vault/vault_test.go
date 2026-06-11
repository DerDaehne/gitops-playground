package vault

import (
	"testing"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

// fixedToken makes the dev-mode root token deterministic in tests.
func fixedToken() string { return "test-root-token" }

func TestIsEnabled(t *testing.T) {
	cfg := config.New()
	if (Feature{}).IsEnabled(cfg) {
		t.Errorf("disabled by default when Mode is empty")
	}
	cfg.Features.Secrets.Vault.Mode = ModeDev
	if !(Feature{}).IsEnabled(cfg) {
		t.Errorf("enabled when Mode=dev")
	}
	cfg.Features.Secrets.Vault.Mode = ModeProd
	if !(Feature{}).IsEnabled(cfg) {
		t.Errorf("enabled when Mode=prod")
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
	if got, want := (Feature{}).Order(), 110; got != want {
		t.Errorf("Order=%d want %d", got, want)
	}
}

func TestBuildValuesProdMinimal(t *testing.T) {
	cfg := config.New()
	cfg.Features.Secrets.Vault.Mode = ModeProd
	v := (Feature{}).buildValues(cfg)

	ui, ok := v["ui"].(map[string]any)
	if !ok {
		t.Fatalf("ui missing")
	}
	if ui["enabled"] != true || ui["externalPort"] != 80 || ui["serviceType"] != "ClusterIP" {
		t.Errorf("ui block wrong: %v", ui)
	}
	if _, ok := v["server"]; ok {
		t.Errorf("server must be absent for prod mode with no image/host/podResources")
	}
	if _, ok := v["global"]; ok {
		t.Errorf("global pull secret must be absent")
	}
}

func TestBuildValuesDevAddsDevBlock(t *testing.T) {
	cfg := config.New()
	cfg.Features.Secrets.Vault.Mode = ModeDev
	cfg.Application.Username = "alice"
	cfg.Application.Password = "s3cret"
	cfg.Features.ArgoCD.Active = true

	f := Feature{RootTokenFn: fixedToken}
	v := f.buildValues(cfg)

	server, ok := v["server"].(map[string]any)
	if !ok {
		t.Fatalf("server missing in dev mode")
	}

	dev, ok := server["dev"].(map[string]any)
	if !ok {
		t.Fatalf("server.dev missing")
	}
	if dev["enabled"] != true {
		t.Errorf("server.dev.enabled must be true")
	}
	if dev["devRootToken"] != "test-root-token" {
		t.Errorf("devRootToken=%v want test-root-token", dev["devRootToken"])
	}

	vols, ok := server["volumes"].([]any)
	if !ok || len(vols) != 1 {
		t.Fatalf("volumes missing or wrong shape: %v", server["volumes"])
	}
	vol := vols[0].(map[string]any)
	if vol["name"] != postStartVolume {
		t.Errorf("volume name=%v want %v", vol["name"], postStartVolume)
	}
	cm := vol["configMap"].(map[string]any)
	if cm["name"] != postStartConfigMap {
		t.Errorf("configMap name=%v want %v", cm["name"], postStartConfigMap)
	}

	postStart, ok := server["postStart"].([]any)
	if !ok || len(postStart) != 3 {
		t.Fatalf("postStart missing/wrong: %v", server["postStart"])
	}
	cmd := postStart[2].(string)
	wantSub := "USERNAME=alice PASSWORD=s3cret ARGOCD=true /var/opt/scripts/dev-post-start.sh"
	if !contains(cmd, wantSub) {
		t.Errorf("postStart cmd missing %q, got %q", wantSub, cmd)
	}
}

func TestBuildValuesProdHasNoDevBlock(t *testing.T) {
	cfg := config.New()
	cfg.Features.Secrets.Vault.Mode = ModeProd
	cfg.Application.PodResources = true // force server block to be emitted

	v := (Feature{}).buildValues(cfg)

	server, ok := v["server"].(map[string]any)
	if !ok {
		t.Fatalf("server missing")
	}
	if _, ok := server["dev"]; ok {
		t.Errorf("server.dev must be absent in prod mode")
	}
	if _, ok := server["volumes"]; ok {
		t.Errorf("volumes must be absent in prod mode")
	}
	if _, ok := server["postStart"]; ok {
		t.Errorf("postStart must be absent in prod mode")
	}
	if _, ok := server["resources"]; !ok {
		t.Errorf("server.resources must be present when podResources=true")
	}
}

func TestBuildValuesDevArgocdFalse(t *testing.T) {
	cfg := config.New()
	cfg.Features.Secrets.Vault.Mode = ModeDev
	cfg.Features.ArgoCD.Active = false

	f := Feature{RootTokenFn: fixedToken}
	v := f.buildValues(cfg)
	server := v["server"].(map[string]any)
	cmd := server["postStart"].([]any)[2].(string)
	if !contains(cmd, "ARGOCD=false") {
		t.Errorf("expected ARGOCD=false, got %q", cmd)
	}
}

func TestBuildValuesPullSecretAndImage(t *testing.T) {
	cfg := config.New()
	cfg.Features.Secrets.Vault.Mode = ModeProd
	cfg.Registry.CreateImagePullSecrets = true
	cfg.Features.Secrets.Vault.Helm.Image = "registry.example.com/hashicorp/vault:1.15.0"

	v := (Feature{}).buildValues(cfg)

	global, ok := v["global"].(map[string]any)
	if !ok {
		t.Fatalf("global missing")
	}
	ips := global["imagePullSecrets"].([]any)
	if ips[0].(map[string]any)["name"] != "proxy-registry" {
		t.Errorf("pull secret name wrong: %v", ips)
	}

	server := v["server"].(map[string]any)
	img := server["image"].(map[string]any)
	if img["repository"] != "registry.example.com/hashicorp/vault" {
		t.Errorf("image repo=%v", img["repository"])
	}
	if img["tag"] != "1.15.0" {
		t.Errorf("image tag=%v", img["tag"])
	}
}

func TestBuildValuesIngressWithoutCertManager(t *testing.T) {
	cfg := config.New()
	cfg.Features.Secrets.Vault.Mode = ModeProd
	cfg.Features.Secrets.Vault.URL = "https://vault.example.com/"

	v := (Feature{}).buildValues(cfg)

	server := v["server"].(map[string]any)
	ing, ok := server["ingress"].(map[string]any)
	if !ok {
		t.Fatalf("ingress missing")
	}
	if ing["enabled"] != true {
		t.Errorf("ingress.enabled must be true")
	}
	hosts := ing["hosts"].([]any)
	if hosts[0].(map[string]any)["host"] != "vault.example.com" {
		t.Errorf("host=%v", hosts[0])
	}
	if _, ok := ing["tls"]; ok {
		t.Errorf("tls must be absent without certManager")
	}
}

func TestBuildValuesIngressWithCertManager(t *testing.T) {
	cfg := config.New()
	cfg.Features.Secrets.Vault.Mode = ModeProd
	cfg.Features.Secrets.Vault.URL = "https://vault.example.com"
	cfg.Features.CertManager.Active = true
	cfg.Features.CertManager.Issuer = "letsencrypt-prod"

	v := (Feature{}).buildValues(cfg)
	server := v["server"].(map[string]any)
	ing := server["ingress"].(map[string]any)
	ann := ing["annotations"].(map[string]any)
	if ann["cert-manager.io/cluster-issuer"] != "letsencrypt-prod" {
		t.Errorf("issuer annotation wrong: %v", ann)
	}
	tls := ing["tls"].([]any)
	tlsBlock := tls[0].(map[string]any)
	if tlsBlock["secretName"] != "vault-tls" {
		t.Errorf("tls secretName=%v", tlsBlock["secretName"])
	}
	if tlsBlock["hosts"].([]any)[0] != "vault.example.com" {
		t.Errorf("tls hosts wrong: %v", tlsBlock["hosts"])
	}
}

func TestHostFromURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"https://vault.example.com/path", "vault.example.com"},
		{"http://vault.local:8200", "vault.local"},
		{"not a url", ""}, // url.Parse accepts most strings; Hostname will be ""
	}
	for _, c := range cases {
		if got := hostFromURL(c.in); got != c.want {
			t.Errorf("hostFromURL(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestParseImage(t *testing.T) {
	cases := []struct{ in, repo, tag string }{
		{"vault:1.15", "vault", "1.15"},
		{"vault", "vault", "latest"},
		{"localhost:5000/vault:dev", "localhost:5000/vault", "dev"},
	}
	for _, c := range cases {
		repo, tag := parseImage(c.in)
		if repo != c.repo || tag != c.tag {
			t.Errorf("parseImage(%q)=(%q,%q) want (%q,%q)", c.in, repo, tag, c.repo, c.tag)
		}
	}
}

func TestBoolToFreemarkerC(t *testing.T) {
	if boolToFreemarkerC(true) != "true" || boolToFreemarkerC(false) != "false" {
		t.Errorf("boolToFreemarkerC wrong")
	}
}

func TestRootTokenDefaultIsHex(t *testing.T) {
	tok := (Feature{}).rootToken()
	if len(tok) != 32 { // 16 random bytes hex-encoded
		t.Errorf("default root token unexpected length %d: %q", len(tok), tok)
	}
}

// contains is a tiny strings.Contains so the test file stays
// import-light.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
