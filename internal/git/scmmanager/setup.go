package scmmanager

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/DerDaehne/gitops-playground/internal/config"
	gitconfig "github.com/DerDaehne/gitops-playground/internal/git/config"
	"github.com/DerDaehne/gitops-playground/internal/git/scmmanager/api"
	"github.com/DerDaehne/gitops-playground/internal/helm"
)

func SetupHelm(scmmConfig *gitconfig.SCMManagerTenantConfig) error {
	slog.Info("Deploying SCM-Manager via Helm")
	_, err := helm.DeployHelmChart(scmmConfig.Helm, scmmConfig.Namespace, "scmm")
	return err
}

func WaitForScmmAvailable(apiClient *api.ScmManagerApiClient, timeoutSeconds int, intervalMillis int) error {
	slog.Info("Waiting for SCM-Manager to become available...")
	startTime := time.Now()
	timeout := time.Duration(timeoutSeconds) * time.Second
	interval := time.Duration(intervalMillis) * time.Millisecond

	for time.Since(startTime) < timeout {
		err := apiClient.CheckAvailable()
		if err == nil {
			slog.Info("SCM-Manager is available")
			return nil
		}
		slog.Debug("Waiting for SCM-Manager...", "error", err)
		time.Sleep(interval)
	}
	return fmt.Errorf("timeout: SCM-Manager did not respond with 200 OK within %d seconds", timeoutSeconds)
}

func Configure(apiClient *api.ScmManagerApiClient, appConfig *config.ApplicationConfig, scmmConfig *gitconfig.SCMManagerTenantConfig, jenkinsConfig *config.JenkinsConfig, scmmUrl string) error {
	if err := installScmmPlugins(apiClient, scmmConfig, jenkinsConfig); err != nil {
		return err
	}
	if err := setSetupConfigs(apiClient, scmmUrl); err != nil {
		return err
	}
	if jenkinsConfig.Active {
		if err := configureJenkinsPlugin(apiClient, jenkinsConfig); err != nil {
			return err
		}
	}
	if err := addDefaultUsers(apiClient, appConfig, scmmConfig); err != nil {
		return err
	}
	slog.Info("ScmManager Setup finished!")
	return nil
}

func installScmmPlugins(apiClient *api.ScmManagerApiClient, scmmConfig *gitconfig.SCMManagerTenantConfig, jenkinsConfig *config.JenkinsConfig) error {
	if scmmConfig.SkipPlugins {
		slog.Debug("Skipping SCM plugin installation")
		return nil
	}

	pluginNames := []string{
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

	if jenkinsConfig.Active {
		pluginNames = append(pluginNames, "scm-jenkins-plugin")
	}

	restartForLastPlugin := false
	for i, pluginName := range pluginNames {
		slog.Debug("Installing Plugin " + pluginName)
		restart := !scmmConfig.SkipRestart && i == len(pluginNames)-1
		if err := apiClient.InstallPlugin(pluginName, restart); err != nil {
			return err
		}
		if restart {
			restartForLastPlugin = true
		}
	}

	slog.Debug("SCM-Manager plugin installation finished successfully!")
	if restartForLastPlugin {
		if err := WaitForScmmAvailable(apiClient, 180, 2000); err != nil {
			return err
		}
	}
	return nil
}

func setSetupConfigs(apiClient *api.ScmManagerApiClient, scmmUrl string) error {
	setupConfigs := map[string]any{
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
		"baseUrl":                  scmmUrl,
		"forceBaseUrl":             false,
		"loginAttemptLimit":        -1,
		"proxyExcludes":            []string{},
		"skipFailedAuthenticators": false,
		"pluginUrl":                "https://plugin-center-api.scm-manager.org/api/v1/plugins/{version}?os={os}&arch={arch}",
		"loginAttemptLimitTimeout": 300,
		"enabledXsrfProtection":    true,
		"namespaceStrategy":        "CustomNamespaceStrategy",
		"loginInfoUrl":             "https://login-info.scm-manager.org/api/v1/login-info",
		"releaseFeedUrl":           "https://scm-manager.org/download/rss.xml",
		"mailDomainName":           "scm-manager.local",
		"adminGroups":              []string{},
		"adminUsers":               []string{},
	}
	if err := apiClient.SetConfig(setupConfigs); err != nil {
		return err
	}
	slog.Debug("Successfully added SCMM Setup Configs")
	return nil
}

func configureJenkinsPlugin(apiClient *api.ScmManagerApiClient, jenkinsConfig *config.JenkinsConfig) error {
	jenkinsPluginConfig := map[string]any{
		"disableRepositoryConfiguration": false,
		"disableMercurialTrigger":        false,
		"disableGitTrigger":              false,
		"disableEventTrigger":            false,
		"url":                            jenkinsConfig.Url,
	}
	if err := apiClient.ConfigureJenkinsPlugin(jenkinsPluginConfig); err != nil {
		return err
	}
	slog.Debug("Successfully configured JenkinsPlugin in SCM-Manager.")
	return nil
}

func addDefaultUsers(apiClient *api.ScmManagerApiClient, appConfig *config.ApplicationConfig, scmmConfig *gitconfig.SCMManagerTenantConfig) error {
	metricsUsername := appConfig.NamePrefix + "metrics"
	if err := addUser(apiClient, scmmConfig.GitOpsUsername, scmmConfig.Credentials.Password); err != nil {
		return err
	}
	if err := addUser(apiClient, metricsUsername, scmmConfig.Credentials.Password); err != nil {
		return err
	}
	if err := grantUserPermissions(apiClient, metricsUsername, []string{"metrics:read"}); err != nil {
		return err
	}
	return nil
}

func addUser(apiClient *api.ScmManagerApiClient, username string, password string) error {
	user := api.ScmManagerUser{
		Name:        username,
		DisplayName: username,
		Mail:        "changeme@test.local",
		External:    false,
		Password:    password,
		Active:      true,
	}
	if err := apiClient.AddUser(user); err != nil {
		return err
	}
	slog.Debug("Successfully created SCM-Manager User.")
	return nil
}

func grantUserPermissions(apiClient *api.ScmManagerApiClient, username string, permissions []string) error {
	if err := apiClient.SetPermissionForUser(username, permissions); err != nil {
		return err
	}
	slog.Debug("Granted permissions to user " + username)
	return nil
}
