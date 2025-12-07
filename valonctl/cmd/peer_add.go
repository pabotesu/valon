package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/pabotesu/valon/valonctl/pkg/client"
	"github.com/pabotesu/valon/valonctl/pkg/validation"
)

var (
	addWgIP  string
	addAlias string

	peerAddCmd = &cobra.Command{
		Use:   "add <pubkey>",
		Short: "Add a new peer to VALON",
		Long: `Add a new WireGuard peer to the VALON network.
This command:
1. Validates the public key and alias
2. Adds the peer to the WireGuard interface
3. Registers the peer in etcd with alias mapping`,
		Args: cobra.ExactArgs(1),
		RunE: runPeerAdd,
	}
)

func init() {
	peerAddCmd.Flags().StringVar(&addWgIP, "wg-ip", "", "WireGuard IP address for the peer (required)")
	peerAddCmd.Flags().StringVar(&addAlias, "alias", "", "User-friendly alias for the peer (required)")
	peerAddCmd.MarkFlagRequired("wg-ip")
	peerAddCmd.MarkFlagRequired("alias")

	peerCmd.AddCommand(peerAddCmd)
}

func runPeerAdd(cmd *cobra.Command, args []string) error {
	pubkey := args[0]

	// Validate alias
	if err := validation.ValidateAlias(addAlias); err != nil {
		return fmt.Errorf("invalid alias: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create WireGuard client
	wgClient, err := client.NewWireGuardClient()
	if err != nil {
		return fmt.Errorf("failed to create WireGuard client: %w", err)
	}
	defer wgClient.Close()

	// Add peer to WireGuard interface
	fmt.Printf("Adding peer to WireGuard interface %s...\n", cfg.WireGuard.Interface)
	if err := wgClient.AddPeer(cfg.WireGuard.Interface, pubkey, addWgIP); err != nil {
		return fmt.Errorf("failed to add peer to WireGuard: %w", err)
	}

	// Create etcd client
	etcdClient, err := client.NewEtcdClient(&cfg.Etcd)
	if err != nil {
		return fmt.Errorf("failed to create etcd client: %w", err)
	}
	defer etcdClient.Close()

	// Register peer in etcd
	fmt.Println("Registering peer in etcd...")
	peerInfo := &client.PeerInfo{
		Pubkey: pubkey,
		IP:     addWgIP,
		Alias:  addAlias,
	}

	if err := etcdClient.AddPeer(ctx, peerInfo); err != nil {
		// Rollback: remove from WireGuard
		fmt.Println("Failed to register in etcd, rolling back WireGuard configuration...")
		_ = wgClient.RemovePeer(cfg.WireGuard.Interface, pubkey)
		return fmt.Errorf("failed to register peer in etcd: %w", err)
	}

	fmt.Printf("✓ Successfully added peer %s (alias: %s, IP: %s)\n", pubkey, addAlias, addWgIP)
	return nil
}
