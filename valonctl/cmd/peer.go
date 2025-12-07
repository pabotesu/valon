package main

import (
	"github.com/spf13/cobra"
)

var (
	peerCmd = &cobra.Command{
		Use:   "peer",
		Short: "Manage VALON peers",
		Long:  `Commands for adding, removing, and listing WireGuard peers in the VALON network.`,
	}
)

func init() {
	rootCmd.AddCommand(peerCmd)
}
