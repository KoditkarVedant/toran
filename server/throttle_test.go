package main

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func testLimiter() (*failureLimiter, *time.Time) {
	now := t0
	l := newFailureLimiter()
	l.now = func() time.Time { return now }
	return l, &now
}

func TestLimiterLocksAfterFiveFailuresForOneName(t *testing.T) {
	l, now := testLimiter()
	for i := 0; i < 5; i++ {
		if ok, _ := l.allow("1.2.3.4", "alice"); !ok {
			t.Fatalf("attempt %d refused too early", i+1)
		}
		l.failed("1.2.3.4", "alice")
		*now = now.Add(time.Second)
	}
	ok, wait := l.allow("1.2.3.4", "alice")
	if ok {
		t.Fatal("the sixth attempt must wait")
	}
	if wait <= 14*time.Minute || wait > 15*time.Minute {
		t.Errorf("wait %v, want just under 15 minutes (until the oldest failure expires)", wait)
	}
	if ok, _ := l.allow("1.2.3.4", " ALICE "); ok {
		t.Error("the name is compared case-insensitively")
	}

	*now = t0.Add(15*time.Minute + time.Second) // the first failure has expired, so one try is free
	if ok, _ := l.allow("1.2.3.4", "alice"); !ok {
		t.Error("after the window passes the visitor may try again")
	}
}

func TestLimiterIsPerAddressAndName(t *testing.T) {
	l, _ := testLimiter()
	for i := 0; i < 5; i++ {
		l.failed("1.2.3.4", "alice")
	}
	if ok, _ := l.allow("5.6.7.8", "alice"); !ok {
		t.Error("a stranger's failures must not lock alice out from her own address")
	}
	if ok, _ := l.allow("1.2.3.4", "bob"); !ok {
		t.Error("another name from the same address is separate (until the address limit)")
	}
}

func TestLimiterPerAddressLimit(t *testing.T) {
	l, _ := testLimiter()
	for i := 0; i < 20; i++ {
		if ok, _ := l.allow("1.2.3.4", fmt.Sprintf("user%d", i)); !ok {
			t.Fatalf("attempt %d refused too early", i+1)
		}
		l.failed("1.2.3.4", fmt.Sprintf("user%d", i))
	}
	if ok, _ := l.allow("1.2.3.4", "fresh-name"); ok {
		t.Error("one address trying many names must be stopped")
	}
	if ok, _ := l.allow("9.9.9.9", "fresh-name"); !ok {
		t.Error("other addresses are unaffected")
	}
}

func TestLimiterSuccessClearsTheName(t *testing.T) {
	l, _ := testLimiter()
	for i := 0; i < 4; i++ {
		l.failed("1.2.3.4", "alice")
	}
	l.succeeded("1.2.3.4", "alice")
	for i := 0; i < 4; i++ {
		l.failed("1.2.3.4", "alice")
	}
	if ok, _ := l.allow("1.2.3.4", "alice"); !ok {
		t.Error("a correct password resets that name's count")
	}
}

func TestLimiterMemoryIsBounded(t *testing.T) {
	l, now := testLimiter()
	l.maxKeys = 50
	for i := 0; i < 500; i++ {
		l.failed(fmt.Sprintf("10.0.%d.%d", i/250, i%250), "alice")
		if len(l.fails) > 50 {
			t.Fatalf("after %d failures the map holds %d keys", i+1, len(l.fails))
		}
	}
	*now = now.Add(time.Hour)
	l.failed("1.1.1.1", "x")
	l.sweep()
	if len(l.fails) > 2 {
		t.Errorf("expired entries must be swept, %d keys left", len(l.fails))
	}
}

func TestLimiterIgnoresHugeNames(t *testing.T) {
	l, _ := testLimiter()
	long := string(make([]byte, 5000))
	for i := 0; i < 5; i++ {
		l.failed("1.2.3.4", long+fmt.Sprint(i)) // differ only after the cut-off: one key
	}
	if ok, _ := l.allow("1.2.3.4", long); ok {
		t.Error("names longer than any real name must share one counter")
	}
}

func TestHashGate(t *testing.T) {
	g := newHashGate(2)
	r1, ok1 := g.acquire(context.Background())
	r2, ok2 := g.acquire(context.Background())
	if !ok1 || !ok2 {
		t.Fatal("two slots must be free")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, ok := g.acquire(ctx); ok {
		t.Error("a third caller must give up when its time runs out")
	}
	r1()
	if r, ok := g.acquire(context.Background()); !ok {
		t.Error("a freed slot must be reusable")
	} else {
		r()
	}
	r2()
}
