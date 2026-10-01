package mesh

import (
	"testing"
	"time"
)

// What any node's status puts in front of you about credentials. Built from
// Members(), whose first entry is this device.
func TestDueListsTheSoonFirstAndOnlyThoseDue(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	day := 24 * time.Hour
	members := []Member{
		{Name: "laptop", NotAfter: now.Add(25 * day)}, // this device, fine
		{Name: "vps", NotAfter: now.Add(6 * day)},     // due
		{Name: "k11", NotAfter: now.Add(-2 * day)},    // lapsed
		{Name: "pi5", NotAfter: now.Add(20 * day)},    // fine
		{Name: "noauth"}, // no authority: never due
		{Name: "edge", NotAfter: now.Add(DueWithin)},               // exactly at the window: due
		{Name: "past", NotAfter: now.Add(DueWithin + time.Second)}, // just outside
	}
	got := DueAmong("home", members, now)
	var names []string
	for _, d := range got {
		names = append(names, d.Name)
	}
	want := []string{"k11", "vps", "edge"}
	if len(names) != len(want) {
		t.Fatalf("due = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("due = %v, want %v (soonest first)", names, want)
		}
	}
	if !got[0].Expired || got[1].Expired {
		t.Errorf("expired flags wrong: %+v", got)
	}
	if got[1].Fix != "shrooms admin renew --mesh home" {
		t.Errorf("fix = %q", got[1].Fix)
	}
}

func TestThisDeviceIsMarkedWhenItIsDue(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	got := DueAmong("office", []Member{{Name: "phone", NotAfter: now.Add(3 * 24 * time.Hour)}}, now)
	if len(got) != 1 || !got[0].Self {
		t.Errorf("this device's own expiry was not marked as its own: %+v", got)
	}
}

// From a real mesh's own bookkeeping, not a hand-made list: a peer whose
// credential expiry was recorded the way an accepted announce records it.
func TestDueComesFromTheMeshItself(t *testing.T) {
	f := newRelayFixture(t)
	now := f.now
	f.m.cfg.Name = "laptop"
	st := newAdviceFixture(t, f.m.cfg).m.st // a real state, so Members can read this device's identity
	f.m.st = st
	f.m.mu.Lock()
	if f.m.expiry == nil {
		f.m.expiry = map[string]int64{}
	}
	f.m.expiry[f.relayID] = now.Add(4 * 24 * time.Hour).Unix()
	f.m.mu.Unlock()

	got := DueAmong("office", f.m.Members(), now)
	if len(got) != 1 || got[0].Name != "vps" || got[0].Self {
		t.Fatalf("due from the mesh = %+v, want the peer named vps", got)
	}
}
