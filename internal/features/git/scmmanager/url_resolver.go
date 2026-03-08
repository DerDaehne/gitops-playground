package scmmanager

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/DerDaehne/gitops-playground/internal/config"
	feature "github.com/DerDaehne/gitops-playground/internal/features/git/config"
)

type ScmManagerUrlResolver struct {
	appConfig *config.ApplicationConfig
	scmm      *feature.SCMManagerTenantConfig
}

func NewScmManagerUrlResolver(appConfig *config.ApplicationConfig, scmm *feature.SCMManagerTenantConfig) *ScmManagerUrlResolver {
	return &ScmManagerUrlResolver{
		appConfig: appConfig,
		scmm:      scmm,
	}
}

func (r *ScmManagerUrlResolver) ClientBase() *url.URL {
	return noTrailSlash(ensureScm(r.clientBaseRaw()))
}

func (r *ScmManagerUrlResolver) ClientApiBase() *url.URL {
	return resolveRef(withSlash(r.ClientBase()), "api/")
}

func (r *ScmManagerUrlResolver) ClientRepoBase() *url.URL {
	base := withSlash(r.ClientBase())
	return noTrailSlash(resolveRef(base, r.root()+"/"))
}

func (r *ScmManagerUrlResolver) InClusterBase() *url.URL {
	return noTrailSlash(ensureScm(r.inClusterBaseRaw()))
}

func (r *ScmManagerUrlResolver) InClusterRepoPrefix() string {
	prefix := strings.TrimSpace(r.appConfig.NamePrefix)
	base := withSlash(r.InClusterBase())
	u := withSlash(resolveRef(base, r.root()))
	return u.String() + prefix
}

func (r *ScmManagerUrlResolver) InClusterRepoUrl(repoTarget string) string {
	repo := strings.TrimSpace(repoTarget)
	base := withSlash(r.InClusterBase())
	u := resolveRef(base, r.root()+"/"+repo+"/")
	return noTrailSlash(u).String()
}

func (r *ScmManagerUrlResolver) ClientRepoUrl(repoTarget string) string {
	repo := strings.TrimSpace(repoTarget)
	base := withSlash(r.ClientRepoBase())
	u := resolveRef(base, repo+"/")
	return noTrailSlash(u).String()
}

func (r *ScmManagerUrlResolver) PrometheusEndpoint() *url.URL {
	return resolveRef(withSlash(r.ClientBase()), "api/v2/metrics/prometheus")
}

func (r *ScmManagerUrlResolver) clientBaseRaw() *url.URL {
	if r.scmm.Internal {
		return r.serviceDnsBase()
	}
	return r.externalBase()
}

func (r *ScmManagerUrlResolver) inClusterBaseRaw() *url.URL {
	if r.scmm.Internal {
		return r.serviceDnsBase()
	}
	return r.externalBase()
}

func (r *ScmManagerUrlResolver) serviceDnsBase() *url.URL {
	namespace := strings.TrimSpace(r.scmm.Namespace)
	if namespace == "" {
		namespace = "scm-manager"
	}
	u, _ := url.Parse(fmt.Sprintf("http://scmm.%s.svc.cluster.local", namespace))
	return u
}

func (r *ScmManagerUrlResolver) externalBase() *url.URL {
	urlStr := strings.TrimSpace(r.scmm.Url)
	if urlStr != "" {
		u, _ := url.Parse(urlStr)
		return u
	}
	ingress := strings.TrimSpace(r.scmm.Ingress)
	if ingress != "" {
		u, _ := url.Parse("http://" + ingress)
		return u
	}
	panic("either scmm.url or scmm.ingress must be set when internal=false")
}

func (r *ScmManagerUrlResolver) root() string {
	root := strings.TrimSpace(r.scmm.RootPath)
	if root == "" {
		return "repo"
	}
	return root
}

func ensureScm(u *url.URL) *url.URL {
	s := withSlash(u)
	if strings.HasSuffix(s.Path, "/scm/") {
		return s
	}
	return resolveRef(s, "scm/")
}

func withSlash(u *url.URL) *url.URL {
	s := u.String()
	if strings.HasSuffix(s, "/") {
		return u
	}
	parsed, _ := url.Parse(s + "/")
	return parsed
}

func noTrailSlash(u *url.URL) *url.URL {
	s := u.String()
	if strings.HasSuffix(s, "/") {
		parsed, _ := url.Parse(strings.TrimRight(s, "/"))
		return parsed
	}
	return u
}

func resolveRef(base *url.URL, ref string) *url.URL {
	refURL, _ := url.Parse(ref)
	return base.ResolveReference(refURL)
}
