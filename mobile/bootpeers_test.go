package mobile

import (
	"reflect"
	"testing"
	"time"

	"github.com/vpavlin/shrooms/internal/invite"
	"github.com/vpavlin/shrooms/internal/state"
)

const vpsBoot = "/ip4/128.140.55.128/tcp/30304/p2p/16Uiu2HAmNQQC8L8T55KoLiCRD75bSDehn2evQ1vEEG8test"

// The phone never used the delivery addresses mesh Core nodes publish. On
// 2026-10-01, with five of six public fleet nodes down after the v0.39 rollout,
// desktops got back in through the VPS's own delivery node while a phone that
// had been told the same address kept dialling only the dead public list.
// These drive the functions the phone builds its node from.
func TestThePhoneStartsFromAddressesItLearned(t *testing.T) {
	dir := t.TempDir()
	cfgPath, stateDir := paths(dir)
	if _, err := Init("phone", "office", dir); err != nil {
		t.Fatal(err)
	}
	st, err := state.LoadOrCreateState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.NoteBootPeer(vpsBoot, time.Now()); err != nil {
		t.Fatal(err)
	}

	got := sharedNodeConfig(cfgPath)["entryNodes"]
	if !reflect.DeepEqual(got, []string{vpsBoot}) {
		t.Errorf("entryNodes = %v, want the learned VPS address", got)
	}

	// Start builds its own node the same way.
	cfg, st2, err := load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := nodeConfig(cfg, learnedBootPeers(st2)...)["entryNodes"]; !reflect.DeepEqual(got, []string{vpsBoot}) {
		t.Errorf("Start's entryNodes = %v, want the learned VPS address", got)
	}
}

// A new phone has learned nothing; the invite's boot address is all it has.
func TestANewPhoneStartsFromTheInvitesAddress(t *testing.T) {
	dir := t.TempDir()
	cfgPath, _ := paths(dir)

	secret, err := invite.New()
	if err != nil {
		t.Fatal(err)
	}
	token := secret.URIWithBoot(vpsBoot)
	if got := sharedNodeConfig(cfgPath, invite.BootFromToken(token))["entryNodes"]; !reflect.DeepEqual(got, []string{vpsBoot}) {
		t.Errorf("entryNodes = %v, want the invite's address", got)
	}
}

// Nothing learned and no hint: no entryNodes at all, so the library uses the
// preset's fleet exactly as before. Adding ours must never be the only way in.
func TestWithNothingLearnedThePresetIsUntouched(t *testing.T) {
	dir := t.TempDir()
	cfgPath, _ := paths(dir)
	c := sharedNodeConfig(cfgPath, "")
	if _, set := c["entryNodes"]; set {
		t.Errorf("entryNodes set to %v with nothing learned; the preset should stand alone", c["entryNodes"])
	}
}
