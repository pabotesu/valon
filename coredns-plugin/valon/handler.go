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
// Supports both direct pubkey queries and CNAME aliases.
func (v Valon) handleA(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, state request.Request) (int, error) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	// Extract label from query name
	// Format: <base32-label>.valon.internal. or lan.<base32-label>.valon.internal. or nated.<base32-label>.valon.internal.
	// Or: <alias>.valon.internal. (CNAME to base32 label)
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
		// Could be direct pubkey query or alias
		dnsLabel = name
	}

	// Try to convert DNS label (base32) to WireGuard pubkey (base64)
	pubkey, err := dnsLabelToPubkey(dnsLabel)
	if err != nil {
		// Not a valid base32 label, try alias lookup
		if !isEndpoint {
			if targetLabel := v.lookupAlias(ctx, dnsLabel); targetLabel != "" {
				log.Printf("[valon] Alias lookup: %s -> %s", dnsLabel, targetLabel)
				return v.returnCNAME(ctx, w, r, state, targetLabel)
			}
		}
		log.Printf("[valon] Invalid DNS label format: %s (%v)", dnsLabel, err)
		return v.nxdomain(w, r)
	}

	log.Printf("[valon] A query for: %s (label: %s, pubkey: %s)", state.Name(), dnsLabel, pubkey)

	// Query cache
	peerInfo := v.cache.Get(pubkey)
	if peerInfo == nil {
		log.Printf("[valon] No data found in cache for pubkey: %s", pubkey)
		return v.nxdomain(w, r)
	}

	var value string
	if isEndpoint {
		if endpointType == "LAN" {
			value = peerInfo.LANEndpoint
		} else {
			value = peerInfo.NATEndpoint
		}
		if value == "" {
			log.Printf("[valon] %s endpoint not available for pubkey: %s", endpointType, pubkey)
			return v.nxdomain(w, r)
		}
	} else {
		value = peerInfo.WgIP
		if value == "" {
			log.Printf("[valon] WireGuard IP not available for pubkey: %s", pubkey)
			return v.nxdomain(w, r)
		}
	}

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

	// Query cache
	peerInfo := v.cache.Get(pubkey)
	if peerInfo == nil {
		log.Printf("[valon] No data found in cache for pubkey: %s", pubkey)
		return v.nxdomain(w, r)
	}

	// Process LAN endpoint (from DDNS API)
	if peerInfo.LANEndpoint != "" {
		endpoint := peerInfo.LANEndpoint
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
				Priority: 0, // Higher priority
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
			log.Printf("[valon] Added LAN SRV record: %s -> %s:%d", state.Name(), host, port)
		}
	}

	// Process NAT endpoint (from wg show observation)
	if peerInfo.NATEndpoint != "" {
		endpoint := peerInfo.NATEndpoint
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
				Priority: 10, // Lower priority
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
			log.Printf("[valon] Added NAT SRV record: %s -> %s:%d", state.Name(), host, port)
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

// lookupAlias queries etcd for CNAME alias mapping.
// Returns the target base32 label if found, empty string otherwise.
func (v Valon) lookupAlias(ctx context.Context, alias string) string {
	key := fmt.Sprintf("/valon/aliases/%s", alias)

	ctxTimeout, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	resp, err := v.etcdClient.Get(ctxTimeout, key)
	if err != nil {
		log.Printf("[valon] etcd alias lookup error: %v", err)
		return ""
	}

	if len(resp.Kvs) == 0 {
		return ""
	}

	return strings.TrimSpace(string(resp.Kvs[0].Value))
}

// returnCNAME returns a CNAME record pointing to the target label,
// along with the target's A record in the answer section.
func (v Valon) returnCNAME(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, state request.Request, targetLabel string) (int, error) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	// Create CNAME record
	targetFQDN := fmt.Sprintf("%s.%s", targetLabel, v.Zone)
	cname := &dns.CNAME{
		Hdr: dns.RR_Header{
			Name:   state.Name(),
			Rrtype: dns.TypeCNAME,
			Class:  dns.ClassINET,
			Ttl:    30,
		},
		Target: targetFQDN,
	}
	m.Answer = append(m.Answer, cname)

	// Resolve target and add A record
	pubkey, err := dnsLabelToPubkey(targetLabel)
	if err != nil {
		log.Printf("[valon] Invalid target label in CNAME: %s (%v)", targetLabel, err)
		w.WriteMsg(m) // Return CNAME only
		return dns.RcodeSuccess, nil
	}

	peerInfo := v.cache.Get(pubkey)
	if peerInfo == nil || peerInfo.WgIP == "" {
		log.Printf("[valon] Target not found in cache for CNAME: %s", targetLabel)
		w.WriteMsg(m) // Return CNAME only
		return dns.RcodeSuccess, nil
	}

	ip := net.ParseIP(peerInfo.WgIP)
	if ip != nil {
		a := &dns.A{
			Hdr: dns.RR_Header{
				Name:   targetFQDN,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    30,
			},
			A: ip.To4(),
		}
		m.Answer = append(m.Answer, a)
	}

	log.Printf("[valon] Returning CNAME: %s -> %s -> %s", state.Name(), targetFQDN, peerInfo.WgIP)
	w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}
