// Package config holds the global GOP configuration as plain Go structs.
//
// The Groovy original combines the configuration model with Picocli @Option
// and Jackson annotations on the same fields. Here we keep the model
// annotation-free — the CLI wiring lives in package cli, schema/YAML
// concerns live below.
//
// All field names use Go conventions but their YAML tags reproduce the
// camelCase keys of the original Groovy Config so existing config files
// and config maps stay compatible.
package config

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// Constants pulled from the Groovy Config.
const (
	HelmImage           = "ghcr.io/cloudogu/helm:4.1.4-1"
	K8sVersion          = "1.35.4"
	DefaultAdminUser    = "admin"
	DefaultRegistryPort = 30000
)

// DefaultAdminPW is the per-process admin password every component
// (Application / Jenkins / SCM-Manager / Argo CD) falls back to when
// the user did not pin one explicitly. Generated once at program start,
// matching the Groovy `public static final String DEFAULT_ADMIN_PW =
// generatePassword()` in Config.groovy.
var DefaultAdminPW = generatePassword()

// generatePassword returns a 12-character random password from the
// alphabet the Groovy original uses. Crypto-random.
func generatePassword() string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@$%&"
	out := make([]byte, 12)
	for i := range out {
		n, err := cryptoRandInt(len(alphabet))
		if err != nil {
			// crypto/rand should not fail on a sane OS; falling back to
			// a fixed string is safer than panicking on startup.
			return "admin-fallback"
		}
		out[i] = alphabet[n]
	}
	return string(out)
}

// cryptoRandInt returns a uniformly distributed int in [0, max).
// Indirected through this helper so generatePassword stays small.
func cryptoRandInt(max int) (int, error) {
	n, err := cryptoRand(max)
	return int(n), err
}

// Config is the root configuration object. The structure mirrors
// com.cloudogu.gitops.config.Config.
type Config struct {
	Registry    RegistrySchema    `yaml:"registry"`
	Jenkins     JenkinsSchema     `yaml:"jenkins"`
	MultiTenant MultiTenantSchema `yaml:"multiTenant"`
	Scm         ScmSchema         `yaml:"scm"`
	Application ApplicationSchema `yaml:"application"`
	Features    FeaturesSchema    `yaml:"features"`
	Content     ContentSchema     `yaml:"content"`
}

// New returns a Config populated with the defaults from the Groovy source.
func New() *Config {
	return &Config{
		Registry: RegistrySchema{
			Internal:     true,
			InternalPort: DefaultRegistryPort,
			Helm: HelmConfigWithValues{
				HelmConfig: HelmConfig{
					Chart:   "docker-registry",
					RepoURL: "https://twuni.github.io/docker-registry.helm",
					Version: "3.0.0",
				},
			},
		},
		Jenkins: JenkinsSchema{
			Internal:                    true,
			InternalBashImage:           "bash:5",
			InternalDockerClientVersion: "27.1.2",
			Username:                    DefaultAdminUser,
			Password:                    DefaultAdminPW,
			MetricsUsername:             "metrics",
			MetricsPassword:             "metrics",
			AdditionalEnvs:              map[string]string{},
			Helm: HelmConfigWithValues{
				HelmConfig: HelmConfig{
					Chart:   "jenkins",
					RepoURL: "https://charts.jenkins.io",
					Version: "5.9.18",
				},
			},
		},
		Application: ApplicationSchema{
			Username: DefaultAdminUser,
			Password: DefaultAdminPW,
			GitName:  "Cloudogu",
			GitEmail: "hello@cloudogu.com",
		},
		Features: FeaturesSchema{
			ArgoCD: ArgoCDSchema{
				EmailFrom:    "argocd@example.org",
				EmailToUser:  "app-team@example.org",
				EmailToAdmin: "infra@example.org",
				Namespace:    "argocd",
			},
			Monitoring: MonitoringSchema{
				GrafanaEmailFrom: "grafana@example.org",
				GrafanaEmailTo:   "infra@example.org",
				Helm: MonitoringHelmSchema{
					HelmConfigWithValues: HelmConfigWithValues{
						HelmConfig: HelmConfig{
							Chart:   "kube-prometheus-stack",
							RepoURL: "https://prometheus-community.github.io/helm-charts",
							Version: "80.2.2",
						},
					},
				},
			},
			Secrets: SecretsSchema{
				ExternalSecrets: ESOSchema{
					Helm: ESOHelmSchema{
						HelmConfigWithValues: HelmConfigWithValues{
							HelmConfig: HelmConfig{
								Chart:   "external-secrets",
								RepoURL: "https://charts.external-secrets.io",
								Version: "0.9.16",
							},
						},
					},
				},
				Vault: VaultSchema{
					Helm: VaultHelmSchema{
						HelmConfigWithValues: HelmConfigWithValues{
							HelmConfig: HelmConfig{
								Chart:   "vault",
								RepoURL: "https://helm.releases.hashicorp.com",
								Version: "0.25.0",
							},
						},
					},
				},
			},
			Ingress: IngressSchema{
				IngressNamespace: "ingress",
				Helm: IngressHelmSchema{
					HelmConfigWithValues: HelmConfigWithValues{
						HelmConfig: HelmConfig{
							Chart:   "traefik",
							RepoURL: "https://traefik.github.io/charts",
							Version: "39.0.0",
						},
					},
				},
			},
			CertManager: CertManagerSchema{
				Issuer: "cluster-selfsigned",
				Helm: CertManagerHelmSchema{
					HelmConfigWithValues: HelmConfigWithValues{
						HelmConfig: HelmConfig{
							Chart:   "cert-manager",
							RepoURL: "https://charts.jetstack.io",
							Version: "1.19.4",
						},
					},
				},
			},
		},
		Content: ContentSchema{
			Variables: map[string]any{},
			AllowedStaticsWhitelist: []string{
				"java.lang.String", "java.lang.Integer", "java.lang.Long",
				"java.lang.Double", "java.lang.Float", "java.lang.Boolean",
				"java.lang.Math", "com.cloudogu.gitops.utils.DockerImageParser",
			},
		},
	}
}

// ToYAML serialises the config. When includeInternals is false, only the
// fields that mirror @JsonPropertyDescription in the Groovy code are kept.
// For Phase 1 the simple variant is sufficient – we filter in Phase 2.
func (c *Config) ToYAML(includeInternals bool) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return "", err
	}
	_ = enc.Close()
	return buf.String(), nil
}
