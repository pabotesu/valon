// Package valon implements a CoreDNS plugin for VALON (Virtual Adaptive Logical Overlay Network).
//
// VALON provides DNS-SD based peer discovery for WireGuard overlay networks.
package valon

import (
	"context"
	"log"
	"time"

	"github.com/coredns/coredns/plugin"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Valon is the main plugin structure.
type Valon struct {
	Next plugin.Handler // Next plugin in the chain

	// Configuration (from Corefile)
	EtcdEndpoints []string // etcd endpoints
	WgInterface   string   // WireGuard interface name
	DdnsListen    string   // DDNS API listen address

	// Zone
	Zone string // DNS zone (e.g., "valon.internal.")

	// Runtime
	etcdClient *clientv3.Client // etcd client
}

// Name returns the plugin name.
func (v Valon) Name() string {
	return "valon"
}

// Init initializes the VALON plugin.
func (v *Valon) Init() error {
	log.Printf("[valon] Initializing VALON plugin")
	log.Printf("[valon] Zone: %s", v.Zone)
	log.Printf("[valon] etcd endpoints: %v", v.EtcdEndpoints)
	log.Printf("[valon] WireGuard interface: %s", v.WgInterface)
	log.Printf("[valon] DDNS listen: %s", v.DdnsListen)

	// Initialize etcd client
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   v.EtcdEndpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Printf("[valon] Failed to connect to etcd: %v", err)
		return err
	}
	v.etcdClient = cli

	// Test etcd connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = cli.Get(ctx, "/valon/health")
	if err != nil {
		log.Printf("[valon] Warning: etcd connection test failed: %v", err)
		// Don't fail initialization - etcd might be empty
	} else {
		log.Printf("[valon] etcd connection successful")
	}

	// TODO: Initialize WireGuard monitor
	// TODO: Start DDNS HTTP server

	log.Printf("[valon] Plugin initialized successfully")
	return nil
}

// Ready implements the ready.Readiness interface.
func (v Valon) Ready() bool {
	// TODO: Check etcd connection, WireGuard interface, etc.
	return true
}
