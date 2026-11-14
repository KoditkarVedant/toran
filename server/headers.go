package main

// Security headers sent with every response.
//
// They tell the browser what the page may do, as a second line of defence: not to be put
// in someone else's frame, not to guess file types, not to leak the address to other sites.
// The Content-Security-Policy is written for what index.html actually loads; if the page
// starts using something new (an external font, a worker...), add it here, or the browser
// will silently block it.

import (
	"net/http"
	"strings"
)

// contentSecurityPolicy, one directive per line so each choice can be explained.
var contentSecurityPolicy = strings.Join([]string{
	"default-src 'self'",
	// The page is one file with inline scripts and styles, so 'unsafe-inline' is needed for now.
	// It means this policy limits what an injected script can load and where it can post, but
	// cannot stop it from running. Moving the script into its own file would let this go.
	"script-src 'self' 'unsafe-inline'",
	"style-src 'self' 'unsafe-inline'",
	// Icons, wallpapers and logos can come from any address the owner types in, plus the icon CDN.
	"img-src 'self' data: blob: http: https:",
	// Without the Go server the browser itself pings each service, so any http(s) address is needed.
	// Weather and the icon CDN are covered by the same rule.
	"connect-src 'self' http: https:",
	"object-src 'none'",      // no plugins
	"base-uri 'self'",        // an injected <base> cannot redirect relative links
	"form-action 'self'",     // an injected <form> cannot post elsewhere
	"frame-ancestors 'none'", // nobody may put Toran in a frame (clickjacking)
}, "; ")

// securityHeaders sets the headers before the handler runs, so a handler can still replace
// one on purpose: uploaded images get a stricter policy in static.go.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY") // older browsers; frame-ancestors covers the rest
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}
