package valon

import (
	"context"
	"fmt"
	"log"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// startEtcdSync starts the etcd synchronization loop.
// It periodically syncs dirty peers from memory cache to etcd.
func (v *Valon) startEtcdSync() {
	log.Printf("[valon] Starting etcd sync (interval: %v)", v.EtcdSyncInterval)

	ticker := time.NewTicker(v.EtcdSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			v.syncToEtcd()
		case <-v.stopCh:
			log.Printf("[valon] etcd sync stopped")
			return
		}
	}
}

// syncToEtcd synchronizes dirty peers to etcd.
func (v *Valon) syncToEtcd() {
	peers := v.cache.GetAll()
	dirtyCount := 0

	for pubkey, peerInfo := range peers {
		if !peerInfo.dirty {
			continue
		}

		if err := v.writePeerToEtcd(pubkey, peerInfo); err != nil {
			log.Printf("[valon] Failed to sync peer %s to etcd: %v", pubkey, err)
			continue
		}

		// Clear dirty flag after successful sync
		v.cache.Update(pubkey, func(p *PeerInfo) {
			p.dirty = false
		})

		dirtyCount++
	}

	if dirtyCount > 0 {
		log.Printf("[valon] Synced %d dirty peers to etcd", dirtyCount)
	}
}

// writePeerToEtcd writes a single peer's information to etcd.
func (v *Valon) writePeerToEtcd(pubkey string, peerInfo *PeerInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Prepare key-value pairs
	ops := []clientv3.Op{}

	// Write WireGuard IP
	if peerInfo.WgIP != "" {
		key := fmt.Sprintf("/valon/peers/%s/wg_ip", pubkey)
		ops = append(ops, clientv3.OpPut(key, peerInfo.WgIP))
	}

	// Write LAN endpoint (from DDNS API)
	if peerInfo.LANEndpoint != "" {
		key := fmt.Sprintf("/valon/peers/%s/endpoints/lan", pubkey)
		ops = append(ops, clientv3.OpPut(key, peerInfo.LANEndpoint))
	}

	// Write NAT endpoint (from wg show observation)
	if peerInfo.NATEndpoint != "" {
		key := fmt.Sprintf("/valon/peers/%s/endpoints/nated", pubkey)
		ops = append(ops, clientv3.OpPut(key, peerInfo.NATEndpoint))
	}

	// Execute transaction
	if len(ops) > 0 {
		_, err := v.etcdClient.Txn(ctx).Then(ops...).Commit()
		if err != nil {
			return fmt.Errorf("etcd transaction failed: %w", err)
		}
	}

	return nil
}
