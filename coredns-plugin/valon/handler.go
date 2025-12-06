package valon

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

// ServeDNS implements the plugin.Handler interface.
func (v Valon) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}

	// Check if the query is for our zone
	if !strings.HasSuffix(state.Name(), v.Zone) {
		// Not our zone, pass to next plugin
		return plugin.NextOrFailure(v.Name(), v.Next, ctx, w, r)
	}

	log.Printf("[valon] DNS query: %s %s", dns.TypeToString[state.QType()], state.Name())

	// Handle different query types
	switch state.QType() {
	case dns.TypeA:
		return v.handleA(ctx, w, r, state)
	case dns.TypeSRV:
		return v.handleSRV(ctx, w, r, state)
	default:
		// Unsupported query type, return NXDOMAIN
		return v.nxdomain(w, r)
	}
}

// handleA handles A record queries.
// Queries etcd for WireGuard IP address.
func (v Valon) handleA(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, state request.Request) (int, error) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	// Extract label from query name
	// Format: <base32-label>.valon.internal. or lan.<base32-label>.valon.internal. or nated.<base32-label>.valon.internal.
	name := strings.TrimSuffix(state.Name(), v.Zone)
	name = strings.TrimSuffix(name, ".")

	var dnsLabel string
	var isEndpoint bool
	var endpointType string

	if strings.HasPrefix(name, "lan.") {
		dnsLabel = strings.TrimPrefix(name, "lan.")
		isEndpoint = true
		endpointType = "LAN"
	} else if strings.HasPrefix(name, "nated.") {
		dnsLabel = strings.TrimPrefix(name, "nated.")
		isEndpoint = true
		endpointType = "NAT"
	} else {
		// Direct pubkey query
		dnsLabel = name
	}

	// Convert DNS label (base32) to WireGuard pubkey (base64)
	pubkey, err := dnsLabelToPubkey(dnsLabel)
	if err != nil {
		log.Printf("[valon] Invalid DNS label format: %s (%v)", dnsLabel, err)
		return v.nxdomain(w, r)
	}

	var etcdKey string
	if isEndpoint {
		if endpointType == "LAN" {
			etcdKey = fmt.Sprintf("/valon/peers/%s/endpoints/lan", pubkey)
		} else {
			etcdKey = fmt.Sprintf("/valon/peers/%s/endpoints/nated", pubkey)
		}
	} else {
		etcdKey = fmt.Sprintf("/valon/peers/%s/wg_ip", pubkey)
	}

	log.Printf("[valon] A query for: %s (label: %s, pubkey: %s) -> etcd key: %s", state.Name(), dnsLabel, pubkey, etcdKey)

	// Query etcd
	ctxTimeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := v.etcdClient.Get(ctxTimeout, etcdKey)
	if err != nil {
		log.Printf("[valon] etcd query error: %v", err)
		return v.nxdomain(w, r)
	}

	if len(resp.Kvs) == 0 {
		log.Printf("[valon] No data found in etcd for key: %s", etcdKey)
		return v.nxdomain(w, r)
	}

	value := string(resp.Kvs[0].Value)

	var ip net.IP
	if isEndpoint {
		// Parse endpoint format: "IP:PORT"
		host, _, err := net.SplitHostPort(value)
		if err != nil {
			log.Printf("[valon] Invalid %s endpoint format: %s", endpointType, value)
			return v.nxdomain(w, r)
		}
		ip = net.ParseIP(host)
	} else {
		// Parse WireGuard IP
		ip = net.ParseIP(value)
	}

	if ip == nil {
		log.Printf("[valon] Invalid IP address: %s", value)
		return v.nxdomain(w, r)
	}

	// Return A record
	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   state.Name(),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    30,
		},
		A: ip.To4(),
	}
	m.Answer = append(m.Answer, rr)

	log.Printf("[valon] Returning A record: %s -> %s", state.Name(), ip.String())
	w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}

// handleSRV handles SRV record queries.
// Queries etcd for endpoint information and returns SRV records.
func (v Valon) handleSRV(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, state request.Request) (int, error) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	// Extract DNS label from SRV query
	// Format: _wireguard._udp.<base32-label>.valon.internal.
	name := strings.TrimSuffix(state.Name(), v.Zone)
	name = strings.TrimSuffix(name, ".")

	if !strings.HasPrefix(name, "_wireguard._udp.") {
		log.Printf("[valon] Invalid SRV query format: %s", state.Name())
		return v.nxdomain(w, r)
	}

	dnsLabel := strings.TrimPrefix(name, "_wireguard._udp.")

	// Convert DNS label (base32) to WireGuard pubkey (base64)
	pubkey, err := dnsLabelToPubkey(dnsLabel)
	if err != nil {
		log.Printf("[valon] Invalid DNS label format: %s (%v)", dnsLabel, err)
		return v.nxdomain(w, r)
	}

	log.Printf("[valon] SRV query for label: %s (pubkey: %s)", dnsLabel, pubkey)

	// Query etcd for both LAN and NAT endpoints
	ctxTimeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	lanKey := fmt.Sprintf("/valon/peers/%s/endpoints/lan", pubkey)
	natedKey := fmt.Sprintf("/valon/peers/%s/endpoints/nated", pubkey)

	lanResp, err := v.etcdClient.Get(ctxTimeout, lanKey)
	if err != nil {
		log.Printf("[valon] etcd query error for LAN endpoint: %v", err)
	}

	natedResp, err := v.etcdClient.Get(ctxTimeout, natedKey)
	if err != nil {
		log.Printf("[valon] etcd query error for NAT endpoint: %v", err)
	}

	// Process LAN endpoint
	if len(lanResp.Kvs) > 0 {
		endpoint := string(lanResp.Kvs[0].Value)
		host, portStr, err := net.SplitHostPort(endpoint)
		if err == nil {
			port, _ := strconv.Atoi(portStr)
			target := fmt.Sprintf("lan.%s.%s", dnsLabel, v.Zone)

			srv := &dns.SRV{
				Hdr: dns.RR_Header{
					Name:   state.Name(),
					Rrtype: dns.TypeSRV,
					Class:  dns.ClassINET,
					Ttl:    30,
				},
				Priority: 0,
				Weight:   0,
				Port:     uint16(port),
				Target:   target,
			}
			m.Answer = append(m.Answer, srv)

			// Add A record in Additional section
			ip := net.ParseIP(host)
			if ip != nil {
				a := &dns.A{
					Hdr: dns.RR_Header{
						Name:   target,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    30,
					},
					A: ip.To4(),
				}
				m.Extra = append(m.Extra, a)
			}
			log.Printf("[valon] Added LAN SRV record: %s -> %s:%d (target: %s)", state.Name(), host, port, target)
		}
	}

	// Process NAT endpoint
	if len(natedResp.Kvs) > 0 {
		endpoint := string(natedResp.Kvs[0].Value)
		host, portStr, err := net.SplitHostPort(endpoint)
		if err == nil {
			port, _ := strconv.Atoi(portStr)
			target := fmt.Sprintf("nated.%s.%s", dnsLabel, v.Zone)

			srv := &dns.SRV{
				Hdr: dns.RR_Header{
					Name:   state.Name(),
					Rrtype: dns.TypeSRV,
					Class:  dns.ClassINET,
					Ttl:    30,
				},
				Priority: 10, // Lower priority than LAN
				Weight:   0,
				Port:     uint16(port),
				Target:   target,
			}
			m.Answer = append(m.Answer, srv)

			// Add A record in Additional section
			ip := net.ParseIP(host)
			if ip != nil {
				a := &dns.A{
					Hdr: dns.RR_Header{
						Name:   target,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    30,
					},
					A: ip.To4(),
				}
				m.Extra = append(m.Extra, a)
			}
			log.Printf("[valon] Added NAT SRV record: %s -> %s:%d (target: %s)", state.Name(), host, port, target)
		}
	}

	// If no endpoints found, return NXDOMAIN
	if len(m.Answer) == 0 {
		log.Printf("[valon] No endpoints found for pubkey: %s", pubkey)
		return v.nxdomain(w, r)
	}

	w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}

// nxdomain returns NXDOMAIN response.
func (v Valon) nxdomain(w dns.ResponseWriter, r *dns.Msg) (int, error) {
	m := new(dns.Msg)
	m.SetRcode(r, dns.RcodeNameError)
	m.Authoritative = true
	w.WriteMsg(m)
	return dns.RcodeNameError, nil
}
