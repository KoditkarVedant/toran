package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	const proxies = "10.0.0.1, 172.16.0.0/12, fd00::/8"
	cases := []struct {
		name  string
		trust string
		peer  string
		xff   []string // one entry per X-Forwarded-For header line
		want  string   // "" means: cannot be determined
	}{
		{"no proxies configured: a forged header is ignored", "", "8.8.8.8:4000", []string{"192.168.1.5"}, "8.8.8.8"},
		{"peer is not a trusted proxy: header ignored", proxies, "8.8.8.8:4000", []string{"192.168.1.5"}, "8.8.8.8"},
		{"trusted proxy, no header: the proxy itself", proxies, "10.0.0.1:4000", nil, "10.0.0.1"},
		{"trusted proxy, one visitor", proxies, "10.0.0.1:4000", []string{"203.0.113.9"}, "203.0.113.9"},
		{"visitor forges a private address on the left", proxies, "10.0.0.1:4000", []string{"192.168.1.50, 203.0.113.9"}, "203.0.113.9"},
		{"two proxies in a row", proxies, "10.0.0.1:4000", []string{"203.0.113.9, 172.16.0.5"}, "203.0.113.9"},
		{"header split over two lines", proxies, "10.0.0.1:4000", []string{"192.168.1.50", "203.0.113.9"}, "203.0.113.9"},
		{"entry with a port", proxies, "10.0.0.1:4000", []string{"203.0.113.9:4444"}, "203.0.113.9"},
		{"IPv6 visitor", proxies, "10.0.0.1:4000", []string{"2606:4700::1"}, "2606:4700::1"},
		{"IPv6 visitor in brackets with a port", proxies, "10.0.0.1:4000", []string{"[2606:4700::1]:99"}, "2606:4700::1"},
		{"IPv6 proxy", proxies, "[fd00::1]:4000", []string{"203.0.113.9"}, "203.0.113.9"},
		{"IPv4-mapped peer is matched as IPv4", proxies, "[::ffff:10.0.0.1]:4000", []string{"203.0.113.9"}, "203.0.113.9"},
		{"every address is a trusted proxy: the leftmost", proxies, "10.0.0.1:4000", []string{"172.16.0.9, 172.16.0.5"}, "172.16.0.9"},
		{"unreadable entry: refuse", proxies, "10.0.0.1:4000", []string{"203.0.113.9, not-an-ip"}, ""},
		{"empty entry: refuse", proxies, "10.0.0.1:4000", []string{"203.0.113.9, "}, ""},
		{"unreadable peer address: refuse", proxies, "garbage", nil, ""},
		{"CIDR is masked", "10.1.2.3/8", "10.9.9.9:1", []string{"203.0.113.9"}, "203.0.113.9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _, _ := newTest(t)
			if err := s.trustProxies(c.trust); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = c.peer
			for _, v := range c.xff {
				req.Header.Add("X-Forwarded-For", v)
			}
			ip, ok := s.clientIP(req)
			switch {
			case c.want == "" && ok:
				t.Errorf("want no answer, got %s", ip)
			case c.want != "" && (!ok || ip.String() != c.want):
				t.Errorf("want %s, got %s (ok=%v)", c.want, ip, ok)
			}
		})
	}
}

// What the setting is for: LAN_ONLY has to judge the visitor, not the proxy in front of them.
func TestLANOnlyBehindProxy(t *testing.T) {
	save := func(h http.Handler, peer, xff string) int {
		hdr := map[string]string{}
		if xff != "" {
			hdr["X-Forwarded-For"] = xff
		}
		return doWith(h, "PUT", "/services.json", peer, []byte(`{"groups":[]}`), hdr).Code
	}

	t.Run("without TRUSTED_PROXIES the header cannot grant access", func(t *testing.T) {
		_, h, _ := newTest(t)
		if c := save(h, "8.8.8.8:1", "192.168.1.5"); c != 403 {
			t.Errorf("public peer forging a private X-Forwarded-For: want 403, got %d", c)
		}
	})
	t.Run("without TRUSTED_PROXIES a local proxy makes everyone look local (the problem being solved)", func(t *testing.T) {
		_, h, _ := newTest(t)
		if c := save(h, "127.0.0.1:1", "203.0.113.9"); c != 204 {
			t.Errorf("want 204 (the proxy's address is judged), got %d", c)
		}
	})
	t.Run("with TRUSTED_PROXIES a public visitor is refused", func(t *testing.T) {
		s, h, _ := newTest(t)
		if err := s.trustProxies("127.0.0.1"); err != nil {
			t.Fatal(err)
		}
		if c := save(h, "127.0.0.1:1", "203.0.113.9"); c != 403 {
			t.Errorf("want 403, got %d", c)
		}
	})
	t.Run("with TRUSTED_PROXIES a local visitor is allowed", func(t *testing.T) {
		s, h, _ := newTest(t)
		if err := s.trustProxies("127.0.0.1"); err != nil {
			t.Fatal(err)
		}
		if c := save(h, "127.0.0.1:1", "192.168.1.20"); c != 204 {
			t.Errorf("want 204, got %d", c)
		}
	})
	t.Run("a visitor cannot pose as local through a trusted proxy", func(t *testing.T) {
		s, h, _ := newTest(t)
		if err := s.trustProxies("127.0.0.1"); err != nil {
			t.Fatal(err)
		}
		if c := save(h, "127.0.0.1:1", "192.168.1.20, 203.0.113.9"); c != 403 {
			t.Errorf("want 403, got %d", c)
		}
	})
	t.Run("an unreadable header is refused", func(t *testing.T) {
		s, h, _ := newTest(t)
		if err := s.trustProxies("127.0.0.1"); err != nil {
			t.Fatal(err)
		}
		if c := save(h, "127.0.0.1:1", "garbage"); c != 403 {
			t.Errorf("want 403, got %d", c)
		}
	})
	t.Run("LAN_ONLY=false still allows anyone", func(t *testing.T) {
		s, h, _ := newTest(t)
		s.lanOnly = false
		if c := save(h, "8.8.8.8:1", ""); c != 204 {
			t.Errorf("want 204, got %d", c)
		}
	})
}

func TestTrustProxiesRejectsBadValues(t *testing.T) {
	for _, bad := range []string{"nope", "10.0.0.0/33", "*", "10.0.0.1, host.example", "fe80::1%eth0", "10.0.0.0/"} {
		s, _, _ := newTest(t)
		if err := s.trustProxies(bad); err == nil {
			t.Errorf("TRUSTED_PROXIES=%q should be rejected", bad)
		}
	}
	for _, good := range []string{"", " ", "10.0.0.1", "10.0.0.0/8, 192.168.0.0/16", "::1, fd00::/8"} {
		s, _, _ := newTest(t)
		if err := s.trustProxies(good); err != nil {
			t.Errorf("TRUSTED_PROXIES=%q should be accepted: %v", good, err)
		}
	}
}
