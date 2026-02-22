package config

import (
	"bytes"
	_ "embed"

	"github.com/spf13/viper"
)

//go:embed defaults.yaml
var DefaultConfigValues []byte

func LoadDefaults() error {
	viper.SetConfigType("yaml")
	return viper.ReadConfig(bytes.NewReader(DefaultConfigValues))
}
