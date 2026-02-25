package features

import "github.com/DerDaehne/gitops-playground/internal/config"

type Jenkins struct {
	Config config.JenkinsConfig
}

func (jenkins *Jenkins) Name() string {
	return "Jenkins"
}

func (jenkins *Jenkins) IsEnabled() bool {
	return jenkins.Config.Active
}

func (jenkins *Jenkins) Validate() error {
	return nil
}

func (jenkins *Jenkins) Install() error {
	println("Installing Jenkins")
	return nil
}
