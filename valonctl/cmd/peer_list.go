package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/pabotesu/valon/valonctl/pkg/client"
)

var (
	peerListCmd = &cobra.Command{
		Use:   "list",
		Short: "List all registered peers",
		Long:  `Display a table of all peers registered in the VALON network with their details.`,
		RunE:  runPeerList,
	}
)

func init() {
	peerCmd.AddCommand(peerListCmd)
}

func runPeerList(cmd *cobra.Command, args []string) error {
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
		fmt.Println("No peers registered.")
		return nil
	}

	// Print table
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ALIAS\tPUBKEY (short)\tWG IP\tENDPOINT (LAN)\tENDPOINT (NAT)\tLAST SEEN")
	fmt.Fprintln(w, "-----\t--------------\t-----\t---------------\t---------------\t---------")

	for _, peer := range peers {
		pubkeyShort := truncatePubkey(peer.Pubkey)

		lanEndpoint := peer.LANEndpoint
		if lanEndpoint == "" {
			lanEndpoint = "-"
		}

		natEndpoint := peer.NATEndpoint
		if natEndpoint == "" {
			natEndpoint = "-"
		}

		lastSeen := formatLastSeen(peer.LastSeen)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			peer.Alias,
			pubkeyShort,
			peer.IP,
			lanEndpoint,
			natEndpoint,
			lastSeen,
		)
	}

	w.Flush()
	return nil
}

// truncatePubkey shortens a public key for display (first 5 chars + "...")
func truncatePubkey(pubkey string) string {
	if len(pubkey) <= 8 {
		return pubkey
	}
	return pubkey[:5] + "..."
}

// formatLastSeen formats the last seen timestamp
func formatLastSeen(t time.Time) string {
	if t.IsZero() {
		return "never"
	}

	elapsed := time.Since(t)
	if elapsed < time.Minute {
		return "just now"
	} else if elapsed < time.Hour {
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	} else if elapsed < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	} else {
		return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
	}
}
