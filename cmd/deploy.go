package cmd

import (
	"fmt"
	"os"

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
		if viper.GetBool("application.debug") {
			settings, _ := yaml.Marshal(viper.AllSettings())
			fmt.Fprintf(os.Stdout, "Config: \n%s\n", settings)
		}
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
}
