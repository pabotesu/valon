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
	peerAddCmd.Flags().StringVar(&addWgIP, "wg-ip", "", "WireGuard IP address for the peer (auto-allocated if not specified)")
	peerAddCmd.Flags().StringVar(&addAlias, "alias", "", "User-friendly alias for the peer (required)")
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

	// Create etcd client first for IP allocation
	etcdClient, err := client.NewEtcdClient(&cfg.Etcd, &cfg.DDNS)
	if err != nil {
		return fmt.Errorf("failed to create etcd client: %w", err)
	}
	defer etcdClient.Close()

	// Auto-allocate IP if not specified
	if addWgIP == "" {
		fmt.Println("Auto-allocating IP address...")
		allocatedIP, err := etcdClient.AllocateIP(ctx, cfg.WireGuard.Network)
		if err != nil {
			return fmt.Errorf("failed to auto-allocate IP: %w", err)
		}
		addWgIP = allocatedIP
		fmt.Printf("  Allocated IP: %s\n", addWgIP)
	}

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

	// Generate WireGuard configuration file for the client
	fmt.Println("\n=== WireGuard Configuration for Client ===")
	if err := printClientConfig(wgClient, pubkey, addWgIP); err != nil {
		fmt.Printf("Warning: Failed to generate client config: %v\n", err)
	}
	fmt.Println("==========================================")

	return nil
}

func printClientConfig(wgClient *client.WireGuardClient, clientPubkey, clientIP string) error {
	// Get Discovery Role's public key
	discoveryPubkey, err := wgClient.GetPublicKey(cfg.WireGuard.Interface)
	if err != nil {
		return fmt.Errorf("failed to get Discovery public key: %w", err)
	}

	// Use endpoint from config, or provide placeholder
	discoveryEndpoint := cfg.WireGuard.Endpoint
	if discoveryEndpoint == "" {
		discoveryEndpoint = "<DISCOVERY_ROLE_LAN_IP:51820>"
	}

	// Get network prefix from IP (assume /24 for simplicity, can be enhanced)
	networkPrefix := "24"

	fmt.Printf(`
Save this as /etc/wireguard/wg0.conf on the client:

[Interface]
Address = %s/%s
PrivateKey = <INSERT_YOUR_PRIVATE_KEY_HERE>
MTU = 1420

[Peer]
# Discovery Role
PublicKey = %s
Endpoint = %s
AllowedIPs = %s/32
PersistentKeepalive = 25

Then run on the client:
  1. Generate keys: wg genkey | tee privatekey | wg pubkey
  2. Edit /etc/wireguard/wg0.conf and insert your PrivateKey
  3. Start interface: sudo wg-quick up wg0
  4. Bootstrap: sudo valon-bootstrap
`, clientIP, networkPrefix, discoveryPubkey, discoveryEndpoint, cfg.WireGuard.IP)

	return nil
}
