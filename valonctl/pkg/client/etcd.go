package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	client       *clientv3.Client
	ddnsEndpoint string // DDNS API endpoint (e.g., "http://localhost:8053")
}

// PeerInfo represents a peer's information stored in etcd
type PeerInfo struct {
	Pubkey      string // WireGuard public key (base64)
	IP          string // WireGuard IP address
	Alias       string // User-friendly alias name
	Endpoint    string // Last known endpoint (IP:port or 0.0.0.0:0 for offline) - deprecated, use LANEndpoint/NATEndpoint
	LANEndpoint string // LAN endpoint from DDNS registration
	NATEndpoint string // NAT endpoint from WireGuard observation
	LastSeen    time.Time
}

// NewEtcdClient creates a new etcd client from configuration
func NewEtcdClient(cfg *config.EtcdConfig, ddnsCfg *config.DDNSConfig) (*EtcdClient, error) {
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

	ddnsEndpoint := ""
	if ddnsCfg != nil {
		ddnsEndpoint = ddnsCfg.APIURL
	}

	return &EtcdClient{
		client:       client,
		ddnsEndpoint: ddnsEndpoint,
	}, nil
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

	// Call DDNS API to delete from cache (if endpoint configured)
	if e.ddnsEndpoint != "" {
		if err := e.callDDNSDelete(ctx, pubkey); err != nil {
			log.Printf("Warning: Failed to call DDNS delete API: %v", err)
			// Continue anyway - we'll delete from etcd
		}
	}

	// Delete peer info and alias reference from etcd
	delResp, err := e.client.Delete(ctx, peerPrefix, clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("failed to delete peer: %w", err)
	}
	log.Printf("Deleted %d peer keys with prefix %s", delResp.Deleted, peerPrefix)

	delResp, err = e.client.Delete(ctx, aliasKey)
	if err != nil {
		return fmt.Errorf("failed to delete alias: %w", err)
	}
	log.Printf("Deleted %d alias keys: %s", delResp.Deleted, aliasKey)

	return nil
}

// callDDNSDelete calls the DDNS API to delete a peer from CoreDNS cache
func (e *EtcdClient) callDDNSDelete(ctx context.Context, pubkey string) error {
	url := fmt.Sprintf("%s/api/endpoint/delete", e.ddnsEndpoint)

	reqBody := map[string]string{"pubkey": pubkey}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("DDNS API returned status %d", resp.StatusCode)
	}

	log.Printf("Successfully called DDNS delete API for peer %s", pubkey)
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
		// Remove prefix to get: <pubkey>/field or <pubkey>/endpoints/type
		relKey := strings.TrimPrefix(keyStr, prefix)

		// Find pubkey by looking for known field patterns
		// Known fields: wg_ip, ip, alias, endpoint, endpoints/, last_seen
		var pubkey, fieldPath string

		if idx := strings.Index(relKey, "/wg_ip"); idx != -1 {
			pubkey = relKey[:idx]
			fieldPath = relKey[idx+1:]
		} else if idx := strings.Index(relKey, "/ip"); idx != -1 && !strings.Contains(relKey[idx:], "/wg_ip") {
			pubkey = relKey[:idx]
			fieldPath = relKey[idx+1:]
		} else if idx := strings.Index(relKey, "/alias"); idx != -1 {
			pubkey = relKey[:idx]
			fieldPath = relKey[idx+1:]
		} else if idx := strings.Index(relKey, "/endpoint"); idx != -1 {
			pubkey = relKey[:idx]
			fieldPath = relKey[idx+1:]
		} else if idx := strings.Index(relKey, "/last_seen"); idx != -1 {
			pubkey = relKey[:idx]
			fieldPath = relKey[idx+1:]
		} else {
			continue
		}

		if _, exists := peerMap[pubkey]; !exists {
			peerMap[pubkey] = &PeerInfo{
				Pubkey: pubkey, // Set pubkey from etcd key
			}
		}

		// Parse field path
		parts := strings.Split(fieldPath, "/")
		if len(parts) == 0 {
			continue
		}

		switch parts[0] {
		case "wg_ip", "ip": // Support both wg_ip and ip (legacy)
			peerMap[pubkey].IP = string(kv.Value)
		case "alias":
			peerMap[pubkey].Alias = string(kv.Value)
		case "endpoint":
			peerMap[pubkey].Endpoint = string(kv.Value)
		case "endpoints":
			if len(parts) >= 2 {
				endpointType := parts[1]
				if endpointType == "lan" {
					peerMap[pubkey].LANEndpoint = string(kv.Value)
				} else if endpointType == "nated" {
					peerMap[pubkey].NATEndpoint = string(kv.Value)
				}
			}
			// Also store in legacy Endpoint field for backward compatibility
			if peerMap[pubkey].Endpoint == "" {
				peerMap[pubkey].Endpoint = string(kv.Value)
			}
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
