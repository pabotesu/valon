package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/pabotesu/valon/valonctl/pkg/client"
)

var (
	peerRemoveCmd = &cobra.Command{
		Use:   "remove <pubkey|alias>",
		Short: "Remove a peer from VALON",
		Long: `Remove a WireGuard peer from the VALON network.
You can specify either the public key or the alias.
This command:
1. Removes the peer from the WireGuard interface
2. Deletes the peer registration from etcd`,
		Args: cobra.ExactArgs(1),
		RunE: runPeerRemove,
	}
)

func init() {
	peerCmd.AddCommand(peerRemoveCmd)
}

func runPeerRemove(cmd *cobra.Command, args []string) error {
	pubkeyOrAlias := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create etcd client
	etcdClient, err := client.NewEtcdClient(&cfg.Etcd, &cfg.DDNS)
	if err != nil {
		return fmt.Errorf("failed to create etcd client: %w", err)
	}
	defer etcdClient.Close()

	// Get peer info first to retrieve the pubkey (if alias was provided)
	peers, err := etcdClient.ListPeers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list peers: %w", err)
	}

	var targetPubkey string
	var targetAlias string
	for _, peer := range peers {
		if peer.Pubkey == pubkeyOrAlias || peer.Alias == pubkeyOrAlias {
			targetPubkey = peer.Pubkey
			targetAlias = peer.Alias
			break
		}
	}

	if targetPubkey == "" {
		return fmt.Errorf("peer %q not found", pubkeyOrAlias)
	}

	// Create WireGuard client
	wgClient, err := client.NewWireGuardClient()
	if err != nil {
		return fmt.Errorf("failed to create WireGuard client: %w", err)
	}
	defer wgClient.Close()

	// Remove from WireGuard interface
	fmt.Printf("Removing peer from WireGuard interface %s...\n", cfg.WireGuard.Interface)
	if err := wgClient.RemovePeer(cfg.WireGuard.Interface, targetPubkey); err != nil {
		fmt.Printf("Warning: failed to remove peer from WireGuard: %v\n", err)
		// Continue to remove from etcd anyway
	}

	// Remove from etcd
	fmt.Println("Removing peer from etcd...")
	if err := etcdClient.RemovePeer(ctx, pubkeyOrAlias); err != nil {
		return fmt.Errorf("failed to remove peer from etcd: %w", err)
	}

	fmt.Printf("✓ Successfully removed peer %s (alias: %s)\n", targetPubkey, targetAlias)
	return nil
}
