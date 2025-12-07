package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// DDNSClient wraps HTTP client for CoreDNS DDNS API operations
type DDNSClient struct {
	baseURL string
	client  *http.Client
}

// NewDDNSClient creates a new DDNS API client
func NewDDNSClient(baseURL string) *DDNSClient {
	return &DDNSClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Ping checks if the DDNS API is reachable
func (d *DDNSClient) Ping(ctx context.Context) error {
	// Try to reach the base URL
	req, err := http.NewRequestWithContext(ctx, "GET", d.baseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("DDNS API unreachable: %w", err)
	}
	defer resp.Body.Close()

	// Accept any 2xx or 404 (endpoint might not exist but server is running)
	if resp.StatusCode >= 500 {
		return fmt.Errorf("DDNS API returned server error: %d", resp.StatusCode)
	}

	return nil
}
