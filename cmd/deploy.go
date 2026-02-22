package cmd

import (
	"github.com/DerDaehne/gitops-playground/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// deployCmd represents the deploy command
var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "starts the depoyment of chosen applications",
	Long: `deploy takes a lot of flags, which each represent a
feature or config setting. 

you can turn everything on and off as you wish`,
	Run: func(cmd *cobra.Command, args []string) {
		var cfg config.Config
		viper.Unmarshal(&cfg)
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)

}
