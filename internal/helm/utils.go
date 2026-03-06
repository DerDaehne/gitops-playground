package helm

import (
	"os"

	"go.yaml.in/yaml/v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
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

func DeployHelmChart (helmConfig HelmConfig, namespace string, releaseName string) (*release.Release, error) {
	settings := cli.New()

	err := addHelmRepository(helmConfig, settings)
	if err != nil { return nil, err }

	actionConfig := new(action.Configuration)
	err = actionConfig.Init(settings.RESTClientGetter(), namespace, "secret", nil)
	if err != nil { return nil, err }

	actionConfig.KubeClient = kube.New(settings.RESTClientGetter())

	client := action.NewInstall(actionConfig)
	client.ReleaseName = releaseName
	client.Namespace = namespace
	client.Version = helmConfig.Version
	client.CreateNamespace = true
	
	chartPath, err := client.ChartPathOptions.LocateChart(helmConfig.Chart + "/" + helmConfig.Chart, settings)
	if err != nil { return nil, err }

	chart, err := loader.Load(chartPath)
	if err != nil { return nil, err }

	return client.Run(chart, helmConfig.Values)
}
