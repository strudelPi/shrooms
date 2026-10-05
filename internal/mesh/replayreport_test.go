package mesh

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/vpavlin/shrooms/internal/control"
	"github.com/vpavlin/shrooms/internal/identity"
)

// The report puts a device's mark beside what was rejected from it, so a mark
// that got ahead of the peer — everything rejected, nothing accepted, the mark
// above the newest number heard — reads off one line.
func TestReplayReportShowsAMarkAheadOfThePeer(t *testing.T) {
	nk, _ := identity.NewNetworkKey()
	self, _ := identity.New()
	ok, _ := identity.New()
	stuck, _ := identity.New()
	m := &Mesh{roster: NewRoster(nk, self.DevicePub), guard: control.NewReplayGuard()}
	now := time.Now()

	// A peer heard normally: accepted, its mark its number.
	a := newAnnounce(t, ok, "atlas", []string{"203.0.113.9:51820"}, 43006)
	if !m.guard.Accept(a) {
		t.Fatal("a first announce was rejected")
	}
	m.replays.accept(a.DevicePub, now.Add(-5*time.Second))
	m.roster.Apply(a, now)

	// A peer whose mark came from disk above anything it now sends.
	m.guard.Load(map[string]uint64{hex.EncodeToString(stuck.DevicePub): 50000})
	for _, seq := range []uint64{41519, 41665, 41602} {
		if m.guard.Accept(newAnnounce(t, stuck, "proteus", nil, seq)) {
			t.Fatalf("seq %d accepted under a mark of 50000", seq)
		}
		m.replays.reject(stuck.DevicePub, seq, now.Add(-time.Second))
	}

	r := m.ReplayReport(now)
	lines := strings.Split(strings.TrimSpace(r), "\n")
	if len(lines) != 2 {
		t.Fatalf("want a line per device, got:\n%s", r)
	}
	find := func(id *identity.Identity) string {
		for _, l := range lines {
			if strings.HasPrefix(l, hex.EncodeToString(id.DevicePub)[:16]) {
				return l
			}
		}
		t.Fatalf("no line for %x in:\n%s", id.DevicePub[:4], r)
		return ""
	}
	if l := find(ok); !strings.Contains(l, "atlas") || !strings.Contains(l, "mark=43006 accepted=1 (last 5s ago) rejected=0") {
		t.Errorf("the peer heard normally: %s", l)
	}
	if l := find(stuck); !strings.Contains(l, "mark=50000 accepted=0 (last never) rejected=3 (newest 41665, last 1s ago)") {
		t.Errorf("the peer with a mark ahead: %s", l)
	}
}
