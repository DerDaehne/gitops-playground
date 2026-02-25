package features

import "github.com/DerDaehne/gitops-playground/internal/config"

type Vault struct {
	Config config.VaultConfig
}

func (vault *Vault) Name() string {
	return "Vault"
}

func (vault *Vault) IsEnabled() bool {
	return vault.Config.Active
}

func (vault *Vault) Validate() error {
	return nil
}

func (vault *Vault) Install() error {
	println("Installing Vault")
	return nil
}
