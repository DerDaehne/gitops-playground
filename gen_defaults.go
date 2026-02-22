//go:build ignore

package main

import (
	"fmt"

	"github.com/DerDaehne/gitops-playground/internal/config"
	"gopkg.in/yaml.v3"
)

func main() {
	cfg := config.Config{}
	out, _ := yaml.Marshal(cfg)
	fmt.Println(string(out))
}
