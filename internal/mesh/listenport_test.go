package mesh

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

// After the daemon moves this mesh to a fresh port (docs/stale-tether-nat.md),
// what we announce must name the new port and nothing peers saw of the old
// one — and peers must be told now, not at the next tick.
func TestAMovedPortIsWhatWeAnnounce(t *testing.T) {
	f := newRelayFixture(t)
	m := f.m
	m.reannounce = make(chan struct{}, 1)
	m.cfg.ListenPort = 51822
	now := time.Now()

	// Two observers agreeing, as a real observation of a NAT that maps the
	// same way for everybody looks — one alone would be dropped beside the
	// fixture's own, by the rule that peers who disagree are not believed.
	old := netip.MustParseAddrPort("198.51.100.7:11482")
	m.prober.NoteReflexive(old, "vps", now)
	m.prober.NoteReflexive(old, "pi5", now)
	if !contains(m.candidates(), "198.51.100.7:11482") {
		t.Fatalf("the old observation was not announced to begin with; the test would prove nothing: %v", m.candidates())
	}

	m.SetListenPort(40123)

	if got := m.ListenPort(); got != 40123 {
		t.Errorf("ListenPort = %d after moving to 40123", got)
	}
	cands := m.candidates()
	for _, c := range cands {
		if c == "198.51.100.7:11482" {
			t.Errorf("still announcing what peers saw of the old port: %v", cands)
		}
		if strings.HasSuffix(c, ":51822") {
			t.Errorf("still announcing a local address on the old port: %v", cands)
		}
	}
	// Local addresses exist on any machine that runs this, CI included; each
	// must name the port we are on.
	for _, c := range cands {
		if ap, err := netip.ParseAddrPort(c); err == nil && ap.Addr().IsPrivate() &&
			ap.Port() != 40123 {
			t.Errorf("local candidate %s is not on the new port", c)
		}
	}
	select {
	case <-m.reannounce:
	default:
		t.Error("the move did not ask for an announce; peers would wait for the next tick")
	}

	// Moving to where we already are is not news.
	m.SetListenPort(40123)
	select {
	case <-m.reannounce:
		t.Error("a move to the same port asked for an announce")
	default:
	}
}
