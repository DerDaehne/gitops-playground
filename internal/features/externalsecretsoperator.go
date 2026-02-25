package features

import "github.com/DerDaehne/gitops-playground/internal/config"

type ExternalSecretsOperator struct {
	Config config.ExternalSecretsOperatorConfig
}

func (eso *ExternalSecretsOperator) Name() string {
	return "ExternalSecretsOperator"
}

func (eso *ExternalSecretsOperator) IsEnabled() bool {
	return eso.Config.Active
}

func (eso *ExternalSecretsOperator) Validate() error {
	return nil
}

func (eso *ExternalSecretsOperator) Install() error {
	println("Installing ExternalSecretsOperator")
	return nil
}
