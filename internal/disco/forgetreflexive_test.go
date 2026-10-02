package disco

import (
	"net/netip"
	"testing"
	"time"
)

// After our socket moves, what peers saw of us was the old port. Announcing it
// would send them to the one path known not to lead here.
func TestForgetReflexiveDropsWhatPeersSaw(t *testing.T) {
	p := NewProber(Key{}, nil, func([]byte, netip.AddrPort) error { return nil })
	now := time.Now()
	p.NoteReflexive(netip.MustParseAddrPort("198.51.100.7:11349"), "vps", now)
	if len(p.Reflexive(now)) != 1 {
		t.Fatal("the observation was not recorded; the test would prove nothing")
	}
	p.ForgetReflexive()
	if got := p.Reflexive(now); len(got) != 0 {
		t.Errorf("still reporting %v after forgetting", got)
	}
	// And it keeps learning afterwards.
	p.NoteReflexive(netip.MustParseAddrPort("198.51.100.7:11403"), "vps", now)
	if got := p.Reflexive(now); len(got) != 1 || got[0].Port() != 11403 {
		t.Errorf("after forgetting, a new observation gave %v", got)
	}
}
