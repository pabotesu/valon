package valon

import (
	"encoding/base64"
	"log"
	"net"
	"time"

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
