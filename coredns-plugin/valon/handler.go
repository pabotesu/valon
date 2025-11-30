package valon

import (
	"context"
	"log"
	"strings"

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
// Phase 1: Return a fixed test response.
func (v Valon) handleA(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, state request.Request) (int, error) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	// Phase 1: Fixed test response
	// TODO: Implement actual logic to query etcd and return WireGuard IPs
	log.Printf("[valon] A query for: %s (returning test response)", state.Name())

	// Return a test response: test.valon.internal. -> 100.64.0.1
	if strings.HasPrefix(state.Name(), "test.") {
		rr := &dns.A{
			Hdr: dns.RR_Header{
				Name:   state.Name(),
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    30,
			},
			A: []byte{100, 64, 0, 1}, // 100.64.0.1
		}
		m.Answer = append(m.Answer, rr)

		w.WriteMsg(m)
		return dns.RcodeSuccess, nil
	}

	// No match, return NXDOMAIN
	return v.nxdomain(w, r)
}

// handleSRV handles SRV record queries.
// Phase 1: Return a fixed test response.
func (v Valon) handleSRV(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, state request.Request) (int, error) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	// Phase 1: Fixed test response
	// TODO: Implement actual DNS-SD logic
	log.Printf("[valon] SRV query for: %s (returning test response)", state.Name())

	// Return a test SRV response
	if strings.HasPrefix(state.Name(), "_wireguard._udp.test.") {
		srv := &dns.SRV{
			Hdr: dns.RR_Header{
				Name:   state.Name(),
				Rrtype: dns.TypeSRV,
				Class:  dns.ClassINET,
				Ttl:    30,
			},
			Priority: 0,
			Weight:   0,
			Port:     51820,
			Target:   "test.valon.internal.",
		}
		m.Answer = append(m.Answer, srv)

		// Add corresponding A record in Additional section
		a := &dns.A{
			Hdr: dns.RR_Header{
				Name:   "test.valon.internal.",
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    30,
			},
			A: []byte{100, 64, 0, 1},
		}
		m.Extra = append(m.Extra, a)

		w.WriteMsg(m)
		return dns.RcodeSuccess, nil
	}

	// No match, return NXDOMAIN
	return v.nxdomain(w, r)
}

// nxdomain returns NXDOMAIN response.
func (v Valon) nxdomain(w dns.ResponseWriter, r *dns.Msg) (int, error) {
	m := new(dns.Msg)
	m.SetRcode(r, dns.RcodeNameError)
	m.Authoritative = true
	w.WriteMsg(m)
	return dns.RcodeNameError, nil
}
