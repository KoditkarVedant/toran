package main

// Limits on guessing passwords.
//
// failureLimiter counts failed logins per (visitor address, user name) and per visitor
// address in a sliding window, and says how long to wait once a limit is reached. Counting per
// name and address together means a stranger cannot lock the real owner out by typing the
// owner's name wrongly from somewhere else. Everything is in memory: a restart gives an attacker
// a fresh set of tries, which is acceptable for a home server and keeps the design simple.
//
// hashGate keeps only a couple of password checks running at once. Each one costs a fraction
// of a second of CPU, and a login needs no account, so without a cap anyone could keep the
// server busy.

import (
	"context"
	"strings"
	"sync"
	"time"
)

type failureLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	window  time.Duration
	perName int // failures per (address, name) before refusing
	perAddr int // failures per address before refusing
	maxKeys int // memory bound
	fails   map[string][]time.Time
}

func newFailureLimiter() *failureLimiter {
	return &failureLimiter{now: time.Now, window: 15 * time.Minute, perName: 5, perAddr: 20, maxKeys: 10000, fails: map[string][]time.Time{}}
}

func nameKey(addr, name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if len(name) > 40 {
		name = name[:40] // names are at most 32 characters; longer ones are junk that must not grow the map
	}
	return "n|" + addr + "|" + name
}
func addrKey(addr string) string { return "a|" + addr }

// recent drops expired entries of one key and returns what is left. Call with mu held.
func (l *failureLimiter) recent(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	ts := l.fails[key]
	i := 0
	for i < len(ts) && !ts[i].After(cutoff) {
		i++
	}
	if i > 0 {
		ts = ts[i:]
		if len(ts) == 0 {
			delete(l.fails, key)
		} else {
			l.fails[key] = ts
		}
	}
	return ts
}

// wait is how long until the key is below its limit again (zero if it already is).
func (l *failureLimiter) wait(key string, limit int) time.Duration {
	ts := l.recent(key)
	if len(ts) < limit {
		return 0
	}
	return ts[len(ts)-limit].Add(l.window).Sub(l.now())
}

// allow says whether this visitor may try a password now; if not, how long to wait.
func (l *failureLimiter) allow(addr, name string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := max(l.wait(nameKey(addr, name), l.perName), l.wait(addrKey(addr), l.perAddr))
	return d <= 0, d
}

// failed records a wrong password.
func (l *failureLimiter) failed(addr, name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.fails) >= l.maxKeys {
		l.sweep()
	}
	now := l.now()
	for _, k := range []string{nameKey(addr, name), addrKey(addr)} {
		if _, known := l.fails[k]; !known && len(l.fails) >= l.maxKeys {
			clear(l.fails) // still full of live entries: someone is spraying; forget rather than grow
		}
		l.fails[k] = append(l.fails[k], now)
	}
}

// succeeded forgets the failures for this name from this address.
func (l *failureLimiter) succeeded(addr, name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, nameKey(addr, name))
}

// sweep removes every expired entry. Call with mu held.
func (l *failureLimiter) sweep() {
	for k := range l.fails {
		l.recent(k)
	}
}

type hashGate chan struct{}

func newHashGate(n int) hashGate { return make(hashGate, n) }

// acquire waits for a free slot, up to the context's deadline. The returned function frees it.
func (g hashGate) acquire(ctx context.Context) (release func(), ok bool) {
	select {
	case g <- struct{}{}:
		return func() { <-g }, true
	case <-ctx.Done():
		return nil, false
	}
}
