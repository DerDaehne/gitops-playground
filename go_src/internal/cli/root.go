// Package cli builds the Cobra command tree.
//
// The Groovy code uses Picocli with annotations directly on the Config
// struct. Here we keep the flag → config wiring explicit so the Config
// type stays free of CLI concerns. Behaviour mirrors GitopsPlaygroundCli:
//
//  1. Parse a first pass of CLI args to learn --profile, --config-file,
//     --config-map.
//  2. Load profile YAML (if any), config files, config maps in this order.
//  3. Apply defaults, then re-apply CLI args on top so they win.
//  4. Run the actual install or destroy.
//
// Step 4 lives in [Application] and is invoked from RunE; everything
// related to flag definition lives here.
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cloudogu/gitops-playground/go/internal/config"
	logpkg "github.com/cloudogu/gitops-playground/go/internal/log"

	"github.com/spf13/cobra"
)

// Execute parses args and runs the CLI. Returns the process exit code.
func Execute(args []string) (ReturnCode, error) {
	logpkg.Configure(os.Stderr, modeFromArgs(args))

	cfg := config.New()
	cmd := newRootCmd(cfg)
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		switch {
		case errors.Is(err, errNotConfirmed):
			return ReturnNotConfirmed, nil
		default:
			return ReturnGenericError, err
		}
	}
	return ReturnSuccess, nil
}

// errNotConfirmed signals that the user declined the install/destroy prompt.
var errNotConfirmed = errors.New("user did not confirm")

func newRootCmd(cfg *config.Config) *cobra.Command {
	var (
		showVersion bool
		outputCfg   bool
	)

	cmd := &cobra.Command{
		Use:           BinaryName,
		Short:         "CLI-tool to deploy gitops-playground.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				fmt.Fprintln(cmd.OutOrStdout(), VersionString())
				return nil
			}
			if outputCfg {
				yaml, err := cfg.ToYAML(false)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), yaml)
				return nil
			}
			// Real install/destroy is wired up once the application runner
			// (Phase 4) lands. For now we keep the command runnable so
			// --help / --version / --output-config-file work standalone.
			fmt.Fprintln(cmd.OutOrStdout(), "install/destroy not yet implemented")
			return nil
		},
	}

	flags := cmd.Flags()
	flags.BoolVarP(&showVersion, "version", "v", false, "Display version and license info")
	flags.BoolVar(&outputCfg, "output-config-file", false, "Output current config as config file as much as possible")

	registerGlobalFlags(flags, cfg)
	registerApplicationFlags(flags, cfg)
	registerRegistryFlags(flags, cfg)
	registerJenkinsFlags(flags, cfg)
	registerArgoCDFlags(flags, cfg)
	registerMonitoringFlags(flags, cfg)
	registerSecretsFlags(flags, cfg)
	registerMailFlags(flags, cfg)
	registerIngressFlags(flags, cfg)
	registerCertManagerFlags(flags, cfg)
	registerContentFlags(flags, cfg)

	return cmd
}

// modeFromArgs determines verbosity from raw args before Cobra parses them,
// so we can configure logging even for very early errors.
func modeFromArgs(args []string) logpkg.Mode {
	for _, a := range args {
		switch a {
		case "-x", "--trace":
			return logpkg.ModeTrace
		case "-d", "--debug":
			return logpkg.ModeDebug
		}
		if strings.HasPrefix(a, "--trace=") || strings.HasPrefix(a, "--debug=") {
			if strings.HasSuffix(a, "=true") {
				if strings.HasPrefix(a, "--trace=") {
					return logpkg.ModeTrace
				}
				return logpkg.ModeDebug
			}
		}
	}
	return logpkg.ModeInfo
}
