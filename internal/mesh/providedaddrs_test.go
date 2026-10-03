package mesh

import (
	"net/netip"
	"testing"

	"github.com/vpavlin/shrooms/internal/v4"
)

// Android refuses net.Interfaces to untrusted apps, so a phone announced no
// endpoints and could be reached only by a peer that had already heard from
// it — which, for a phone on cellular and a laptop behind a home router's
// IPv6 firewall, meant neither end's first packet ever arrived at the other.
// The app can read its addresses from the system instead and hand them
// over; what it hands over must be announced, filtered like anything we
// enumerate, and replaced wholesale when the network changes.

func announcesAddr(addrs []netip.Addr, want string) bool {
	w := netip.MustParseAddr(want)
	for _, a := range addrs {
		if a == w {
			return true
		}
	}
	return false
}

func TestProvidedAddressesAreAnnounced(t *testing.T) {
	t.Cleanup(func() { ProvideLocalAddrs(nil) })
	ProvideLocalAddrs([]netip.Addr{
		netip.MustParseAddr("2001:db8:1::7"), // cellular IPv6: the case this is for
		netip.MustParseAddr("10.212.0.5"),    // and the carrier's private v4, worth a try
	})

	got := localAddrs()
	for _, want := range []string{"2001:db8:1::7", "10.212.0.5"} {
		if !announcesAddr(got, want) {
			t.Errorf("provided %s is not among our addresses: %v", want, got)
		}
	}
}

// The same filter as enumeration, or the phone would announce its own overlay
// address and link-local ones that peers cannot use — the exact circularity
// localAddrs already guards against.
func TestProvidedAddressesAreFilteredLikeEnumeratedOnes(t *testing.T) {
	t.Cleanup(func() { ProvideLocalAddrs(nil) })
	ProvideLocalAddrs([]netip.Addr{
		netip.MustParseAddr("fe80::1"),            // link-local
		netip.MustParseAddr("::1"),                // loopback
		netip.MustParseAddr("fd00:1234:5678::1"),  // our overlay
		v4.Prefix.Addr(),                          // a synthetic IPv4 (ADR-021)
		netip.MustParseAddr("::"),                 // unspecified
		netip.MustParseAddr("::ffff:203.0.113.9"), // v4-mapped: kept, as v4
		netip.MustParseAddr("2001:db8:1::7"),
	})

	got := localAddrs()
	for _, bad := range []string{"fe80::1", "::1", "fd00:1234:5678::1", v4.Prefix.Addr().String(), "::"} {
		if announcesAddr(got, bad) {
			t.Errorf("%s should have been filtered out of %v", bad, got)
		}
	}
	for _, want := range []string{"203.0.113.9", "2001:db8:1::7"} {
		if !announcesAddr(got, want) {
			t.Errorf("%s should have survived the filter: %v", want, got)
		}
	}
}

// Between networks a phone has none; on the next one it has different ones.
// Keeping the old list would announce the last network's addresses, which is
// the one place we are known not to be.
func TestProvidedAddressesAreReplacedNotAccumulated(t *testing.T) {
	t.Cleanup(func() { ProvideLocalAddrs(nil) })

	ProvideLocalAddrs([]netip.Addr{netip.MustParseAddr("2001:db8:1::7")})
	ProvideLocalAddrs([]netip.Addr{netip.MustParseAddr("2001:db8:2::25")})
	got := localAddrs()
	if announcesAddr(got, "2001:db8:1::7") {
		t.Errorf("the previous network's address is still announced: %v", got)
	}
	if !announcesAddr(got, "2001:db8:2::25") {
		t.Errorf("the current network's address is missing: %v", got)
	}

	ProvideLocalAddrs(nil)
	got = localAddrs()
	for _, gone := range []string{"2001:db8:1::7", "2001:db8:2::25"} {
		if announcesAddr(got, gone) {
			t.Errorf("%s is still announced after the host said there are none: %v", gone, got)
		}
	}
}

// A host that can both enumerate and provide — a laptop, or an older Android —
// must not announce an address twice; the announce has four slots and a
// duplicate costs a real one.
func TestProvidedAddressesDoNotDuplicateEnumeratedOnes(t *testing.T) {
	t.Cleanup(func() { ProvideLocalAddrs(nil) })
	ProvideLocalAddrs(nil)
	base := localAddrs()
	if len(base) == 0 {
		t.Skip("no local addresses to duplicate on this machine")
	}

	ProvideLocalAddrs([]netip.Addr{base[0]})
	n := 0
	for _, a := range localAddrs() {
		if a == base[0] {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%s appears %d times after being provided as well as enumerated", base[0], n)
	}
}
