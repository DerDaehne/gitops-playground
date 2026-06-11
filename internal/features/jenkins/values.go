package jenkins

import (
	"github.com/cloudogu/gitops-playground/go/internal/config"
)

// buildValues is the programmatic Go counterpart of
// argocd/cluster-resources/apps/jenkins/values.ftl.yaml.
//
// The Groovy original renders a Freemarker template. We compose the same
// values map here so each branch can be unit-tested without spinning up
// Freemarker. Keep field order stable for diffability against the .ftl.
//
// dockerGid is the GID detected on the worker node (Jenkins.groovy
// findDockerGid). The runner discovers it once before Install and threads
// it through Feature.DockerGid; an empty value falls back to root, matching
// the Groovy `<#if dockerGid?has_content>` branch.
func buildValues(cfg *config.Config, dockerGid string) map[string]any {
	v := map[string]any{
		"dockerClientVersion": cfg.Jenkins.InternalDockerClientVersion,
		"controller":          controllerValues(cfg),
		"persistence":         persistenceValues(),
		"agent":               agentValues(dockerGid),
	}

	// proxy-registry pull-secret block. The .ftl does not have this
	// explicitly — the Helm strategy injects it for every chart — but
	// keeping it next to the controller mirrors what other features do.
	if cfg.Registry.CreateImagePullSecrets {
		if c, ok := v["controller"].(map[string]any); ok {
			c["imagePullSecretName"] = "proxy-registry"
		}
	}

	if cfg.Features.Monitoring.Active {
		v["serviceMonitor"] = map[string]any{
			"enabled":   true,
			"namespace": cfg.Application.NamePrefix + "monitoring",
			"additionalLabels": map[string]any{
				"release": "kube-prometheus-stack",
			},
		}
	}

	return v
}

// controllerValues mirrors the `controller:` block from the .ftl. The
// admin block always references the externally-created jenkins-credentials
// secret; the Groovy Jenkins.enable() creates it imperatively before the
// helm install.
func controllerValues(cfg *config.Config) map[string]any {
	j := cfg.Jenkins

	ctrl := map[string]any{
		"image": map[string]any{
			"registry":   "ghcr.io",
			"repository": "cloudogu/jenkins-helm",
			// The image tag corresponds to the helm chart version because
			// each chart release pins a specific bundled plugin set.
			"tag": j.Helm.Version,
		},
		"installPlugins": false,
		// to prevent the jenkins-ui-test pod being created
		"testEnabled":  false,
		"serviceType":  "NodePort",
		"servicePort":  80,
		"jenkinsUrl":   j.URL,
		"numExecutors": 0,
		// controller and agents must run on the same host. See the comment
		// above agent.workingDir for details.
		"nodeSelector": map[string]any{"node": "jenkins"},
		"runAsUser":    1000,
		"admin": map[string]any{
			"existingSecret": "jenkins-credentials",
		},
		"containerEnv": []any{
			map[string]any{
				"name": "PATH",
				// Workaround for the docker pipeline plugin clearing the
				// environment in docker.inside {}: re-export PATH inside
				// the container.
				"value": "/opt/java/openjdk/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/tmp/docker",
			},
		},
		"customInitContainers": []any{agentDirInitContainer(cfg)},
	}

	if cfg.Application.BaseURL != "" {
		ing := map[string]any{
			"enabled":  true,
			"hostName": j.Ingress,
		}
		if cfg.Features.CertManager.Active {
			ing["annotations"] = map[string]any{
				"cert-manager.io/cluster-issuer": cfg.Features.CertManager.Issuer,
			}
			ing["tls"] = []any{
				map[string]any{
					"secretName": "jenkins-tls",
					"hosts":      []any{j.Ingress},
				},
			}
		}
		ctrl["ingress"] = ing
	}

	return ctrl
}

// agentDirInitContainer mirrors the customInitContainers entry that
// pre-creates the agent working directory on the host and downloads the
// static docker client. The Groovy template assembles the same script.
func agentDirInitContainer(cfg *config.Config) map[string]any {
	return map[string]any{
		"name":            "create-agent-working-dir",
		"securityContext": map[string]any{"runAsUser": 1000},
		"image":           cfg.Jenkins.InternalBashImage,
		"imagePullPolicy": "{{ .Values.controller.imagePullPolicy }}",
		"command":         []any{"/usr/local/bin/bash", "-c"},
		"args": []any{
			// Same heredoc-style script as the .ftl. Kept verbatim so the
			// host-side bootstrap stays identical between Groovy and Go.
			"set -x -o nounset -o pipefail -o errexit;" +
				" id;" +
				" if [[ ! -d /host-tmp/gitops-playground-jenkins-agent ]]; then" +
				" echo creating /tmp/gitops-playground-jenkins-agent on host and chowning to UID 1000;" +
				" mkdir /host-tmp/gitops-playground-jenkins-agent;" +
				" fi;" +
				" if [[ -f /host-tmp/docker/docker ]]; then echo 'Docker already installed'; exit 0; fi;" +
				" cd /host-tmp;" +
				" wget -q https://download.docker.com/linux/static/stable/x86_64/docker-{{.Values.dockerClientVersion}}.tgz -O docker.tgz;" +
				" tar -xzf docker.tgz;" +
				" rm docker.tgz;" +
				" find docker -type f -not -name 'docker' -delete;",
		},
		"volumeMounts": []any{
			map[string]any{"name": "host-tmp", "mountPath": "/host-tmp"},
		},
	}
}

// persistenceValues mirrors the `persistence:` block: only the initContainer
// volume is declared; the helm chart provides the rest.
func persistenceValues() map[string]any {
	return map[string]any{
		"volumes": []any{
			map[string]any{
				"name":     "host-tmp",
				"hostPath": map[string]any{"path": "/tmp"},
			},
		},
	}
}

// agentValues mirrors the `agent:` block. When dockerGid is empty (the
// node lookup failed or did not return a docker group) the agent falls
// back to root + GID 133, same as the .ftl `<#if dockerGid?has_content>`
// branch.
func agentValues(dockerGid string) map[string]any {
	runAsUser := "0"
	runAsGroup := "133"
	if dockerGid != "" {
		runAsUser = "1000"
		runAsGroup = dockerGid
	}

	return map[string]any{
		// See the .ftl comment block: agents bind-mount paths into nested
		// build containers, so the path must exist with the same name on
		// the host. /tmp is the simplest writable target.
		"workingDir":          "/tmp/gitops-playground-jenkins-agent",
		"runAsUser":           runAsUser,
		"runAsGroup":          runAsGroup,
		"nodeSelector":        map[string]any{"node": "jenkins"},
		"containerCap":        2,
		"customJenkinsLabels": []any{"docker"},
		"resources": map[string]any{
			"limits": map[string]any{"cpu": "1", "memory": "4Gi"},
		},
		"envVars": []any{
			map[string]any{
				"name":  "PATH",
				"value": "/opt/java/openjdk/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/tmp/docker",
			},
		},
		"volumes": []any{
			map[string]any{
				"type":      "HostPath",
				"hostPath":  "/tmp/gitops-playground-jenkins-agent",
				"mountPath": "/tmp/gitops-playground-jenkins-agent",
			},
			map[string]any{
				// Persist Jenkins-home (maven caches etc.) on the worker;
				// massive speed-up on repeated builds.
				"type":      "HostPath",
				"hostPath":  "/tmp/gitops-playground-jenkins-agent",
				"mountPath": "/home/jenkins",
			},
			map[string]any{
				// Allow the controller to talk to the host docker daemon.
				"type":      "HostPath",
				"hostPath":  "/var/run/docker.sock",
				"mountPath": "/var/run/docker.sock",
			},
			map[string]any{
				// Static docker binary downloaded once by the controller.
				"type":      "HostPath",
				"hostPath":  "/tmp/docker/",
				"mountPath": "/tmp/docker/",
			},
		},
		// Keep agent pods after the build for debugging. Tidy them up via
		//   kubectl delete pod -l jenkins/jenkins-jenkins-agent=true
		"podRetention": "Always",
	}
}
