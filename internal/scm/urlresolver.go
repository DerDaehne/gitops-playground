package scm

import (
	"fmt"
	"net/url"
	"strings"
)

// URLBuilder constructs the various URLs that gitops-playground needs for
// an SCM-Manager installation. It consolidates ScmManagerUrlResolver.groovy
// into a single, side-effect-free helper: callers feed it a few base URLs
// they have already resolved (Service-DNS, NodePort or external ingress)
// and ask for the rendered path.
//
// The Groovy original mixed URI parsing with manual string concatenation
// (see ensureScm/withSlash/noTrailSlash), which is why we centralise the
// joining logic here instead of duplicating it across the provider
// implementations.
type URLBuilder struct {
	// ClientBase is the base URL used by external/CI clients, e.g.
	// "http://scmm.example.com/scm".
	ClientBase string
	// InClusterBase is the base URL used by workloads inside Kubernetes,
	// usually the Service DNS. May equal ClientBase for non-internal
	// installations.
	InClusterBase string
	// NamePrefix is the optional tenant prefix (e.g. "tenant-").
	NamePrefix string
}

// ClientBaseURL returns the client-facing base, normalised to drop any
// trailing slash and to make sure it ends in "/scm".
func (b URLBuilder) ClientBaseURL() string {
	return ensureScmSuffix(noTrailingSlash(b.ClientBase))
}

// InClusterBaseURL is the in-cluster equivalent of ClientBaseURL.
func (b URLBuilder) InClusterBaseURL() string {
	return ensureScmSuffix(noTrailingSlash(b.InClusterBase))
}

// ClientAPIBase returns "<clientBase>/api/" – the prefix every Retrofit
// call used in the Groovy code starts with.
func (b URLBuilder) ClientAPIBase() string {
	return strings.TrimRight(b.ClientBaseURL(), "/") + "/api/"
}

// ClientRepoBase returns "<clientBase>/repo" (no trailing slash).
func (b URLBuilder) ClientRepoBase() string {
	return strings.TrimRight(b.ClientBaseURL(), "/") + "/repo"
}

// InClusterRepoBase is the in-cluster equivalent of ClientRepoBase.
func (b URLBuilder) InClusterRepoBase() string {
	return strings.TrimRight(b.InClusterBaseURL(), "/") + "/repo"
}

// InClusterRepoPrefix returns "<inClusterBase>/repo/<namePrefix>".
func (b URLBuilder) InClusterRepoPrefix() string {
	return b.InClusterRepoBase() + "/" + strings.TrimSpace(b.NamePrefix)
}

// InClusterRepoURL returns "<inClusterBase>/repo/<namespace>/<name>".
func (b URLBuilder) InClusterRepoURL(namespace, name string) string {
	return b.InClusterRepoBase() + "/" + joinPath(namespace, name)
}

// ClientRepoURL returns "<clientBase>/repo/<namespace>/<name>".
func (b URLBuilder) ClientRepoURL(namespace, name string) string {
	return b.ClientRepoBase() + "/" + joinPath(namespace, name)
}

// PrometheusEndpoint returns the path SCM-Manager exposes the Prometheus
// metrics on – always relative to the *client* base because Prometheus is
// usually run outside the cluster.
func (b URLBuilder) PrometheusEndpoint() string {
	return strings.TrimRight(b.ClientBaseURL(), "/") + "/api/v2/metrics/prometheus"
}

// ParseHost extracts the host from a URL string (without port).
// Returns the empty string when the input does not parse.
func ParseHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// ParseScheme extracts the scheme from a URL string.
func ParseScheme(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme
}

// GitLabRepoURL builds "<base>/<parentFullPath>/<namespace>/<name>.git".
// Both arguments are joined into the URL exactly the way Gitlab.groovy
// does it.
func GitLabRepoURL(base, parentFullPath, namespace, name string) string {
	parts := []string{
		strings.TrimRight(strings.TrimSpace(base), "/"),
		strings.Trim(strings.TrimSpace(parentFullPath), "/"),
		strings.Trim(strings.TrimSpace(namespace), "/"),
		strings.Trim(strings.TrimSpace(name), "/") + ".git",
	}
	// Drop empty pieces so the join stays clean when no parent group is
	// configured (which we treat as a misconfiguration but try not to
	// crash on while building URLs).
	cleaned := parts[:0]
	for _, p := range parts {
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return strings.Join(cleaned, "/")
}

// GitLabRepoPrefix mirrors Gitlab.repoPrefix() – useful for log lines and
// for consumers that need to derive ArgoCD project paths.
func GitLabRepoPrefix(base, parentFullPath, namePrefix string) string {
	return fmt.Sprintf("%s/%s/%s",
		strings.TrimRight(strings.TrimSpace(base), "/"),
		strings.Trim(strings.TrimSpace(parentFullPath), "/"),
		strings.TrimSpace(namePrefix))
}

// joinPath joins two URL segments without producing empty pieces.
func joinPath(ns, name string) string {
	ns = strings.Trim(strings.TrimSpace(ns), "/")
	name = strings.Trim(strings.TrimSpace(name), "/")
	switch {
	case ns == "" && name == "":
		return ""
	case ns == "":
		return name
	case name == "":
		return ns
	default:
		return ns + "/" + name
	}
}

// noTrailingSlash returns s without the trailing slash, if any.
func noTrailingSlash(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}

// ensureScmSuffix appends "/scm" if the URL doesn't already end in it.
// The Groovy code does the same via URI.resolve("scm/"). We avoid the
// resolve dance because url.URL.ResolveReference behaves differently
// from java.net.URI when the base lacks a trailing slash.
func ensureScmSuffix(s string) string {
	if s == "" {
		return s
	}
	if strings.HasSuffix(s, "/scm") {
		return s
	}
	// Only add "/scm" if the URL's *path* does not already end in /scm.
	u, err := url.Parse(s)
	if err == nil && strings.HasSuffix(u.Path, "/scm") {
		return s
	}
	return s + "/scm"
}
