package features

import "github.com/DerDaehne/gitops-playground/internal/config"

type ContentLoader struct {
	Config config.ContentLoaderConfig
}

func (content *ContentLoader) Name() string {
	return "ContentLoader"
}

func (content *ContentLoader) IsEnabled() bool {
	if len(content.Config.Repos) == 0 {
		return false
	}
	return true
}

func (content *ContentLoader) Validate() error {
	return nil
}

func (content *ContentLoader) Install() error {
	println("Installing ContentLoader")
	return nil
}
