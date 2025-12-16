package client

import (
	"fmt"
	"net"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// WireGuardClient wraps wgctrl for WireGuard operations
type WireGuardClient struct {
	client *wgctrl.Client
}

// NewWireGuardClient creates a new WireGuard client
func NewWireGuardClient() (*WireGuardClient, error) {
	client, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create wgctrl client: %w", err)
	}

	return &WireGuardClient{client: client}, nil
}

// Close closes the WireGuard client
func (w *WireGuardClient) Close() error {
	return w.client.Close()
}

// GetPublicKey retrieves the public key for the specified interface
func (w *WireGuardClient) GetPublicKey(interfaceName string) (string, error) {
	device, err := w.client.Device(interfaceName)
	if err != nil {
		return "", fmt.Errorf("failed to get device %s: %w", interfaceName, err)
	}
	return device.PublicKey.String(), nil
}

// GetDevice retrieves device information for the specified interface
func (w *WireGuardClient) GetDevice(interfaceName string) (*wgtypes.Device, error) {
	device, err := w.client.Device(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get device %s: %w", interfaceName, err)
	}
	return device, nil
}

// AddPeer adds a new peer to the WireGuard interface
func (w *WireGuardClient) AddPeer(interfaceName string, pubkey string, allowedIP string) error {
	// Parse public key
	key, err := wgtypes.ParseKey(pubkey)
	if err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	// Parse allowed IP
	_, ipNet, err := net.ParseCIDR(allowedIP + "/32")
	if err != nil {
		return fmt.Errorf("invalid IP address: %w", err)
	}

	// Configure peer
	peerConfig := wgtypes.PeerConfig{
		PublicKey:         key,
		AllowedIPs:        []net.IPNet{*ipNet},
		ReplaceAllowedIPs: true,
	}

	cfg := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peerConfig},
	}

	if err := w.client.ConfigureDevice(interfaceName, cfg); err != nil {
		return fmt.Errorf("failed to add peer: %w", err)
	}

	return nil
}

// RemovePeer removes a peer from the WireGuard interface
func (w *WireGuardClient) RemovePeer(interfaceName string, pubkey string) error {
	// Parse public key
	key, err := wgtypes.ParseKey(pubkey)
	if err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	// Configure to remove peer
	peerConfig := wgtypes.PeerConfig{
		PublicKey: key,
		Remove:    true,
	}

	cfg := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peerConfig},
	}

	if err := w.client.ConfigureDevice(interfaceName, cfg); err != nil {
		return fmt.Errorf("failed to remove peer: %w", err)
	}

	return nil
}

// IsInterfaceUp checks if the WireGuard interface exists and is configured
func (w *WireGuardClient) IsInterfaceUp(interfaceName string) (bool, error) {
	_, err := w.client.Device(interfaceName)
	if err != nil {
		return false, nil // Interface doesn't exist or not accessible
	}
	return true, nil
}

// GetPeerCount returns the number of peers on the interface
func (w *WireGuardClient) GetPeerCount(interfaceName string) (int, error) {
	device, err := w.GetDevice(interfaceName)
	if err != nil {
		return 0, err
	}
	return len(device.Peers), nil
}
