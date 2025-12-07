package valon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

// DDNSEndpointRequest represents the request body for endpoint registration.
type DDNSEndpointRequest struct {
	PubKey      string `json:"pubkey"`       // WireGuard public key (Base64)
	LANEndpoint string `json:"lan_endpoint"` // LAN endpoint (IP:PORT)
	Alias       string `json:"alias"`        // Optional: CNAME alias (e.g., "alice-macbook")
}

// DDNSResponse represents the API response.
type DDNSResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// startDDNSServer starts the HTTP API server for DDNS.
func (v *Valon) startDDNSServer() {
	if v.DdnsListen == "" {
		log.Printf("[valon] DDNS API disabled (no listen address configured)")
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/endpoint", v.handleEndpointUpdate)
	mux.HandleFunc("/api/endpoint/delete", v.handleEndpointDelete)
	mux.HandleFunc("/health", v.handleHealth)

	server := &http.Server{
		Addr:         v.DdnsListen,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("[valon] Starting DDNS API server on %s", v.DdnsListen)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[valon] DDNS API server error: %v", err)
		}
	}()
}

// handleEndpointUpdate handles POST /api/endpoint
// Registers LAN endpoint for a peer.
// If lan_endpoint is "0.0.0.0:0", it marks the peer as offline.
func (v *Valon) handleEndpointUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		v.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req DDNSEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		v.sendError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	// Validate pubkey
	if req.PubKey == "" {
		v.sendError(w, http.StatusBadRequest, "pubkey is required")
		return
	}

	// Access control: verify source IP
	clientIP := extractClientIP(r)
	if !v.isAuthorized(clientIP, req.PubKey) {
		v.sendError(w, http.StatusForbidden, "Not authorized to modify this peer")
		return
	}

	// Validate LAN endpoint format (allow "0.0.0.0:0" for offline, or empty for removal)
	if req.LANEndpoint != "" && req.LANEndpoint != "0.0.0.0:0" {
		if _, _, err := net.SplitHostPort(req.LANEndpoint); err != nil {
			v.sendError(w, http.StatusBadRequest, "Invalid lan_endpoint format (expected IP:PORT)")
			return
		}
	}

	// Update cache
	v.cache.Update(req.PubKey, func(p *PeerInfo) {
		p.PubKey = req.PubKey
		if p.LANEndpoint != req.LANEndpoint {
			if req.LANEndpoint == "0.0.0.0:0" || req.LANEndpoint == "" {
				// Offline: remove LAN endpoint
				p.LANEndpoint = ""
			} else {
				// Online: update LAN endpoint
				p.LANEndpoint = req.LANEndpoint
			}
			p.dirty = true
		}
		p.UpdatedAt = time.Now()
	})

	if req.LANEndpoint == "0.0.0.0:0" || req.LANEndpoint == "" {
		log.Printf("[valon] DDNS: Peer %s went offline (LAN endpoint removed)", req.PubKey)
	} else {
		log.Printf("[valon] DDNS: Updated LAN endpoint for %s: %s", req.PubKey, req.LANEndpoint)
	}

	// Register alias if provided
	if req.Alias != "" {
		dnsLabel, err := pubkeyToDnsLabel(req.PubKey)
		if err != nil {
			log.Printf("[valon] DDNS: Failed to convert pubkey to DNS label: %v", err)
		} else {
			ctx := r.Context()

			// Write alias mapping: alias -> Base64 pubkey (not base32 label)
			aliasKey := fmt.Sprintf("/valon/aliases/%s", req.Alias)
			_, err = v.etcdClient.Put(ctx, aliasKey, req.PubKey)
			if err != nil {
				log.Printf("[valon] DDNS: Failed to register alias to etcd: %v", err)
			} else {
				log.Printf("[valon] DDNS: Registered alias %s -> %s (DNS label: %s)", req.Alias, req.PubKey[:20]+"...", dnsLabel)
			}

			// Write reverse mapping: pubkey -> alias (for management)
			peerAliasKey := fmt.Sprintf("/valon/peers/%s/alias", req.PubKey)
			_, err = v.etcdClient.Put(ctx, peerAliasKey, req.Alias)
			if err != nil {
				log.Printf("[valon] DDNS: Failed to write peer alias to etcd: %v", err)
			}
		}
	}

	v.sendSuccess(w, "Endpoint updated successfully")
}

// handleEndpointDelete handles DELETE /api/endpoint/delete
// Removes peer from cache and deletes alias from etcd.
// Only Discovery Role can delete peers (not peers themselves).
func (v *Valon) handleEndpointDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		v.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		PubKey string `json:"pubkey"` // WireGuard public key (Base64)
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		v.sendError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	// Validate pubkey
	if req.PubKey == "" {
		v.sendError(w, http.StatusBadRequest, "pubkey is required")
		return
	}

	// Access control: Only Discovery Role can delete peers
	clientIP := extractClientIP(r)
	if clientIP != v.selfWgIP {
		log.Printf("[valon] DDNS: Delete rejected - only Discovery Role allowed (clientIP=%s)", clientIP)
		v.sendError(w, http.StatusForbidden, "Only Discovery Role can delete peers")
		return
	}

	// Delete from cache
	v.cache.Delete(req.PubKey)

	// Delete alias from etcd
	ctx := context.Background()
	aliasKey := fmt.Sprintf("/valon/aliases/%s", req.PubKey)
	_, err := v.etcdClient.Delete(ctx, aliasKey)
	if err != nil {
		log.Printf("[valon] DDNS: Failed to delete alias for %s: %v", req.PubKey, err)
	}

	log.Printf("[valon] DDNS: Peer %s deleted from cache and alias removed", req.PubKey)
	v.sendSuccess(w, "Endpoint deleted successfully")
}

// extractClientIP extracts the client IP address from the HTTP request.
func extractClientIP(r *http.Request) string {
	// Try X-Forwarded-For header first (if behind proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list
		if idx := len(xff); idx > 0 {
			if commaIdx := 0; commaIdx < idx {
				for i, c := range xff {
					if c == ',' {
						commaIdx = i
						break
					}
				}
				if commaIdx > 0 {
					return xff[:commaIdx]
				}
			}
			return xff
		}
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isAuthorized checks if the client IP is authorized to modify the specified peer.
// Returns true if:
// 1. Client is Discovery Role (self.wgIP)
// 2. Client is the peer itself (clientIP matches peer's WgIP)
func (v *Valon) isAuthorized(clientIP, targetPubKey string) bool {
	// Discovery Role can manage all peers
	if clientIP == v.selfWgIP {
		log.Printf("[valon] DDNS: Authorization granted for Discovery Role (%s)", clientIP)
		return true
	}

	// Check if client is the peer itself
	peer := v.cache.Get(targetPubKey)
	if peer != nil && peer.WgIP == clientIP {
		log.Printf("[valon] DDNS: Authorization granted for peer itself (%s)", clientIP)
		return true
	}

	log.Printf("[valon] DDNS: Authorization denied - clientIP=%s, targetPubKey=%s", clientIP, targetPubKey[:16]+"...")
	return false
}

// handleHealth handles GET /health
func (v *Valon) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "ok",
		"peers_count": v.cache.Count(),
	})
}

// sendSuccess sends a successful JSON response.
func (v *Valon) sendSuccess(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(DDNSResponse{
		Success: true,
		Message: message,
	})
}

// sendError sends an error JSON response.
func (v *Valon) sendError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(DDNSResponse{
		Success: false,
		Message: message,
	})
}
