package features

import "github.com/DerDaehne/gitops-playground/internal/config"

type Monitoring struct {
	Config config.MonitoringConfig
}

func (monitoring *Monitoring) Name() string {
	return "Monitoring"
}

func (monitoring *Monitoring) IsEnabled() bool {
	return monitoring.Config.Active
}

func (monitoring *Monitoring) Validate() error {
	return nil
}

func (monitoring *Monitoring) Install() error {
	println("Installing Monitoring")
	return nil
}
