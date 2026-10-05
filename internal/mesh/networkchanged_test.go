package mesh

import (
	"net/netip"
	"testing"
	"time"
)

// When the host says the network changed — a phone moving from Wi-Fi to
// cellular is the case — what peers observed of us belongs to the network we
// left, and peers must be told now rather than at the next tick: until they
// hear the new addresses, nothing they send reaches us.
func TestANetworkChangeDropsOldObservationsAndAnnounces(t *testing.T) {
	f := newRelayFixture(t)
	m := f.m
	m.reannounce = make(chan struct{}, 1)
	now := time.Now()

	// Two observers agreeing, as a real observation looks; one alone would be
	// dropped by the rule that peers who disagree are not believed.
	old := netip.MustParseAddrPort("198.51.100.7:11482")
	m.prober.NoteReflexive(old, "vps", now)
	m.prober.NoteReflexive(old, "pi5", now)
	if !contains(m.candidates(), "198.51.100.7:11482") {
		t.Fatalf("the old observation was not announced to begin with; the test would prove nothing: %v", m.candidates())
	}

	m.NetworkChanged()

	if cands := m.candidates(); contains(cands, "198.51.100.7:11482") {
		t.Errorf("still announcing what peers saw of us on the old network: %v", cands)
	}
	select {
	case <-m.reannounce:
	default:
		t.Error("the change did not ask for an announce; peers would wait for the next tick")
	}
}
