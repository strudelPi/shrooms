package main

import (
	"strings"
	"testing"
)

// A dual-stack laptop on its home LAN was told it was behind endpoint-dependent
// NAT and needed a relay. The two "distinct addresses" were its IPv4 and its
// IPv6, reported by the same peer on the same LAN — two families, one NAT
// mapping at most — on a node with no NAT problem at all.

func TestReflexiveNoteIgnoresOneAddressPerFamily(t *testing.T) {
	if got := reflexiveNote([]string{"192.168.7.22:51820", "[2001:db8::25]:51820"}); got != "" {
		t.Errorf("a dual-stack node was warned about NAT:\n%s", got)
	}
}

// Several addresses within one family is the real signal: each peer reaches
// us through a different port of the same NAT.
func TestReflexiveNoteCountsWithinAFamily(t *testing.T) {
	got := reflexiveNote([]string{"198.51.100.7:11482", "198.51.100.7:11490", "[2001:db8::25]:51820"})
	if !strings.Contains(got, "2 distinct IPv4 addresses") {
		t.Errorf("two IPv4 ports from one NAT were not called out:\n%s", got)
	}
	if strings.Contains(got, "IPv6") {
		t.Errorf("the lone IPv6 address was blamed as well:\n%s", got)
	}
}

// The port is what differs under endpoint-dependent NAT, so two ports on one
// address must count as two.
func TestReflexiveNoteCountsPortsOnOneAddress(t *testing.T) {
	got := reflexiveNote([]string{"198.51.100.7:11482", "198.51.100.7:11490"})
	if !strings.Contains(got, "2 distinct IPv4") {
		t.Errorf("two ports on one address were not counted as two:\n%s", got)
	}
}
