package cmd

import (
	"fmt"
	"os"

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
			fmt.Fprintf(os.Stderr, "ERROR while unmarshaling config\n")
		}
		
		println ("Starting Deployment ...")


		if globalConfig.Application.Debug {
			settings, _ := yaml.Marshal(viper.AllSettings())
			fmt.Fprintf(os.Stdout, "Config: \n%s\n", settings)
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

		for _, f := range allFeatures {
			if f.IsEnabled() {
				err := f.Install()
				if err != nil {
					println("Error while installing Feature \"" + f.Name() + "\": " + err.Error())
				}
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
}
