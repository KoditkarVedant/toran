package main

// Cross-site request protection (CSRF).
//
// Without it, any web page open in a browser on your network could make that browser
// save changes to Toran. The check uses the standard library's http.CrossOriginProtection:
// for requests that change something (PUT, POST, DELETE) it looks at the Sec-Fetch-Site
// header and, on plain-HTTP addresses where browsers do not send it, compares the Origin
// header with the Host header. Requests that carry neither header (curl, scripts) pass:
// they are not a browser being steered by another site, and LAN_ONLY still applies to them.

import (
	"fmt"
	"log"
	"net/http"
)

// newOriginGuard returns the guard with a deny response that says what to do about it.
func newOriginGuard() *http.CrossOriginProtection {
	g := http.NewCrossOriginProtection()
	g.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("blocked cross-origin %s %s (Origin %q, Host %q): %v", r.Method, r.URL.Path, r.Header.Get("Origin"), r.Host, g.Check(r))
		http.Error(w, "request blocked: it came from another website. "+
			"If you reach Toran through a reverse proxy, set TRUSTED_ORIGINS to the address in your browser "+
			"(see docs/configuration.md).", http.StatusForbidden)
	}))
	return g
}

// trustOrigins allows writes whose Origin exactly matches one of the comma-separated
// scheme://host[:port] values, for setups where a proxy changes the Host header.
func (s *server) trustOrigins(list string) error {
	for _, o := range splitList(list) {
		if err := s.origin.AddTrustedOrigin(o); err != nil {
			return fmt.Errorf("TRUSTED_ORIGINS: %w", err)
		}
	}
	return nil
}
