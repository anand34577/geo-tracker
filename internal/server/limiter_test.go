package server

import "testing"

func TestLimiterIPCollapsesIPv6(t *testing.T) {
	a := limiterIP("2001:db8:1:2:aaaa:bbbb:cccc:dddd")
	b := limiterIP("2001:db8:1:2:1111:2222:3333:4444")
	if a != b {
		t.Fatalf("same /64 got different keys: %s vs %s", a, b)
	}
	if c := limiterIP("2001:db8:1:3::1"); c == a {
		t.Fatal("different /64 shared a key")
	}
	if got := limiterIP("203.0.113.7"); got != "203.0.113.7" {
		t.Fatalf("ipv4 changed: %s", got)
	}
}

func TestQuotasIndependentOfLogins(t *testing.T) {
	l := newLimiter()
	for i := 0; i < maxLimiterKeys; i++ {
		l.allow("ip:"+string(rune(i))+"x", 1, 15*60*1e9)
	}
	q := newLimiter()
	if !q.allow("ingest:1", 600, 60*1e9) {
		t.Fatal("a separate limiter must not be starved by a full one")
	}
}
