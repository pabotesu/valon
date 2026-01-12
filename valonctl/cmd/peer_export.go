package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/pabotesu/valon/valonctl/pkg/client"
)

var (
	peerExportCmd = &cobra.Command{
		Use:   "export [output-file]",
		Short: "Export peer information to JSON file",
		Long: `Export all registered peers to a JSON file.
This excludes dynamic fields (LAN/NAT endpoints) for static hosting purposes (e.g., Cloudflare Pages).

Example:
  valonctl peer export              # exports to ./peers.json
  valonctl peer export peers.json   # explicit path`,
		Args: cobra.MaximumNArgs(1),
		RunE: runPeerExport,
	}
)

func init() {
	peerCmd.AddCommand(peerExportCmd)
}

// ExportedPeer represents static peer information suitable for export
type ExportedPeer struct {
	Pubkey string `json:"pubkey"`
	IP     string `json:"ip"`
	Alias  string `json:"alias"`
}

func runPeerExp"peers.json" // default
	if len(args) > 0 {
		outputPath = args[0]
	} *cobra.Command, args []string) error {
	outputPath := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create etcd client
	etcdClient, err := client.NewEtcdClient(&cfg.Etcd, &cfg.DDNS)
	if err != nil {
		return fmt.Errorf("failed to create etcd client: %w", err)
	}
	defer etcdClient.Close()

	// Retrieve peers
	peers, err := etcdClient.ListPeers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list peers: %w", err)
	}

	if len(peers) == 0 {
		return fmt.Errorf("no peers registered")
	}

	// Export peers to JSON (excluding dynamic endpoint fields)
	exportData := make([]ExportedPeer, 0, len(peers))
	for _, peer := range peers {
		exportData = append(exportData, ExportedPeer{
			Pubkey: peer.Pubkey,
			IP:     peer.IP,
			Alias:  peer.Alias,
		})
	}

	// Marshal to JSON with indentation
	jsonData, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Write to file
	if err := os.WriteFile(outputPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Printf("✓ Exported %d peers to %s\n", len(exportData), outputPath)
	return nil
}
