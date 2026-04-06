package cli

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/sydneyowl/wsjtx-relay/internal/server/config"
	"github.com/sydneyowl/wsjtx-relay/internal/server/runtime"
	"github.com/sydneyowl/wsjtx-relay/internal/server/tlsutil"
	"github.com/sydneyowl/wsjtx-relay/internal/shared/buildinfo"
	"github.com/sydneyowl/wsjtx-relay/internal/shared/cliargs"
)

func Execute(args []string) error {
	if args == nil {
		args = os.Args[1:]
	}
	if err := cliargs.RejectSingleDashLongFlags(args); err != nil {
		return err
	}

	cmd := NewRootCmd()
	cmd.SetArgs(args)
	return cmd.Execute()
}

func NewRootCmd() *cobra.Command {
	defaults := config.DefaultConfig()
	flagValues := defaults
	configPath := ""
	showVersion := false

	cmd := &cobra.Command{
		Use:               "wsjtx-relay-server",
		Short:             "Run the WSJT-X relay server",
		Args:              cobra.NoArgs,
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				return buildinfo.WriteVersion(cmd.OutOrStdout(), "wsjtx-relay-server")
			}

			cfg, err := config.LoadForCLI(configPath, flagValues, cmd.Flags().Changed)
			if err != nil {
				return err
			}
			return runServer(cfg)
		},
	}

	cmd.Flags().BoolVar(&showVersion, "version", false, "print version info and exit")
	config.BindFlags(cmd.Flags(), &flagValues, &configPath)
	cmd.AddCommand(newVersionCmd("wsjtx-relay-server"))
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

func runServer(cfg config.Config) error {
	certificate, err := tlsutil.EnsureCertificate(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return fmt.Errorf("prepare TLS certificate: %w", err)
	}

	relayServer := runtime.NewServer(cfg)
	httpServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: relayServer.Routes(),
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{certificate},
		},
	}

	log.Printf("wsjtx-relay-server listening on https://%s", cfg.ListenAddr)
	log.Printf("shared secret loaded from %s", cfg.SharedSecretFile)
	if err := httpServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("relay server stopped with error: %w", err)
	}
	return nil
}
