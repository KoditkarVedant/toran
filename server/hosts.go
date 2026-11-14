package main

// Host allowlist (ALLOWED_HOSTS), the defence against DNS rebinding.
//
// In a rebinding attack a website's name is pointed at your Toran address. The browser then
// treats Toran as part of that website, so the cross-site check (which compares Origin with
// Host) passes and the request comes from a machine on your network, so LAN_ONLY passes too.
// The one thing the attacker cannot change is the Host header: it holds their name, not yours.
// So when ALLOWED_HOSTS is set, any request whose Host is a name not on the list is refused.
//
// Addresses written as numbers (192.168.1.5, [::1]) and "localhost" are always accepted: a
// rebinding attack needs a name, and the container health check connects to 127.0.0.1.
// Leaving the setting empty turns the check off, because Toran cannot guess what you call it.

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// hostOf reduces a Host header or a configured entry to a bare, comparable name: no port,
// no brackets, lower case, no trailing dot, IPv6 in its canonical form.
func hostOf(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.ToLower(strings.TrimSuffix(strings.Trim(h, "[]"), "."))
	if a, err := netip.ParseAddr(h); err == nil {
		return a.Unmap().String()
	}
	return h
}

// allowHosts reads the comma-separated ALLOWED_HOSTS list. Entries are plain host names:
// a scheme, port, path or wildcard is almost certainly a mistake, so it stops startup.
func (s *server) allowHosts(list string) error {
	for _, e := range splitList(list) {
		if strings.ContainsAny(e, "/*@ ") || strings.Contains(e, "://") || (strings.Contains(e, ":") && !strings.Contains(e, "::")) {
			return fmt.Errorf("ALLOWED_HOSTS: %q is not a plain host name (write toran.home.lan, not a URL, port or wildcard)", e)
		}
		if s.hosts == nil {
			s.hosts = map[string]bool{}
		}
		s.hosts[hostOf(e)] = true
	}
	return nil
}

// hostAllowed reports whether the request's Host may be served.
func (s *server) hostAllowed(host string) bool {
	if s.hosts == nil {
		return true // not configured
	}
	h := hostOf(host)
	if h == "localhost" {
		return true
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return true
	}
	return s.hosts[h]
}

// hostGuard wraps the whole handler, so no route can skip it.
func (s *server) hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostAllowed(r.Host) {
			log.Printf("blocked request for unknown host %q: %s %s", r.Host, r.Method, r.URL.Path)
			http.Error(w, "unknown host name. If this is the address you use for Toran, add it to ALLOWED_HOSTS "+
				"(see docs/configuration.md).", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
