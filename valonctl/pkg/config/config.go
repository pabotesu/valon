package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigPath is the default location for valonctl configuration
	DefaultConfigPath = "/etc/valon/valonctl.yml"
)

// Config represents the valonctl configuration file structure
type Config struct {
	WireGuard WireGuardConfig `yaml:"wireguard"`
	Etcd      EtcdConfig      `yaml:"etcd"`
	DDNS      DDNSConfig      `yaml:"ddns"`
}

// WireGuardConfig holds WireGuard interface configuration
type WireGuardConfig struct {
	Interface string `yaml:"interface"` // e.g., "wg0"
	IP        string `yaml:"ip"`        // Discovery Role's WireGuard IP (e.g., "100.100.0.1")
	Endpoint  string `yaml:"endpoint"`  // Discovery Role's public endpoint (e.g., "192.168.1.100:51820")
	Network   string `yaml:"network"`   // WireGuard network CIDR (e.g., "100.100.0.0/24") for IP auto-allocation
}

// EtcdConfig holds etcd connection settings
type EtcdConfig struct {
	Endpoints []string   `yaml:"endpoints"` // e.g., ["https://localhost:2379"]
	TLS       *TLSConfig `yaml:"tls,omitempty"`
}

// TLSConfig holds TLS certificate paths for etcd connection
type TLSConfig struct {
	CACert     string `yaml:"ca_cert,omitempty"`
	ClientCert string `yaml:"client_cert,omitempty"`
	ClientKey  string `yaml:"client_key,omitempty"`
}

// DDNSConfig holds CoreDNS DDNS API settings
type DDNSConfig struct {
	APIURL string `yaml:"api_url"` // e.g., "http://localhost:8053"
}

// Load reads and parses the configuration file from the specified path.
// If configPath is empty, it uses DefaultConfigPath.
func Load(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = DefaultConfigPath
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	// Validate required fields
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// Validate checks if the configuration has all required fields
func (c *Config) Validate() error {
	if c.WireGuard.Interface == "" {
		return fmt.Errorf("wireguard.interface is required")
	}

	if c.WireGuard.IP == "" {
		return fmt.Errorf("wireguard.ip is required")
	}

	if c.WireGuard.Network == "" {
		return fmt.Errorf("wireguard.network is required")
	}

	if len(c.Etcd.Endpoints) == 0 {
		return fmt.Errorf("etcd.endpoints is required")
	}

	if c.DDNS.APIURL == "" {
		return fmt.Errorf("ddns.api_url is required")
	}

	return nil
}
