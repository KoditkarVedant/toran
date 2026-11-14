package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBlockedTarget(t *testing.T) {
	blocked := []string{
		"169.254.169.254",        // cloud metadata
		"169.254.0.1",            // link-local
		"::ffff:169.254.169.254", // the same address written as IPv6
		"fe80::1",                // IPv6 link-local
		"0.0.0.0", "::", "0.1.2.3",
		"::ffff:0.0.0.0", "::ffff:0.1.2.3", // "this network" written as IPv6: the library does not unmap these itself
		"224.0.0.1", "239.255.255.250", "ff02::1", // multicast
	}
	for _, s := range blocked {
		if !blockedTarget(netip.MustParseAddr(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	if !blockedTarget(netip.Addr{}) {
		t.Error("an invalid address should be blocked")
	}
	allowed := []string{
		"127.0.0.1", "::1", "::ffff:127.0.0.1", // this machine: services on localhost are the point
		"10.0.0.5", "172.16.3.4", "192.168.1.1", "fd00::5", // the home network
		"8.8.8.8", "93.184.216.34", "2606:4700::1111", // public sites can be monitored too
	}
	for _, s := range allowed {
		if blockedTarget(netip.MustParseAddr(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

// The guard has to act before any connection is made, so these do not depend on the network.
func TestCheckerRefusesMetadataAddress(t *testing.T) {
	s, _, _ := newTest(t)
	for _, u := range []string{"http://169.254.169.254/latest/meta-data/", "http://169.254.169.254:8080/", "https://[fe80::1]/"} {
		_, err := s.client.Get(u)
		if err == nil || !strings.Contains(err.Error(), "refusing to connect") {
			t.Errorf("GET %s: want the guard to refuse, got %v", u, err)
		}
		if c := s.checkOne(u); c.State != "down" {
			t.Errorf("checkOne(%s) = %+v, want down", u, c)
		}
	}
}

func TestCheckerStillReachesLocalServices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })) // 127.0.0.1
	defer srv.Close()
	s, _, _ := newTest(t)
	if c := s.checkOne(srv.URL); c.State != "up" || c.Code != 200 {
		t.Errorf("a service on localhost must still be checked: %+v", c)
	}
}

func TestCheckerDoesNotFollowRedirects(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	s, _, _ := newTest(t)
	c := s.checkOne(redirector.URL)
	if c.Code != http.StatusFound {
		t.Errorf("the redirect itself is the answer: %+v", c)
	}
	if reached.Load() != 0 {
		t.Error("the checker followed a redirect")
	}
}

func TestCheckerOnlyChecksWebAddresses(t *testing.T) {
	s, _, _ := newTest(t)
	for _, u := range []string{"ftp://127.0.0.1:21/", "file:///etc/passwd", "gopher://127.0.0.1/", "http:///nohost", "ssh://pi"} {
		if c := s.checkOne(u); c.State != "down" {
			t.Errorf("checkOne(%q) = %+v, want down", u, c)
		}
	}
}

func TestDialControl(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
		}
	}()
	d := &net.Dialer{Control: dialControl}
	if c, err := d.Dial("tcp", ln.Addr().String()); err != nil {
		t.Errorf("loopback must be reachable: %v", err)
	} else {
		c.Close()
	}
	if _, err := d.Dial("tcp", "169.254.169.254:80"); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("metadata address must be refused, got %v", err)
	}
}
