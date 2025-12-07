package valon

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// startWgMonitor starts the WireGuard monitoring loop.
// It uses wgctrl library to query WireGuard interface state
// and updates the memory cache with peer information.
func (v *Valon) startWgMonitor() {
	log.Printf("[valon] Starting WireGuard monitor (interval: %v)", v.WgPollInterval)

	ticker := time.NewTicker(v.WgPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			v.pollWireGuard()
		case <-v.stopCh:
			log.Printf("[valon] WireGuard monitor stopped")
			return
		}
	}
}

// pollWireGuard queries WireGuard interface using wgctrl and updates cache.
func (v *Valon) pollWireGuard() {
	client, err := wgctrl.New()
	if err != nil {
		log.Printf("[valon] Failed to create wgctrl client: %v", err)
		return
	}
	defer client.Close()

	device, err := client.Device(v.WgInterface)
	if err != nil {
		log.Printf("[valon] Failed to get WireGuard device %s: %v", v.WgInterface, err)
		return
	}

	// Process each peer
	for _, peer := range device.Peers {
		v.processPeer(&peer)
	}
}

// processPeer processes a single WireGuard peer and updates cache.
func (v *Valon) processPeer(peer *wgtypes.Peer) {
	// Convert public key to Base64 (standard WireGuard format)
	pubkey := base64.StdEncoding.EncodeToString(peer.PublicKey[:])

	// Extract endpoint (NAT endpoint observed by WireGuard)
	var endpoint string
	if peer.Endpoint != nil {
		endpoint = peer.Endpoint.String()
	}

	// Extract WireGuard overlay IP from allowed IPs
	wgIP := v.extractWgIP(peer.AllowedIPs)

	// Check if peer exists in cache
	existing := v.cache.Get(pubkey)
	if existing == nil {
		// New peer detected but not in cache
		// Try to load from etcd (in case it was added via valonctl while CoreDNS was running)
		if v.loadPeerFromEtcd(pubkey, wgIP) {
			log.Printf("[valon] Loaded peer from etcd into cache: %s (wgIP: %s)", pubkey[:16]+"...", wgIP)
			// Now update with NAT endpoint
			existing = v.cache.Get(pubkey)
		} else {
			// Not in etcd either - awaiting DDNS registration
			if peer.LastHandshakeTime.After(time.Now().Add(-30 * time.Second)) {
				log.Printf("[valon] New peer detected: %s (wgIP: %s, endpoint: %s) - not in cache, awaiting DDNS registration",
					pubkey[:16]+"...", wgIP, endpoint)
			}
			return
		}
	}

	// Update cache
	v.cache.Update(pubkey, func(p *PeerInfo) {
		p.PubKey = pubkey
		if wgIP != "" {
			p.WgIP = wgIP
		}
		p.LastHandshake = peer.LastHandshakeTime
		p.UpdatedAt = time.Now()

		// Update NAT endpoint from wg observation
		if endpoint != "" && p.NATEndpoint != endpoint {
			p.NATEndpoint = endpoint
			p.dirty = true
		}
	})
}

// loadPeerFromEtcd attempts to load a peer from etcd and add to cache.
// Returns true if peer was found and loaded, false otherwise.
func (v *Valon) loadPeerFromEtcd(pubkey, wgIP string) bool {
	ctx := context.Background()
	peerPrefix := fmt.Sprintf("/valon/peers/%s/", pubkey)

	resp, err := v.etcdClient.Get(ctx, peerPrefix, clientv3.WithPrefix())
	if err != nil {
		log.Printf("[valon] Failed to query etcd for peer %s: %v", pubkey[:16]+"...", err)
		return false
	}

	if len(resp.Kvs) == 0 {
		return false // Not in etcd
	}

	// Parse peer data from etcd keys
	peer := &PeerInfo{
		PubKey: pubkey,
		WgIP:   wgIP,
	}

	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		value := string(kv.Value)

		if strings.HasSuffix(key, "/wg_ip") {
			peer.WgIP = value
		} else if strings.HasSuffix(key, "/endpoints/lan") {
			peer.LANEndpoint = value
		} else if strings.HasSuffix(key, "/endpoints/nated") {
			peer.NATEndpoint = value
		}
	}

	// Add to cache
	v.cache.Set(pubkey, peer)
	return true
}

// extractWgIP extracts the first IPv4 address from allowed IPs.
// This is typically the WireGuard overlay network IP.
func (v *Valon) extractWgIP(allowedIPs []net.IPNet) string {
	for _, ipNet := range allowedIPs {
		// Prefer IPv4 addresses
		if ip := ipNet.IP.To4(); ip != nil {
			return ip.String()
		}
	}
	return ""
}
