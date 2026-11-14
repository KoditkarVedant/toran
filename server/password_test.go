package main

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const testIterations = 1000 // real hashes use defaultIterations; tests stay fast

// Guards the algorithm itself: RFC 7914 section 11 test vector for PBKDF2-HMAC-SHA-256.
func TestPBKDF2Vector(t *testing.T) {
	key, err := pbkdf2.Key(sha256.New, "passwd", []byte("salt"), 1, 64)
	if err != nil {
		t.Fatal(err)
	}
	const want = "55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc49ca9cccf179b645991664b39d77ef317c71b845b1e30bd509112041d3a19783"
	if got := hex.EncodeToString(key); got != want {
		t.Errorf("PBKDF2-HMAC-SHA256 = %s", got)
	}
}

func TestHashAndVerify(t *testing.T) {
	h, err := hashPassword("correct horse battery", testIterations)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$1000$") || strings.Count(h, "$") != 3 {
		t.Errorf("unexpected stored form %q", h)
	}
	if ok, up := verifyPassword("correct horse battery", h, testIterations); !ok || up {
		t.Errorf("right password: ok=%v upgrade=%v", ok, up)
	}
	for _, bad := range []string{"correct horse batter", "", "Correct horse battery", "correct horse battery "} {
		if ok, _ := verifyPassword(bad, h, testIterations); ok {
			t.Errorf("%q must not verify", bad)
		}
	}
}

func TestSaltsDiffer(t *testing.T) {
	a, _ := hashPassword("same password here", testIterations)
	b, _ := hashPassword("same password here", testIterations)
	if a == b {
		t.Error("two hashes of one password must differ (random salt)")
	}
}

func TestUpgradeWhenIterationsAreRaised(t *testing.T) {
	old, _ := hashPassword("an old password", 500)
	ok, up := verifyPassword("an old password", old, testIterations)
	if !ok || !up {
		t.Errorf("older hash: ok=%v upgrade=%v, want true true", ok, up)
	}
	if ok, up := verifyPassword("wrong password!!", old, testIterations); ok || up {
		t.Error("a wrong password must neither verify nor ask for an upgrade")
	}
}

func TestMalformedHashesNeverMatch(t *testing.T) {
	good, _ := hashPassword("a good password", testIterations)
	parts := strings.Split(good, "$")
	for name, bad := range map[string]string{
		"empty":           "",
		"plain text":      "a good password",
		"wrong scheme":    "md5$1000$" + parts[2] + "$" + parts[3],
		"missing part":    "pbkdf2-sha256$1000$" + parts[2],
		"extra part":      good + "$x",
		"zero iterations": "pbkdf2-sha256$0$" + parts[2] + "$" + parts[3],
		"negative":        "pbkdf2-sha256$-5$" + parts[2] + "$" + parts[3],
		"absurd count":    "pbkdf2-sha256$999999999999$" + parts[2] + "$" + parts[3],
		"not a number":    "pbkdf2-sha256$lots$" + parts[2] + "$" + parts[3],
		"bad salt":        "pbkdf2-sha256$1000$!!!$" + parts[3],
		"empty salt":      "pbkdf2-sha256$1000$$" + parts[3],
		"short hash":      "pbkdf2-sha256$1000$" + parts[2] + "$AAAA",
		"hash not base64": "pbkdf2-sha256$1000$" + parts[2] + "$@@@@",
		"too many rounds": "pbkdf2-sha256$10000001$" + parts[2] + "$" + parts[3],
	} {
		if ok, _ := verifyPassword("a good password", bad, testIterations); ok {
			t.Errorf("%s: %q must not verify", name, bad)
		}
	}
}

func TestHugePasswordIsRefusedBeforeHashing(t *testing.T) {
	h, _ := hashPassword("a good password", testIterations)
	if ok, _ := verifyPassword(strings.Repeat("a", maxPassword+1), h, testIterations); ok {
		t.Error("an over-long password must not verify")
	}
}

func TestCheckPassword(t *testing.T) {
	good := []string{"abcdefghij", "correct horse battery staple", strings.Repeat("x", maxPassword), "pässwörd-ünïcode"}
	for _, p := range good {
		if err := checkPassword("vedant", p); err != nil {
			t.Errorf("%q refused: %v", p, err)
		}
	}
	bad := map[string]string{"short": "abcdefghi", "empty": "", "long": strings.Repeat("x", maxPassword+1), "name": "VEDANT-ADMIN"}
	for what, p := range bad {
		name := "vedant"
		if what == "name" {
			name = "vedant-admin"
		}
		if err := checkPassword(name, p); err == nil {
			t.Errorf("%s: %q should be refused", what, p)
		}
	}
}
