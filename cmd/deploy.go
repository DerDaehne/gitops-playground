package cmd

import (
	"fmt"
	"os"

	"github.com/DerDaehne/gitops-playground/internal/config"
	"github.com/DerDaehne/gitops-playground/internal/features"
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

		// IMPORTANT: the order of features here defines the order in which they are installed
		allFeatures := []features.Feature{
			&features.ArgoCD{Config: globalConfig.Features.ArgoCD},
		}

		for _, f := range allFeatures {
			if f.IsEnabled() { println(f.Name()) }
		}
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
}
