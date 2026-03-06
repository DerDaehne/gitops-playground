package helm

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"go.yaml.in/yaml/v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
	"helm.sh/helm/v3/pkg/storage/driver"
)

func addHelmRepository(helmConfig HelmConfig, settings *cli.EnvSettings) error {
	repositoryFilePath := settings.RepositoryConfig

	buf, err := os.ReadFile(repositoryFilePath)
	if err != nil { return err }

	var repositoryFile repo.File
	yaml.Unmarshal(buf, &repositoryFile)

	repositoryEntry := &repo.Entry{
		Name: helmConfig.Chart,
		URL: helmConfig.RepoUrl,
	}

	repository, err := repo.NewChartRepository(repositoryEntry, getter.All(settings))
	if err != nil { return  err }

	_, err = repository.DownloadIndexFile()
	if  err != nil { return err }

	repositoryFile.Update(repositoryEntry)
	
	return repositoryFile.WriteFile(repositoryFilePath, 0644)
}

func installHelmChart(helmConfig HelmConfig, namespace string, releaseName string, actionConfig *action.Configuration, settings *cli.EnvSettings) (*release.Release, error) {
	err := addHelmRepository(helmConfig, settings)
	if err != nil { return nil, err }


	deployClient := action.NewInstall(actionConfig)
	deployClient.ReleaseName = releaseName
	deployClient.Namespace = namespace
	deployClient.Version = helmConfig.Version
	deployClient.CreateNamespace = true
	
	chartPath, err := deployClient.ChartPathOptions.LocateChart(helmConfig.Chart + "/" + helmConfig.Chart, settings)
	if err != nil { return nil, err }

	chart, err := loader.Load(chartPath)
	if err != nil { return nil, err }

	return deployClient.Run(chart, helmConfig.Values)
}

func upgradeHelmChart(helmConfig HelmConfig, namespace string, releaseName string, actionConfig *action.Configuration, settings *cli.EnvSettings) (*release.Release, error) {
	err := addHelmRepository(helmConfig, settings)
	if err != nil { return nil, err }

	upgradeClient := action.NewUpgrade(actionConfig)
	upgradeClient.Namespace = namespace
	upgradeClient.Version = helmConfig.Version
	
	chartPath, err := upgradeClient.ChartPathOptions.LocateChart(helmConfig.Chart + "/" + helmConfig.Chart, settings)
	if err != nil { return nil, err }

	chart, err := loader.Load(chartPath)
	if err != nil { return nil, err }

	return upgradeClient.Run(releaseName, chart, helmConfig.Values)
}

func DeployHelmChart (helmConfig HelmConfig, namespace string, releaseName string) (*release.Release, error) {
	settings := cli.New()

	actionConfig := new(action.Configuration)
	err := actionConfig.Init(settings.RESTClientGetter(), namespace, "secret", func(format string, v ...interface{}) {
		slog.Debug(fmt.Sprintf(format, v...))	
	})
	if err != nil { return nil, err }

	actionConfig.KubeClient = kube.New(settings.RESTClientGetter())

	historyClient := action.NewHistory(actionConfig)
	historyClient.Max = 1
	_, err = historyClient.Run(releaseName)

	if err != nil && errors.Is(err, driver.ErrReleaseNotFound) {
		return installHelmChart(helmConfig, namespace, releaseName, actionConfig, settings)
	}
	if err != nil { return nil, err }

	return upgradeHelmChart(helmConfig, namespace, releaseName, actionConfig, settings)
}
