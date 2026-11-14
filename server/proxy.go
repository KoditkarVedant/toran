package main

// Finding the real visitor's address.
//
// By default the address of the TCP connection is the visitor. Behind a reverse proxy that
// address is the proxy's, so LAN_ONLY would judge the proxy instead of the visitor: either
// everything looks local, or nothing does. TRUSTED_PROXIES lists the proxies whose
// X-Forwarded-For header may be believed; the header from anyone else is ignored.

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

// trustProxies reads a comma-separated list of IP addresses and CIDR ranges.
// Call it before the server starts: the list is not locked.
func (s *server) trustProxies(list string) error {
	var out []netip.Prefix
	for _, e := range splitList(list) {
		p, err := parseProxy(e)
		if err != nil {
			return fmt.Errorf("TRUSTED_PROXIES: %q is not an IP address or a CIDR range such as 10.0.0.0/8", e)
		}
		out = append(out, p)
	}
	s.proxies = out
	return nil
}

func parseProxy(e string) (netip.Prefix, error) {
	if strings.Contains(e, "/") {
		p, err := netip.ParsePrefix(e)
		return p.Masked(), err
	}
	a, err := netip.ParseAddr(e)
	if err != nil || a.Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("bad address")
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func (s *server) isTrustedProxy(a netip.Addr) bool {
	for _, p := range s.proxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// parseAddr reads "1.2.3.4", "1.2.3.4:80", "::1", "[::1]" or "[::1]:80".
func parseAddr(v string) (netip.Addr, bool) {
	v = strings.TrimSpace(v)
	a, err := netip.ParseAddr(v)
	if err != nil {
		if ap, err2 := netip.ParseAddrPort(v); err2 == nil {
			a, err = ap.Addr(), nil
		} else {
			a, err = netip.ParseAddr(strings.Trim(v, "[]"))
		}
	}
	if err != nil {
		return netip.Addr{}, false
	}
	return a.WithZone("").Unmap(), true
}

// clientIP is the visitor's address, or false when it cannot be worked out (the caller must refuse).
//
// The connection's own address is used unless it belongs to a trusted proxy. Then
// X-Forwarded-For is read from the right, which is the end our own proxy appends to:
// the first address that is not a trusted proxy is the visitor. Entries further left
// were supplied by the visitor and are never used, so they cannot be forged.
func (s *server) clientIP(r *http.Request) (netip.Addr, bool) {
	peer, ok := parseAddr(r.RemoteAddr)
	if !ok {
		return netip.Addr{}, false
	}
	if !s.isTrustedProxy(peer) {
		return peer, true
	}
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		a, ok := parseAddr(hops[i])
		if !ok {
			return netip.Addr{}, false // a proxy of ours wrote something unreadable: fail closed
		}
		if i == 0 || !s.isTrustedProxy(a) {
			return a, true
		}
	}
	return peer, true // no header: the proxy itself is talking to us
}
