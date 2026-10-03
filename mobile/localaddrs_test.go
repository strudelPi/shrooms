package mobile

import (
	"testing"
)

// The app sends what ConnectivityManager gave it, as a comma-separated list.
// Whitespace, a zone suffix on a link-local address, an empty field and a
// value that is not an address at all must each cost only themselves.
func TestParseLocalAddressesTakesWhatTheAppSends(t *testing.T) {
	got := parseLocalAddresses(" 2a00:1450:4001:80b::1 ,10.212.0.5, fe80::1%wlan0,,not-an-address")
	want := []string{"2a00:1450:4001:80b::1", "10.212.0.5", "fe80::1"}
	if len(got) != len(want) {
		t.Fatalf("parsed %v, want %v", got, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("address %d = %s, want %s", i, got[i], want[i])
		}
	}
}

// Between networks the app has nothing to send, and that must mean "announce
// no local address" rather than "keep the last network's".
func TestParseLocalAddressesOfNothingIsNothing(t *testing.T) {
	for _, in := range []string{"", " ", ",", " , "} {
		if got := parseLocalAddresses(in); len(got) != 0 {
			t.Errorf("parseLocalAddresses(%q) = %v, want none", in, got)
		}
	}
}

// With no session running the addresses are still taken, so the first
// announce after Start names them; the count is what the app logs.
func TestNetworkChangedCountsWhatItTook(t *testing.T) {
	t.Cleanup(func() { NetworkChanged("") })
	if n := NetworkChanged("2a00:1450:4001:80b::1, bad, 10.212.0.5"); n != 2 {
		t.Errorf("NetworkChanged took %d addresses, want 2", n)
	}
}
