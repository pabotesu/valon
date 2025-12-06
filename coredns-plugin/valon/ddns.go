package valon

import (
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

	// Validate LAN endpoint format
	if req.LANEndpoint != "" {
		if _, _, err := net.SplitHostPort(req.LANEndpoint); err != nil {
			v.sendError(w, http.StatusBadRequest, "Invalid lan_endpoint format (expected IP:PORT)")
			return
		}
	}

	// Update cache
	v.cache.Update(req.PubKey, func(p *PeerInfo) {
		p.PubKey = req.PubKey
		if req.LANEndpoint != "" && p.LANEndpoint != req.LANEndpoint {
			p.LANEndpoint = req.LANEndpoint
			p.dirty = true
		}
		p.UpdatedAt = time.Now()
	})

	log.Printf("[valon] DDNS: Updated LAN endpoint for %s: %s", req.PubKey, req.LANEndpoint)

	// Register alias if provided
	if req.Alias != "" {
		dnsLabel, err := pubkeyToDnsLabel(req.PubKey)
		if err != nil {
			log.Printf("[valon] DDNS: Failed to convert pubkey to DNS label: %v", err)
		} else {
			ctx := r.Context()

			// Write CNAME alias mapping: alias -> Base32 label
			aliasKey := fmt.Sprintf("/valon/aliases/%s", req.Alias)
			_, err = v.etcdClient.Put(ctx, aliasKey, dnsLabel)
			if err != nil {
				log.Printf("[valon] DDNS: Failed to register alias to etcd: %v", err)
			} else {
				log.Printf("[valon] DDNS: Registered alias %s -> %s", req.Alias, dnsLabel)
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
