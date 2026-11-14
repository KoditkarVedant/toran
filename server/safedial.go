package main

// Where the status checker may connect.
//
// The checker fetches every URL in services.json, and that file can be changed by anyone
// who may save. Left unchecked it could be pointed at things that are never a service to
// monitor, notably the cloud-metadata address 169.254.169.254, which hands out credentials.
//
// Home services are the whole point of the feature, so loopback and private addresses are
// allowed. What is refused are link-local, unspecified (0.0.0.0, ::) and multicast
// addresses. The check runs in the dialer, after the name has been resolved and just
// before connecting, so it judges the address that would really be used. That also covers
// names that resolve to a blocked address, and a name that changes its answer between
// lookups (DNS rebinding). Redirects are not followed (see newServer), so they cannot lead
// somewhere else either.

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

var thisNetwork = netip.MustParsePrefix("0.0.0.0/8") // "this network": 0.0.0.0 reaches the local machine on Linux

// blockedTarget reports whether the checker must not connect to ip.
func blockedTarget(ip netip.Addr) bool {
	ip = ip.Unmap() // ::ffff:169.254.169.254 is the same address as 169.254.169.254
	return !ip.IsValid() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() || thisNetwork.Contains(ip)
}

// dialControl is net.Dialer.Control: it sees the final "ip:port" before the connection is made.
func dialControl(network, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("refusing to connect to %q", address)
	}
	if blockedTarget(ap.Addr()) {
		return fmt.Errorf("refusing to connect to %s: link-local, unspecified and multicast addresses are not checked", ap.Addr())
	}
	return nil
}

// newCheckTransport is the transport used for status checks. It deliberately has no Proxy,
// so environment proxy settings are ignored, and a guarded dialer.
func newCheckTransport() *http.Transport {
	d := &net.Dialer{Timeout: 2 * time.Second, Control: dialControl}
	return &http.Transport{
		DisableKeepAlives: true,
		DialContext:       d.DialContext,
		// Home services often use self-signed certificates. Only the HTTP status code is read:
		// no credentials are sent and no response body is kept, so there is nothing to protect
		// from an impostor, and a certificate error would only turn a running service red.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}
}
