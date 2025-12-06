package valon

import (
	"log"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() {
	// Register the VALON plugin with CoreDNS
	plugin.Register("valon", setup)
}

// setup configures the VALON plugin from the Corefile.
func setup(c *caddy.Controller) error {
	v := &Valon{
		EtcdEndpoints: []string{},
		WgInterface:   "wg0",             // default
		DdnsListen:    "127.0.0.1:8080",  // default
		Zone:          "valon.internal.", // default
	}

	// Parse Corefile configuration
	for c.Next() {
		// Get zone from the server block
		if c.Val() != "valon" {
			v.Zone = c.Val()
		}

		// Parse block directives
		for c.NextBlock() {
			switch c.Val() {
			case "etcd_endpoints":
				args := c.RemainingArgs()
				if len(args) == 0 {
					return c.ArgErr()
				}
				v.EtcdEndpoints = append(v.EtcdEndpoints, args...)

			case "wg_interface":
				if !c.NextArg() {
					return c.ArgErr()
				}
				v.WgInterface = c.Val()

			case "ddns_listen":
				if !c.NextArg() {
					return c.ArgErr()
				}
				v.DdnsListen = c.Val()

			case "wg_poll_interval":
				if !c.NextArg() {
					return c.ArgErr()
				}
				duration, err := time.ParseDuration(c.Val())
				if err != nil {
					return c.Errf("invalid wg_poll_interval: %v", err)
				}
				v.WgPollInterval = duration

			case "etcd_sync_interval":
				if !c.NextArg() {
					return c.ArgErr()
				}
				duration, err := time.ParseDuration(c.Val())
				if err != nil {
					return c.Errf("invalid etcd_sync_interval: %v", err)
				}
				v.EtcdSyncInterval = duration

			default:
				return c.Errf("unknown property '%s'", c.Val())
			}
		}
	}

	// Initialize the plugin
	if err := v.Init(); err != nil {
		return plugin.Error("valon", err)
	}

	// Add the plugin to the server's plugin chain
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		v.Next = next
		return v
	})

	log.Printf("[valon] Plugin setup complete")
	return nil
}
