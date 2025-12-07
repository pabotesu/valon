// Package valon implements a CoreDNS plugin for VALON (Virtual Adaptive Logical Overlay Network).
//
// VALON provides DNS-SD based peer discovery for WireGuard overlay networks.
package valon

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/coredns/coredns/plugin"
	clientv3 "go.etcd.io/etcd/client/v3"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Valon is the main plugin structure.
type Valon struct {
	Next plugin.Handler // Next plugin in the chain

	// Configuration (from Corefile)
	EtcdEndpoints    []string      // etcd endpoints
	WgInterface      string        // WireGuard interface name
	DdnsListen       string        // DDNS API listen address
	WgPollInterval   time.Duration // WireGuard polling interval (default: 1s)
	EtcdSyncInterval time.Duration // etcd sync interval (default: 10s)

	// Zone
	Zone string // DNS zone (e.g., "valon.internal.")

	// Runtime
	etcdClient *clientv3.Client // etcd client
	cache      *PeerCache       // in-memory peer cache
	stopCh     chan struct{}    // stop signal for background goroutines
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

	// Initialize memory cache
	v.cache = NewPeerCache()
	log.Printf("[valon] Memory cache initialized")

	// Load initial data from etcd
	if err := v.loadFromEtcd(); err != nil {
		log.Printf("[valon] Warning: failed to load from etcd: %v", err)
	}

	// Restore WireGuard peers from etcd (for restart resilience)
	if err := v.restoreWireGuardPeers(); err != nil {
		log.Printf("[valon] Warning: failed to restore WireGuard peers: %v", err)
		// Continue anyway - peers will be re-added on next sync
	}

	// Register self (Discovery Role's own peer info)
	if err := v.registerSelf(); err != nil {
		log.Printf("[valon] Failed to register self: %v", err)
		return fmt.Errorf("failed to register self: %w", err)
	}

	// Set default intervals if not configured
	if v.WgPollInterval == 0 {
		v.WgPollInterval = 1 * time.Second
	}
	if v.EtcdSyncInterval == 0 {
		v.EtcdSyncInterval = 10 * time.Second
	}

	log.Printf("[valon] WireGuard poll interval: %v", v.WgPollInterval)
	log.Printf("[valon] etcd sync interval: %v", v.EtcdSyncInterval)

	// Initialize stop channel
	v.stopCh = make(chan struct{})

	// Start background monitors
	go v.startWgMonitor()
	go v.startEtcdSync()

	// Start DDNS HTTP server
	v.startDDNSServer()

	log.Printf("[valon] Plugin initialized successfully")
	return nil
}

// Ready implements the ready.Readiness interface.
func (v Valon) Ready() bool {
	// TODO: Check etcd connection, WireGuard interface, etc.
	return true
}

// loadFromEtcd loads all peer data from etcd into memory cache.
func (v *Valon) loadFromEtcd() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get all keys under /valon/peers/
	resp, err := v.etcdClient.Get(ctx, "/valon/peers/", clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("etcd get failed: %w", err)
	}

	if len(resp.Kvs) == 0 {
		log.Printf("[valon] No peers found in etcd")
		return nil
	}

	// Parse keys and group by pubkey
	peersByPubkey := make(map[string]*PeerInfo)

	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		value := string(kv.Value)

		// Parse key: /valon/peers/<pubkey>/wg_ip or /valon/peers/<pubkey>/endpoints/lan
		// Note: pubkey may contain "/" characters in base64 encoding
		relKey := strings.TrimPrefix(key, "/valon/peers/")

		// Find pubkey by looking for known field patterns
		// Known fields: wg_ip, alias, endpoints/
		var pubkey, fieldPath string

		if idx := strings.Index(relKey, "/wg_ip"); idx != -1 {
			pubkey = relKey[:idx]
			fieldPath = relKey[idx+1:]
		} else if idx := strings.Index(relKey, "/alias"); idx != -1 {
			pubkey = relKey[:idx]
			fieldPath = relKey[idx+1:]
		} else if idx := strings.Index(relKey, "/endpoints/"); idx != -1 {
			pubkey = relKey[:idx]
			fieldPath = relKey[idx+1:]
		} else {
			continue
		}

		if peersByPubkey[pubkey] == nil {
			peersByPubkey[pubkey] = &PeerInfo{
				PubKey: pubkey, // Set pubkey from etcd key
			}
		}

		// Parse field path (e.g., "wg_ip" or "endpoints/lan")
		fieldParts := strings.Split(fieldPath, "/")
		if len(fieldParts) == 0 {
			continue
		}

		switch fieldParts[0] {
		case "wg_ip":
			peersByPubkey[pubkey].WgIP = value
		case "endpoints":
			if len(fieldParts) >= 2 {
				endpointType := fieldParts[1]
				if endpointType == "lan" {
					peersByPubkey[pubkey].LANEndpoint = value
				} else if endpointType == "nated" {
					peersByPubkey[pubkey].NATEndpoint = value
				}
			}
		}
	}

	// Load into cache using pubkey as key
	loaded := 0
	for pubkey, peer := range peersByPubkey {
		log.Printf("[valon] Loading peer into cache: pubkey=%s, wg_ip=%s", pubkey[:min(len(pubkey), 20)]+"...", peer.WgIP)
		v.cache.Set(pubkey, peer)
		loaded++
	}

	log.Printf("[valon] Loaded %d peers from etcd into cache", loaded)
	return nil
}

// registerSelf registers this node's WireGuard peer information.
// It verifies WireGuard interface existence and extracts public key and IP.
// Returns error if WireGuard interface is not found (plugin initialization will fail).
func (v *Valon) registerSelf() error {
	if v.WgInterface == "" {
		return fmt.Errorf("WireGuard interface not configured")
	}

	// Check if WireGuard interface exists
	_, err := net.InterfaceByName(v.WgInterface)
	if err != nil {
		return fmt.Errorf("WireGuard interface %s not found: %w", v.WgInterface, err)
	}

	// Get own public key using: wg show <interface> public-key
	pubkey, err := v.getOwnPublicKey()
	if err != nil {
		return fmt.Errorf("failed to get public key: %w", err)
	}

	// Get own WireGuard IP
	wgIP, err := v.getOwnWireGuardIP()
	if err != nil {
		return fmt.Errorf("failed to get WireGuard IP: %w", err)
	}

	// Register to cache
	selfInfo := &PeerInfo{
		PubKey:    pubkey,
		WgIP:      wgIP,
		UpdatedAt: time.Now(),
		dirty:     true, // Needs sync to etcd
	}

	v.cache.Set(pubkey, selfInfo)
	log.Printf("[valon] Registered self: pubkey=%s, wgIP=%s", pubkey, wgIP)

	return nil
}

// getOwnPublicKey retrieves this node's WireGuard public key using wgctrl.
func (v *Valon) getOwnPublicKey() (string, error) {
	client, err := wgctrl.New()
	if err != nil {
		return "", fmt.Errorf("failed to create wgctrl client: %w", err)
	}
	defer client.Close()

	device, err := client.Device(v.WgInterface)
	if err != nil {
		return "", fmt.Errorf("failed to get WireGuard device: %w", err)
	}

	// Convert public key to Base64 (standard WireGuard format)
	pubkey := base64.StdEncoding.EncodeToString(device.PublicKey[:])
	return pubkey, nil
}

// getOwnWireGuardIP retrieves this node's WireGuard interface IP address.
func (v *Valon) getOwnWireGuardIP() (string, error) {
	iface, err := net.InterfaceByName(v.WgInterface)
	if err != nil {
		return "", err
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no IPv4 address found on interface %s", v.WgInterface)
}

// restoreWireGuardPeers restores all peers from etcd to WireGuard interface.
// This is called on plugin initialization to recover from restarts.
func (v *Valon) restoreWireGuardPeers() error {
	log.Printf("[valon] Restoring WireGuard peers from etcd...")

	// Get all peers from cache (already loaded from etcd)
	peers := v.cache.GetAll()
	if len(peers) == 0 {
		log.Printf("[valon] No peers to restore")
		return nil
	}

	// Create WireGuard client
	wgClient, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("failed to create WireGuard client: %w", err)
	}
	defer wgClient.Close()

	// Get current WireGuard device state
	device, err := wgClient.Device(v.WgInterface)
	if err != nil {
		return fmt.Errorf("failed to get WireGuard device: %w", err)
	}

	// Build map of existing peers
	existingPeers := make(map[string]bool)
	for _, peer := range device.Peers {
		pubkeyStr := base64.StdEncoding.EncodeToString(peer.PublicKey[:])
		existingPeers[pubkeyStr] = true
	}

	// Add missing peers to WireGuard
	restored := 0
	skipped := 0
	for _, peer := range peers {
		// Skip if peer already exists in WireGuard
		if existingPeers[peer.PubKey] {
			skipped++
			continue
		}

		// Parse WireGuard IP
		_, ipNet, err := net.ParseCIDR(peer.WgIP + "/32")
		if err != nil {
			log.Printf("[valon] Warning: invalid WgIP for peer %s: %v", peer.PubKey, err)
			continue
		}

		// Decode public key
		pubkeyBytes, err := base64.StdEncoding.DecodeString(peer.PubKey)
		if err != nil {
			log.Printf("[valon] Warning: invalid pubkey for peer %s: %v", peer.PubKey, err)
			continue
		}

		pubkey, err := wgtypes.NewKey(pubkeyBytes)
		if err != nil {
			log.Printf("[valon] Warning: failed to create key for peer %s: %v", peer.PubKey, err)
			continue
		}

		// Configure peer
		peerConfig := wgtypes.PeerConfig{
			PublicKey:  pubkey,
			AllowedIPs: []net.IPNet{*ipNet},
		}

		// Apply configuration
		config := wgtypes.Config{
			Peers: []wgtypes.PeerConfig{peerConfig},
		}

		if err := wgClient.ConfigureDevice(v.WgInterface, config); err != nil {
			log.Printf("[valon] Warning: failed to restore peer %s: %v", peer.PubKey, err)
			continue
		}

		restored++
		log.Printf("[valon] Restored peer: %s (IP: %s)", peer.PubKey[:16]+"...", peer.WgIP)
	}

	log.Printf("[valon] WireGuard peer restoration complete: %d restored, %d already existed", restored, skipped)
	return nil
}
