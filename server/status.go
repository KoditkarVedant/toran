package main

// Server-side health checks of the services listed in the config.

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var portOnly = regexp.MustCompile(`^:?(\d{1,5})$`)

func (s *server) target(u string) string {
	u = strings.TrimSpace(u)
	if m := portOnly.FindStringSubmatch(u); m != nil {
		return "http://" + net.JoinHostPort(s.checkHost, m[1])
	}
	if !strings.Contains(u, "://") {
		return "http://" + u
	}
	return u
}

func (s *server) configURLs() []string {
	b, err := os.ReadFile(s.configPath())
	if err != nil {
		return nil
	}
	var c struct {
		Groups []struct {
			Items []struct {
				URL string `json:"url"`
			} `json:"items"`
		} `json:"groups"`
	}
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	seen, out := map[string]bool{}, []string{}
	for _, g := range c.Groups {
		for _, i := range g.Items {
			if i.URL != "" && !seen[i.URL] {
				seen[i.URL] = true
				out = append(out, i.URL)
			}
		}
	}
	return out
}

func (s *server) checkOne(u string) check {
	start := time.Now()
	req, err := http.NewRequest(http.MethodGet, s.target(u), nil)
	if err != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.Hostname() == "" {
		return check{State: "down"} // only web addresses are checked: no file://, ftp:// ...
	}
	req.Header.Set("User-Agent", "toran-status/"+version)
	resp, err := s.client.Do(req)
	ms := int(time.Since(start) / time.Millisecond)
	if err != nil {
		return check{State: "down", MS: ms}
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	resp.Body.Close()
	state := "up"
	if resp.StatusCode >= 500 {
		state = "warn"
	}
	return check{State: state, Code: resp.StatusCode, MS: ms}
}

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	urls := s.configURLs()
	out := make(map[string]check, len(urls))
	var stale []string
	s.stMu.Lock()
	for _, u := range urls {
		if c, ok := s.stCache[u]; ok && time.Since(c.at) < s.stTTL {
			out[u] = c
		} else {
			stale = append(stale, u)
		}
	}
	s.stMu.Unlock()

	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	for _, u := range stale {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			c := s.checkOne(u)
			<-sem
			c.at = time.Now()
			mu.Lock()
			out[u] = c
			mu.Unlock()
		}(u)
	}
	wg.Wait()
	s.stMu.Lock()
	for _, u := range stale {
		s.stCache[u] = out[u]
	}
	s.stMu.Unlock()
	writeJSON(w, 200, out)
}
