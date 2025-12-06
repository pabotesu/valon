package valon

import (
	"bufio"
	"bytes"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// startWgMonitor starts the WireGuard monitoring loop.
// It polls `wg show <interface> dump` at the configured interval
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

// pollWireGuard executes `wg show <interface> dump` and updates cache.
func (v *Valon) pollWireGuard() {
	cmd := exec.Command("wg", "show", v.WgInterface, "dump")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[valon] wg show dump failed: %v", err)
		return
	}

	v.parseWgDump(output)
}

// parseWgDump parses the output of `wg show <interface> dump`.
//
// Format:
// Line 1 (interface): private-key  public-key  listen-port  fwmark
// Line 2+ (peers):    public-key  preshared-key  endpoint  allowed-ips  latest-handshake  transfer-rx  transfer-tx  persistent-keepalive
//
// Example:
// (hidden)	ABC123...	51820	off
// DEF456...	(none)	192.168.1.100:51820	10.0.0.5/32	1701234567	1024	2048	0
func (v *Valon) parseWgDump(output []byte) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	lineNum := 0

	for scanner.Scan() {
		line := scanner.Text()
		lineNum++

		// Skip interface line (line 1)
		if lineNum == 1 {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 8 {
			log.Printf("[valon] Invalid wg dump line: %s", line)
			continue
		}

		pubkey := fields[0]
		endpoint := fields[2]
		allowedIPs := fields[3]
		handshakeStr := fields[4]

		// Parse last handshake timestamp (Unix timestamp)
		var lastHandshake time.Time
		if handshakeStr != "0" {
			if ts, err := strconv.ParseInt(handshakeStr, 10, 64); err == nil {
				lastHandshake = time.Unix(ts, 0)
			}
		}

		// Extract WireGuard overlay IP from allowed-ips
		// Format: "10.0.0.5/32" or "10.0.0.5/32,10.0.1.0/24"
		wgIP := v.extractFirstIP(allowedIPs)

		// Update cache
		v.cache.Update(pubkey, func(p *PeerInfo) {
			p.PubKey = pubkey
			p.WgIP = wgIP
			p.LastHandshake = lastHandshake
			p.UpdatedAt = time.Now()

			// Update NAT endpoint from wg observation (no private IP check)
			if endpoint != "(none)" && p.NATEndpoint != endpoint {
				p.NATEndpoint = endpoint
				p.dirty = true
			}
		})
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[valon] Error parsing wg dump: %v", err)
	}
}

// extractFirstIP extracts the first IP address from allowed-ips.
// Input: "10.0.0.5/32" or "10.0.0.5/32,10.0.1.0/24"
// Output: "10.0.0.5"
func (v *Valon) extractFirstIP(allowedIPs string) string {
	if allowedIPs == "" {
		return ""
	}

	// Split by comma (multiple ranges)
	parts := strings.Split(allowedIPs, ",")
	if len(parts) == 0 {
		return ""
	}

	// Take first range and strip CIDR suffix
	first := strings.TrimSpace(parts[0])
	if idx := strings.Index(first, "/"); idx > 0 {
		return first[:idx]
	}

	return first
}
