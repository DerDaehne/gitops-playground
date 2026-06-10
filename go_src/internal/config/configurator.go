package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

// Initialise applies the same derivations the Groovy ApplicationConfigurator
// does: derived flags, image-pull validation, internal vs. external Jenkins
// and SCM-Manager URLs, base-URL fan-out, name-prefix normalisation and
// multi-tenant guards.
//
// Returns the same pointer it was given so callers can chain. Returns an
// error instead of panicking; the CLI converts that into a non-zero exit.
func Initialise(cfg *Config) error {
	addAdditionalApplicationConfig(cfg)
	addNamePrefix(cfg)
	if err := addScmConfig(cfg); err != nil {
		return err
	}
	if err := addRegistryConfig(cfg); err != nil {
		return err
	}
	addJenkinsConfig(cfg)
	addFeatureConfig(cfg)
	if err := evaluateBaseURL(cfg); err != nil {
		return err
	}
	if err := setResourceInclusionsCluster(cfg); err != nil {
		return err
	}
	if err := setMultiTenantModeConfig(cfg); err != nil {
		return err
	}
	return nil
}

func addAdditionalApplicationConfig(cfg *Config) {
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		cfg.Application.RunningInsideK8s = true
	}
}

func addNamePrefix(cfg *Config) {
	p := cfg.Application.NamePrefix
	if p == "" {
		return
	}
	if !strings.HasSuffix(p, "-") {
		p += "-"
		cfg.Application.NamePrefix = p
	}
	cfg.Application.NamePrefixForEnvVars = strings.ToUpper(strings.ReplaceAll(p, "-", "_"))
}

func addFeatureConfig(cfg *Config) {
	if cfg.Features.Secrets.Vault.Mode != "" {
		cfg.Features.Secrets.Active = true
	}
	if cfg.Features.Mail.SMTPAddress != "" {
		cfg.Features.Mail.Active = true
	}
	if cfg.Features.Ingress.Active && cfg.Application.BaseURL == "" {
		slog.Warn("Ingress-controller is activated without baseUrl parameter. Services will not be accessible by hostnames.")
	}
}

func addRegistryConfig(cfg *Config) error {
	r := &cfg.Registry

	// Image pull secrets gate – may fire even when no registry is set.
	if r.CreateImagePullSecrets {
		username := r.ReadOnlyUsername
		if username == "" {
			username = r.Username
		}
		password := r.ReadOnlyPassword
		if password == "" {
			password = r.Password
		}
		if username == "" || password == "" {
			return errors.New("createImagePullSecrets needs to be used with either registry username and password or the readOnly variants")
		}
	}

	switch {
	case r.URL != "":
		r.Internal = false
		r.Active = true
	case r.Active:
		r.Internal = true
		r.URL = fmt.Sprintf("localhost:%d", r.InternalPort)
	default:
		return nil
	}

	if r.ProxyURL != "" {
		r.TwoRegistries = true
		if r.ProxyUsername == "" || r.ProxyPassword == "" {
			return errors.New("proxy URL needs to be used with proxy-username and proxy-password")
		}
	}
	return nil
}

// addScmConfig mirrors the SCM section of ApplicationConfigurator. Until
// the typed SCM schema lands in Phase 3 we operate on the opaque Raw map
// so that the CLI does not break when a config sets `scm.scmManager.url`.
//
// For Phase 2 this is purposely conservative: the only thing the Groovy
// version does at init time is derive `urlForJenkins`, `ingress`, the
// `internal` flag and default the admin user/password. The deeper SCM
// modelling is part of the Phase 3 SCM adapter SPEC.
func addScmConfig(cfg *Config) error {
	if cfg.Scm.Raw == nil {
		cfg.Scm.Raw = map[string]any{}
	}
	scmm := getOrMakeMap(cfg.Scm.Raw, "scmManager")

	if u, _ := scmm["url"].(string); u != "" {
		scmm["internal"] = false
		scmm["urlForJenkins"] = u
	} else {
		scmm["urlForJenkins"] = fmt.Sprintf(
			"http://scmm.%sscm-manager.svc.cluster.local/scm",
			cfg.Application.NamePrefix,
		)
	}

	if cfg.Application.BaseURL != "" {
		host, err := injectSubdomainHost("scmm", cfg.Application.BaseURL, cfg.Application.URLSeparatorHyphen)
		if err != nil {
			return fmt.Errorf("invalid baseUrl %q: %w", cfg.Application.BaseURL, err)
		}
		scmm["ingress"] = host
	}

	// Default user/password fall-through. The Groovy code uses a generated
	// admin password sentinel; in Go we treat empty as the trigger to fall
	// back to the application credentials.
	if v, _ := scmm["username"].(string); v == "" {
		scmm["username"] = cfg.Application.Username
	}
	if v, _ := scmm["password"].(string); v == "" {
		scmm["password"] = cfg.Application.Password
	}
	return nil
}

func addJenkinsConfig(cfg *Config) {
	j := &cfg.Jenkins

	switch {
	case j.URL != "":
		j.Active = true
		j.Internal = false
		j.URLForScm = j.URL
	case j.Active:
		j.URLForScm = fmt.Sprintf("http://jenkins.%sjenkins.svc.cluster.local",
			cfg.Application.NamePrefix)
	default:
		return
	}

	if cfg.Application.BaseURL != "" {
		host, err := injectSubdomainHost("jenkins", cfg.Application.BaseURL, cfg.Application.URLSeparatorHyphen)
		if err == nil {
			j.Ingress = host
		}
	}
	if j.Username == "" {
		j.Username = cfg.Application.Username
	}
	if j.Password == "" {
		j.Password = cfg.Application.Password
	}
}

func evaluateBaseURL(cfg *Config) error {
	base := cfg.Application.BaseURL
	if base == "" {
		return nil
	}
	hyphen := cfg.Application.URLSeparatorHyphen
	if cfg.Features.ArgoCD.Active && cfg.Features.ArgoCD.URL == "" {
		u, err := injectSubdomain("argocd", base, hyphen)
		if err != nil {
			return err
		}
		cfg.Features.ArgoCD.URL = u
	}
	if cfg.Features.Monitoring.Active && cfg.Features.Monitoring.GrafanaURL == "" {
		u, err := injectSubdomain("grafana", base, hyphen)
		if err != nil {
			return err
		}
		cfg.Features.Monitoring.GrafanaURL = u
	}
	if cfg.Features.Secrets.Active && cfg.Features.Secrets.Vault.URL == "" {
		u, err := injectSubdomain("vault", base, hyphen)
		if err != nil {
			return err
		}
		cfg.Features.Secrets.Vault.URL = u
	}
	return nil
}

// setMultiTenantModeConfig mirrors the Groovy side guards. We use the Raw
// map until the typed MultiTenant schema is fleshed out in Phase 3.
func setMultiTenantModeConfig(cfg *Config) error {
	mt := cfg.MultiTenant.Raw
	if mt == nil {
		return nil
	}
	dedicated, _ := mt["useDedicatedInstance"].(bool)
	if !dedicated {
		return nil
	}
	if cfg.Application.NamePrefix == "" {
		return errors.New("to enable Central Multi-Tenant mode, you must define a name prefix to distinguish between instances")
	}
	if !cfg.Features.ArgoCD.Operator {
		cfg.Features.ArgoCD.Operator = true
	}
	scmm := getOrMakeMap(mt, "scmManager")
	if u, ok := scmm["url"].(string); ok && u != "" {
		scmm["url"] = strings.TrimRight(u, "/")
	}
	cfg.Features.Ingress.Active = false
	return nil
}

func setResourceInclusionsCluster(cfg *Config) error {
	if !cfg.Features.ArgoCD.Operator {
		return nil
	}
	if u := cfg.Features.ArgoCD.ResourceInclusionsCluster; u != "" {
		if _, err := url.Parse(u); err != nil {
			return fmt.Errorf("invalid URL for 'features.argocd.resourceInclusionsCluster': %s: %w", u, err)
		}
		return nil
	}
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return errors.New("could not determine 'features.argocd.resourceInclusionsCluster' which is required when argocd.operator=true. " +
			"Set KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT or set the option explicitly")
	}
	cfg.Features.ArgoCD.ResourceInclusionsCluster = fmt.Sprintf("https://%s:%s", host, port)
	return nil
}

// injectSubdomain returns baseUrl with `subdomain` prepended to its host,
// either with a dot or a hyphen separator.
//
//	injectSubdomain("argocd", "http://localhost:8080", false) → "http://argocd.localhost:8080"
//	injectSubdomain("argocd", "http://localhost",      true)  → "http://argocd-localhost"
func injectSubdomain(subdomain, baseURL string, hyphen bool) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid url %q: %w", baseURL, err)
	}
	sep := "."
	if hyphen {
		sep = "-"
	}
	host := u.Hostname()
	out := u.Scheme + "://" + subdomain + sep + host
	if p := u.Port(); p != "" {
		out += ":" + p
	}
	out += u.Path
	return out, nil
}

func injectSubdomainHost(subdomain, baseURL string, hyphen bool) (string, error) {
	full, err := injectSubdomain(subdomain, baseURL, hyphen)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(full)
	if err != nil {
		return "", err
	}
	return u.Host, nil
}

func getOrMakeMap(parent map[string]any, key string) map[string]any {
	if v, ok := parent[key].(map[string]any); ok {
		return v
	}
	m := map[string]any{}
	parent[key] = m
	return m
}
