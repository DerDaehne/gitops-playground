package cmd

import (
	"fmt"
	"log/slog"

	"github.com/DerDaehne/gitops-playground/internal/config"
	"github.com/DerDaehne/gitops-playground/internal/features"
	"github.com/DerDaehne/gitops-playground/internal/kubeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v2"
)

// deployCmd represents the deploy command
var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "starts the depoyment of chosen applications",
	Long: `deploy takes a lot of flags, which each represent a
feature or config setting. 

you can turn everything on and off as you wish`,
	Run: func(cmd *cobra.Command, args []string) {

		var globalConfig config.Config
		if err := viper.Unmarshal(&globalConfig); err != nil {
			slog.Error("There was an error while unmarshaling the config: " + err.Error())
		}

		if globalConfig.Application.Debug {
			settings, _ := yaml.Marshal(viper.AllSettings())
			slog.Debug(fmt.Sprintf("Config: \n%s\n", settings))
		}

		kubernetesClientSet := kubeutils.GetKubernetesClient("")
		// IMPORTANT: the order of features here defines the order in which they are installed
		// registry -> git -> jenkins -> argo -> ingress -> certmngr -> mail -> monitoring -> eso -> vault -> CL
		allFeatures := []features.Feature{
			&features.Registry{Config: globalConfig.Registry, KubernetesClientSet: kubernetesClientSet, NamePrefix: globalConfig.Application.NamePrefix},
			&features.Jenkins{Config: globalConfig.Jenkins, KubernetesClientSet: kubernetesClientSet},
			&features.ArgoCD{Config: globalConfig.Features.ArgoCD, KubernetesClientSet: kubernetesClientSet},
			&features.Ingress{Config: globalConfig.Features.Ingress, KubernetesClientSet: kubernetesClientSet},
			&features.CertManager{Config: globalConfig.Features.CertManager, KubernetesClientSet: kubernetesClientSet},
			&features.Mail{Config: globalConfig.Features.Mail, KubernetesClientSet: kubernetesClientSet},
			&features.Monitoring{Config: globalConfig.Features.Monitoring, KubernetesClientSet: kubernetesClientSet},
			&features.ExternalSecretsOperator{Config: globalConfig.Features.Secrets, KubernetesClientSet: kubernetesClientSet},
			&features.Vault{Config: globalConfig.Features.Secrets, KubernetesClientSet: kubernetesClientSet},
			&features.ContentLoader{Config: globalConfig.Content, KubernetesClientSet: kubernetesClientSet},
		}

		slog.Info("Starting Deployment...")
		for _, f := range allFeatures {
			if f.IsEnabled() {
				slog.Info("Installing Feature " + f.Name())
				err := f.Install()
				if err != nil {
					slog.Error("Error while installing Feature " + f.Name() + ": " + err.Error())
				}
			}
		}
		slog.Info("Deployment finished successfully!")
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
}
