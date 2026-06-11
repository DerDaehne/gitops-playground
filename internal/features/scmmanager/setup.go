// setup.go contains the post-deploy configuration logic: wait for the
// SCM-Manager pod to answer the API, then install plugins, push the
// /v2/config block, optionally configure the Jenkins plugin and add the
// gitops + metrics users. This is the Go counterpart of
// com.cloudogu.gitops.tools.core.ScmManagerSetup (configure, installScmmPlugins,
// setSetupConfigs, configureJenkinsPlugin, addDefaultUsers).
//
// Mapping (Groovy → Go):
//
//	ScmManagerSetup.configure              → Feature.Configure
//	ScmManagerSetup.installScmmPlugins     → Feature.installPlugins
//	ScmManagerSetup.setSetupConfigs        → Feature.applySetupConfig
//	ScmManagerSetup.configureJenkinsPlugin → Feature.configureJenkinsPlugin
//	ScmManagerSetup.addDefaultUsers        → Feature.addDefaultUsers
//	ScmManagerSetup.addUser                → Client.EnsureUser
//	ScmManagerSetup.grantUserPermissions   → Client.SetUserPermissions
//	ScmManagerSetup.waitForScmmAvailable   → Feature.WaitForAvailable
//
// All loops respect ctx.Done(); the Groovy original busy-waited via
// Thread.sleep with no cancellation surface.

package scmmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudogu/gitops-playground/go/internal/config"
)

// defaultPlugins is the canonical plugin list from
// ScmManagerSetup.installScmmPlugins(). Order matters: the last entry is
// the one we trigger the SCM-Manager restart on (when skipRestart=false),
// so the slice must stay stable across edits.
var defaultPlugins = []string{
	"scm-mail-plugin",
	"scm-review-plugin",
	"scm-code-editor-plugin",
	"scm-editor-plugin",
	"scm-landingpage-plugin",
	"scm-el-plugin",
	"scm-readme-plugin",
	"scm-webhook-plugin",
	"scm-ci-plugin",
	"scm-metrics-prometheus-plugin",
}

// jenkinsPlugin is appended to the plugin list when Jenkins is active. It
// has to come last so SCMM restarts pick it up.
const jenkinsPlugin = "scm-jenkins-plugin"

// WaitForAvailable polls the SCM-Manager API until either a 2xx response
// is observed or the timeout elapses. ctx cancellation propagates through
// the underlying client; both paths return non-nil errors so callers can
// distinguish "not ready" from "user aborted".
//
// timeout == 0 falls back to defaultWaitTimeout (180s, matching Groovy).
func (f Feature) WaitForAvailable(ctx context.Context, timeout time.Duration) error {
	if f.Client == nil {
		return fmt.Errorf("scm-manager: API client not configured")
	}
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	interval := f.PollInterval
	if interval <= 0 {
		interval = defaultWaitInterval
	}
	return f.Client.WaitUntilAvailable(ctx, timeout, interval)
}

// Configure mirrors ScmManagerSetup.configure() in execution order:
//
//  1. installPlugins        – POSTs each plugin name to
//     /v2/plugins/available/{name}/install, restarting on the last entry
//     when skipRestart is false. If the install triggered a restart we
//     re-await /v2 via WaitForAvailable.
//  2. applySetupConfig      – PUTs the /v2/config block (proxies, baseUrl,
//     namespace strategy, …).
//  3. configureJenkinsPlugin – PUTs /v2/config/jenkins/ when Jenkins is
//     active; skipped otherwise.
//  4. addDefaultUsers       – creates the gitops user (full access via
//     repository permissions later) and the metrics user (read-only
//     metrics permission).
//
// Each step bails on the first error – we deliberately do not retry here
// because the API client already maps 409 to "already configured" success.
func (f Feature) Configure(ctx context.Context, cfg *config.Config) error {
	if f.Client == nil {
		return fmt.Errorf("scm-manager: API client not configured")
	}

	if err := f.installPlugins(ctx, cfg); err != nil {
		return err
	}
	if err := f.applySetupConfig(ctx, cfg); err != nil {
		return err
	}
	if f.JenkinsActive != nil && f.JenkinsActive(cfg) {
		if err := f.configureJenkinsPlugin(ctx, cfg); err != nil {
			return err
		}
	}
	if err := f.addDefaultUsers(ctx, cfg); err != nil {
		return err
	}
	return nil
}

// installPlugins drives Client.InstallPlugin once per plugin name. The
// last call is the one that asks SCM-Manager to restart, matching the
// Groovy `restartForThisPlugin = pluginName == pluginNames.last()`
// pattern. When a restart was triggered we wait until the API is back.
func (f Feature) installPlugins(ctx context.Context, cfg *config.Config) error {
	if scmmBool(cfg, "skipPlugins") {
		return nil
	}

	plugins := append([]string(nil), defaultPlugins...)
	if f.JenkinsActive != nil && f.JenkinsActive(cfg) {
		plugins = append(plugins, jenkinsPlugin)
	}

	skipRestart := scmmBool(cfg, "skipRestart")
	var restartTriggered bool

	for i, name := range plugins {
		// ctx.Done() guard – the underlying HTTP call also honors ctx,
		// but checking here gives us a clean exit on user cancellation
		// even between rapid plugin installs.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		restart := !skipRestart && i == len(plugins)-1
		if err := f.Client.InstallPlugin(ctx, name, restart); err != nil {
			return fmt.Errorf("install plugin %s: %w", name, err)
		}
		if restart {
			restartTriggered = true
		}
	}

	if restartTriggered {
		// Use a short interval here: the Groovy code passes 2 s.
		if err := f.Client.WaitUntilAvailable(ctx, defaultWaitTimeout, 2*time.Second); err != nil {
			return fmt.Errorf("wait for SCMM after plugin restart: %w", err)
		}
	}
	return nil
}

// applySetupConfig mirrors ScmManagerSetup.setSetupConfigs. The map is
// kept literal so the JSON the upstream API consumes is identical to the
// Groovy payload byte-for-byte (modulo Jackson key ordering, which the
// SCM-Manager API does not depend on).
func (f Feature) applySetupConfig(ctx context.Context, cfg *config.Config) error {
	baseURL := scmmString(cfg, "url")
	if baseURL == "" {
		// Internal mode: addScmConfig fills in scm.scmManager.url for
		// external SCMM only. For the internal case the Groovy code sets
		// the base URL to the in-cluster service URL via ScmManager.url.
		// We mirror that fallback here.
		baseURL = fmt.Sprintf("http://scmm.%sscm-manager.svc.cluster.local/scm",
			cfg.Application.NamePrefix)
	}

	body := map[string]any{
		"enableProxy":              false,
		"proxyPort":                8080,
		"proxyServer":              "proxy.mydomain.com",
		"proxyUser":                nil,
		"proxyPassword":            nil,
		"realmDescription":         "SONIA :: SCM Manager",
		"disableGroupingGrid":      false,
		"dateFormat":               "YYYY-MM-DD HH:mm:ss",
		"anonymousAccessEnabled":   false,
		"anonymousMode":            "OFF",
		"baseUrl":                  baseURL,
		"forceBaseUrl":             false,
		"loginAttemptLimit":        -1,
		"proxyExcludes":            []any{},
		"skipFailedAuthenticators": false,
		"pluginUrl":                "https://plugin-center-api.scm-manager.org/api/v1/plugins/{version}?os={os}&arch={arch}",
		"loginAttemptLimitTimeout": 300,
		"enabledXsrfProtection":    true,
		"namespaceStrategy":        "CustomNamespaceStrategy",
		"loginInfoUrl":             "https://login-info.scm-manager.org/api/v1/login-info",
		"releaseFeedUrl":           "https://scm-manager.org/download/rss.xml",
		"mailDomainName":           "scm-manager.local",
		"adminGroups":              []any{},
		"adminUsers":               []any{},
	}
	if err := f.Client.SetConfig(ctx, body); err != nil {
		return fmt.Errorf("set scmm config: %w", err)
	}
	return nil
}

// configureJenkinsPlugin pushes the per-plugin Jenkins configuration. The
// URL comes from cfg.Jenkins.URLForScm – the same field the Groovy code
// reads as `scmManager.config.jenkins.urlForScm`.
func (f Feature) configureJenkinsPlugin(ctx context.Context, cfg *config.Config) error {
	body := map[string]any{
		"disableRepositoryConfiguration": false,
		"disableMercurialTrigger":        false,
		"disableGitTrigger":              false,
		"disableEventTrigger":            false,
		"url":                            cfg.Jenkins.URLForScm,
	}
	if err := f.Client.ConfigureJenkinsPlugin(ctx, body); err != nil {
		return fmt.Errorf("configure jenkins plugin: %w", err)
	}
	return nil
}

// addDefaultUsers creates the gitops user and the metrics user, then grants
// the metrics user the "metrics:read" permission. Both passwords come from
// the scm.scmManager.password field – the Groovy code reuses the admin
// password for both technical accounts and we preserve that behaviour to
// keep configurator outputs interchangeable.
func (f Feature) addDefaultUsers(ctx context.Context, cfg *config.Config) error {
	password := scmmString(cfg, "password")
	gitOpsUser := f.Client.GitOpsUsername()
	if gitOpsUser == "" {
		gitOpsUser = scmmString(cfg, "gitOpsUsername")
	}
	if gitOpsUser != "" {
		if err := f.Client.EnsureUser(ctx, gitOpsUser, password, "", ""); err != nil {
			return fmt.Errorf("ensure gitops user: %w", err)
		}
	}

	metricsUser := cfg.Application.NamePrefix + "metrics"
	if err := f.Client.EnsureUser(ctx, metricsUser, password, "", ""); err != nil {
		return fmt.Errorf("ensure metrics user: %w", err)
	}
	if err := f.Client.SetUserPermissions(ctx, metricsUser, []string{"metrics:read"}); err != nil {
		return fmt.Errorf("grant metrics permission: %w", err)
	}
	return nil
}

// scmmBool reads cfg.Scm.Raw["scmManager"][key] as a bool. Missing keys
// and wrong-typed values fall through as false – matching the way the
// Groovy code treats unset booleans.
func scmmBool(cfg *config.Config, key string) bool {
	m, ok := cfg.Scm.Raw["scmManager"].(map[string]any)
	if !ok {
		return false
	}
	v, _ := m[key].(bool)
	return v
}
