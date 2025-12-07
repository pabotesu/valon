package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/pabotesu/valon/valonctl/pkg/config"
)

var (
	// Global flags
	cfgFile string
	cfg     *config.Config

	// rootCmd represents the base command
	rootCmd = &cobra.Command{
		Use:   "valonctl",
		Short: "VALON - WireGuard peer management tool",
		Long: `valonctl is a command-line tool for managing WireGuard peers in a VALON network.
It provides commands for adding, removing, and listing peers, as well as checking system status.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip config loading for commands that don't need it
			if cmd.Use == "version" || cmd.Use == "help" {
				return nil
			}

			// Load configuration
			var err error
			cfg, err = config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			return nil
		},
	}
)

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default /etc/valon/valonctl.yml)")
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
