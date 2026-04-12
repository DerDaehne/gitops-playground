package cmd

import (
	"log/slog"
	"os"

	"github.com/DerDaehne/gitops-playground/internal/config"
	"github.com/DerDaehne/gitops-playground/internal/feature"
	"github.com/DerDaehne/gitops-playground/internal/feature/argocd"
	"github.com/DerDaehne/gitops-playground/internal/feature/certmanager"
	"github.com/DerDaehne/gitops-playground/internal/feature/content"
	"github.com/DerDaehne/gitops-playground/internal/feature/eso"
	"github.com/DerDaehne/gitops-playground/internal/feature/githandler"
	"github.com/DerDaehne/gitops-playground/internal/feature/ingress"
	"github.com/DerDaehne/gitops-playground/internal/feature/jenkins"
	"github.com/DerDaehne/gitops-playground/internal/feature/mail"
	"github.com/DerDaehne/gitops-playground/internal/feature/monitoring"
	"github.com/DerDaehne/gitops-playground/internal/feature/registry"
	"github.com/DerDaehne/gitops-playground/internal/feature/vault"
	"github.com/DerDaehne/gitops-playground/internal/pipeline"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Install or update the GitOps platform on the cluster",
	Long: `apply installs all enabled features in dependency order.
Independent features are installed in parallel.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := viper.Unmarshal(&cfg); err != nil {
			return err
		}

		features := allFeatures(cfg)
		executor := &pipeline.Executor{}
		return executor.Apply(cmd.Context(), features)
	},
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Remove all GitOps platform components from the cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := viper.Unmarshal(&cfg); err != nil {
			return err
		}
		features := allFeatures(cfg)
		executor := &pipeline.Executor{}
		return executor.Destroy(cmd.Context(), features)
	},
}

func allFeatures(cfg config.Config) []feature.Feature {
	return []feature.Feature{
		&githandler.Feature{Config: &cfg},
		&argocd.Feature{Config: cfg.Features.ArgoCD},
		&registry.Feature{Config: cfg.Registry},
		&jenkins.Feature{Config: cfg.Jenkins},
		&ingress.Feature{Config: cfg.Features.Ingress},
		&certmanager.Feature{Config: cfg.Features.CertManager},
		&monitoring.Feature{Config: cfg.Features.Monitoring},
		&vault.Feature{Config: cfg.Features.Secrets},
		&eso.Feature{Config: cfg.Features.Secrets},
		&content.Feature{Config: cfg.Content},
		&mail.Feature{Config: cfg.Features.Mail},
	}
}

func init() {
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(destroyCmd)
}

func exitOnErr(err error) {
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
