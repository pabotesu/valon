package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pabotesu/valon/valonctl/pkg/config"
)

const (
	// EtcdKeyPrefix is the root prefix for all VALON keys in etcd
	EtcdKeyPrefix = "/valon"

	// DefaultDialTimeout is the default timeout for etcd connection
	DefaultDialTimeout = 5 * time.Second
)

// EtcdClient wraps etcd client for VALON-specific operations
type EtcdClient struct {
	client *clientv3.Client
}

// PeerInfo represents a peer's information stored in etcd
type PeerInfo struct {
	Pubkey   string // WireGuard public key (base64)
	IP       string // WireGuard IP address
	Alias    string // User-friendly alias name
	Endpoint string // Last known endpoint (IP:port or 0.0.0.0:0 for offline)
	LastSeen time.Time
}

// NewEtcdClient creates a new etcd client from configuration
func NewEtcdClient(cfg *config.EtcdConfig) (*EtcdClient, error) {
	clientCfg := clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: DefaultDialTimeout,
	}

	// Configure TLS if specified
	if cfg.TLS != nil {
		tlsConfig, err := loadTLSConfig(cfg.TLS)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS config: %w", err)
		}
		clientCfg.TLS = tlsConfig
	}

	client, err := clientv3.New(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	return &EtcdClient{client: client}, nil
}

// Close closes the etcd client connection
func (e *EtcdClient) Close() error {
	return e.client.Close()
}

// Ping checks if etcd is reachable
func (e *EtcdClient) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, err := e.client.Get(ctx, EtcdKeyPrefix, clientv3.WithLimit(1))
	return err
}

// AddPeer registers a new peer in etcd
func (e *EtcdClient) AddPeer(ctx context.Context, peer *PeerInfo) error {
	if peer.Pubkey == "" {
		return fmt.Errorf("pubkey is required")
	}
	if peer.IP == "" {
		return fmt.Errorf("IP is required")
	}
	if peer.Alias == "" {
		return fmt.Errorf("alias is required")
	}

	// Use base64 pubkey directly as etcd key
	peerPrefix := path.Join(EtcdKeyPrefix, "peers", peer.Pubkey)
	aliasKey := path.Join(EtcdKeyPrefix, "aliases", peer.Alias)

	// Use transaction to ensure atomic dual write
	txn := e.client.Txn(ctx).If(
		// Check alias doesn't already exist
		clientv3.Compare(clientv3.Version(aliasKey), "=", 0),
	).Then(
		// Write peer info (wg_ip is the primary field)
		clientv3.OpPut(path.Join(peerPrefix, "wg_ip"), peer.IP),
		clientv3.OpPut(path.Join(peerPrefix, "alias"), peer.Alias),
		// Write alias reference
		clientv3.OpPut(aliasKey, peer.Pubkey),
	)

	resp, err := txn.Commit()
	if err != nil {
		return fmt.Errorf("failed to add peer: %w", err)
	}

	if !resp.Succeeded {
		return fmt.Errorf("alias %q already exists", peer.Alias)
	}

	return nil
}

// RemovePeer removes a peer from etcd by pubkey or alias
func (e *EtcdClient) RemovePeer(ctx context.Context, pubkeyOrAlias string) error {
	// Try to detect if it's a pubkey (base64) or alias
	var pubkey string
	var alias string

	if strings.Contains(pubkeyOrAlias, "+") || strings.Contains(pubkeyOrAlias, "/") || strings.HasSuffix(pubkeyOrAlias, "=") {
		// Looks like a base64 pubkey
		pubkey = pubkeyOrAlias

		// Get alias from etcd
		peerPrefix := path.Join(EtcdKeyPrefix, "peers", pubkey)
		resp, err := e.client.Get(ctx, path.Join(peerPrefix, "alias"))
		if err != nil {
			return fmt.Errorf("failed to get alias: %w", err)
		}
		if len(resp.Kvs) == 0 {
			return fmt.Errorf("peer not found")
		}
		alias = string(resp.Kvs[0].Value)
	} else {
		// Treat as alias
		alias = pubkeyOrAlias

		// Get pubkey from alias
		aliasKey := path.Join(EtcdKeyPrefix, "aliases", alias)
		resp, err := e.client.Get(ctx, aliasKey)
		if err != nil {
			return fmt.Errorf("failed to get pubkey: %w", err)
		}
		if len(resp.Kvs) == 0 {
			return fmt.Errorf("alias %q not found", alias)
		}

		pubkey = string(resp.Kvs[0].Value)
	}

	peerPrefix := path.Join(EtcdKeyPrefix, "peers", pubkey)
	aliasKey := path.Join(EtcdKeyPrefix, "aliases", alias)

	// Delete peer info and alias reference
	_, err := e.client.Delete(ctx, peerPrefix, clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("failed to delete peer: %w", err)
	}

	_, err = e.client.Delete(ctx, aliasKey)
	if err != nil {
		return fmt.Errorf("failed to delete alias: %w", err)
	}

	return nil
}

// ListPeers retrieves all registered peers from etcd
func (e *EtcdClient) ListPeers(ctx context.Context) ([]*PeerInfo, error) {
	prefix := path.Join(EtcdKeyPrefix, "peers") + "/"
	resp, err := e.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to list peers: %w", err)
	}

	// Group keys by label (peer)
	peerMap := make(map[string]*PeerInfo)

	for _, kv := range resp.Kvs {
		keyStr := string(kv.Key)
		// Remove prefix to get: <pubkey>/field
		relKey := strings.TrimPrefix(keyStr, prefix)
		parts := strings.SplitN(relKey, "/", 2)
		if len(parts) != 2 {
			continue
		}

		pubkey := parts[0]
		field := parts[1]

		if _, exists := peerMap[pubkey]; !exists {
			peerMap[pubkey] = &PeerInfo{
				Pubkey: pubkey, // Set pubkey from etcd key
			}
		}

		switch field {
		case "wg_ip", "ip": // Support both wg_ip and ip (legacy)
			peerMap[pubkey].IP = string(kv.Value)
		case "alias":
			peerMap[pubkey].Alias = string(kv.Value)
		case "endpoint":
			peerMap[pubkey].Endpoint = string(kv.Value)
		case "last_seen":
			if t, err := time.Parse(time.RFC3339, string(kv.Value)); err == nil {
				peerMap[pubkey].LastSeen = t
			}
		}
	}

	// Convert map to slice
	peers := make([]*PeerInfo, 0, len(peerMap))
	for _, peer := range peerMap {
		peers = append(peers, peer)
	}

	return peers, nil
}

// loadTLSConfig creates a TLS configuration from certificate paths
func loadTLSConfig(cfg *config.TLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{}

	// Load CA certificate
	if cfg.CACert != "" {
		caCert, err := os.ReadFile(cfg.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to append CA cert")
		}
		tlsConfig.RootCAs = caCertPool
	}

	// Load client certificate and key
	if cfg.ClientCert != "" && cfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCert, cfg.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client cert/key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}
