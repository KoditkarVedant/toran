package main

// Who may write: writes are limited to the local network unless LAN_ONLY=false.

import (
	"net"
	"net/http"
)

// Writes are limited to the local network (loopback + private ranges) unless LAN_ONLY=false.
func (s *server) clientAllowed(r *http.Request) bool {
	if !s.lanOnly {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

func (s *server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.clientAllowed(r) {
			http.Error(w, "writes are limited to the local network", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}
