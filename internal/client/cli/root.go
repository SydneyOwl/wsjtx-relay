package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/sydneyowl/wsjtx-relay/internal/client/config"
	"github.com/sydneyowl/wsjtx-relay/internal/client/relay"
	"github.com/sydneyowl/wsjtx-relay/internal/shared/buildinfo"
	"github.com/sydneyowl/wsjtx-relay/internal/shared/cliargs"
)

func Execute(ctx context.Context, args []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if args == nil {
		args = os.Args[1:]
	}
	if err := cliargs.RejectSingleDashLongFlags(args); err != nil {
		return err
	}

	cmd := NewRootCmd()
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

func NewRootCmd() *cobra.Command {
	defaults := config.DefaultConfig()
	flagValues := defaults
	configPath := ""
	showVersion := false

	cmd := &cobra.Command{
		Use:               "wsjtx-relay-client",
		Short:             "Bridge WSJT-X or JTDX UDP traffic to the relay server",
		Args:              cobra.NoArgs,
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				return buildinfo.WriteVersion(cmd.OutOrStdout(), "wsjtx-relay-client")
			}

			cfg, err := config.LoadForCLI(configPath, flagValues, cmd.Flags().Changed)
			if err != nil {
				return err
			}
			return relay.New(cfg).Run(cmd.Context())
		},
	}

	cmd.Flags().BoolVar(&showVersion, "version", false, "print version info and exit")
	config.BindFlags(cmd.Flags(), &flagValues, &configPath)
	cmd.AddCommand(newVersionCmd("wsjtx-relay-client"))
	return cmd
}

func newVersionCmd(binaryName string) *cobra.Command {
	return &cobra.Command{
		Use:          "version",
		Short:        "Print version info",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return buildinfo.WriteVersion(cmd.OutOrStdout(), binaryName)
		},
	}
}
