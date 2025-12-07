package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/pabotesu/valon/valonctl/pkg/client"
)

var (
	statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Show VALON system status",
		Long:  `Display the health status of WireGuard, etcd, and CoreDNS DDNS API.`,
		RunE:  runStatus,
	}
)

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("VALON System Status")
	fmt.Println("===================")

	// Check WireGuard interface
	wgClient, err := client.NewWireGuardClient()
	if err != nil {
		fmt.Printf("WireGuard Interface: ✗ (failed to create client: %v)\n", err)
	} else {
		defer wgClient.Close()

		isUp, err := wgClient.IsInterfaceUp(cfg.WireGuard.Interface)
		if err != nil {
			fmt.Printf("WireGuard Interface: ✗ %s (error: %v)\n", cfg.WireGuard.Interface, err)
		} else if !isUp {
			fmt.Printf("WireGuard Interface: ✗ %s (down)\n", cfg.WireGuard.Interface)
		} else {
			peerCount, _ := wgClient.GetPeerCount(cfg.WireGuard.Interface)
			fmt.Printf("WireGuard Interface: ✓ %s (up, %d peers)\n", cfg.WireGuard.Interface, peerCount)
		}
	}

	// Check etcd connection
	etcdClient, err := client.NewEtcdClient(&cfg.Etcd, &cfg.DDNS)
	if err != nil {
		fmt.Printf("Etcd: ✗ (failed to create client: %v)\n", err)
	} else {
		defer etcdClient.Close()

		if err := etcdClient.Ping(ctx); err != nil {
			fmt.Printf("Etcd: ✗ %v (unreachable: %v)\n", cfg.Etcd.Endpoints, err)
		} else {
			// Count registered peers
			peers, err := etcdClient.ListPeers(ctx)
			peerCount := 0
			if err == nil {
				peerCount = len(peers)
			}
			fmt.Printf("Etcd: ✓ Connected (%v, %d peers registered)\n", cfg.Etcd.Endpoints, peerCount)
		}
	}

	// Check CoreDNS DDNS API
	ddnsClient := client.NewDDNSClient(cfg.DDNS.APIURL)
	if err := ddnsClient.Ping(ctx); err != nil {
		fmt.Printf("CoreDNS DDNS API: ✗ %s (unreachable: %v)\n", cfg.DDNS.APIURL, err)
	} else {
		fmt.Printf("CoreDNS DDNS API: ✓ Reachable (%s)\n", cfg.DDNS.APIURL)
	}

	return nil
}
