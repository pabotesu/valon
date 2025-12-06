package valon

import (
	"sync"
	"time"
)

// PeerInfo represents cached information about a WireGuard peer.
type PeerInfo struct {
	PubKey        string    // Base64 WireGuard public key
	WgIP          string    // WireGuard IP address (e.g., "100.64.0.1")
	LANEndpoint   string    // LAN endpoint (e.g., "192.168.1.100:51820") - from DDNS API
	NATEndpoint   string    // NAT endpoint (e.g., "203.0.113.1:51820") - from wg show observation
	LastHandshake time.Time // Last WireGuard handshake time
	UpdatedAt     time.Time // Last update time
	dirty         bool      // Needs etcd sync
}

// PeerCache is a thread-safe in-memory cache for peer information.
type PeerCache struct {
	mu    sync.RWMutex
	peers map[string]*PeerInfo // key: Base64 public key
}

// NewPeerCache creates a new peer cache.
func NewPeerCache() *PeerCache {
	return &PeerCache{
		peers: make(map[string]*PeerInfo),
	}
}

// Get retrieves peer info by public key (read lock).
func (c *PeerCache) Get(pubkey string) *PeerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.peers[pubkey]
}

// Set stores peer info (write lock).
func (c *PeerCache) Set(pubkey string, info *PeerInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	info.UpdatedAt = time.Now()
	c.peers[pubkey] = info
}

// Update updates an existing peer with a function (write lock).
// If the peer doesn't exist, the function is not called.
func (c *PeerCache) Update(pubkey string, fn func(*PeerInfo)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if peer := c.peers[pubkey]; peer != nil {
		fn(peer)
		peer.UpdatedAt = time.Now()
		peer.dirty = true
	}
}

// GetAll returns a copy of all peers (read lock).
func (c *PeerCache) GetAll() map[string]*PeerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	copy := make(map[string]*PeerInfo, len(c.peers))
	for k, v := range c.peers {
		// Shallow copy is sufficient
		copy[k] = v
	}
	return copy
}

// Delete removes a peer from the cache.
func (c *PeerCache) Delete(pubkey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.peers, pubkey)
}

// Count returns the number of peers in the cache.
func (c *PeerCache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.peers)
}
