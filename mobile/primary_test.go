package mobile

import (
	"net/netip"
	"testing"

	"github.com/vpavlin/shrooms/internal/identity"
	"github.com/vpavlin/shrooms/internal/state"
)

// A first mesh stopped living in the config's top-level fields on 2026-09-29,
// and three answers the app needs before it can connect still read them. A
// pocophone joined successfully and then could not connect: "no overlay
// address; config unreadable". These drive the real functions on a config
// written the way every device now writes one.
func TestTheAppCanConnectOnANamedFirstMesh(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init("pocophone", "office", dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	cfg, st, err := load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NetworkKey != "" {
		t.Fatalf("this test needs the named shape; the config still has a top-level key")
	}
	m := cfg.Active()[0]
	nk, err := m.Key()
	if err != nil {
		t.Fatal(err)
	}
	// The first mesh keeps the device's own keys, so this is the address it
	// announces — and the one the tunnel must be given.
	want := identity.OverlayAddr(nk, st.Identity.DevicePub).String()

	if got := OverlayAddress(dir); got != want {
		t.Errorf("OverlayAddress = %q, want %q", got, want)
	}
	v4, err := netip.ParseAddr(OverlayV4(dir))
	if err != nil || !v4.Is4() {
		t.Errorf("OverlayV4 = %q, want an IPv4 alias", OverlayV4(dir))
	}
	if DNSAddress(dir) == "" {
		t.Error("DNSAddress is empty, so mesh names would not resolve")
	}
}

// The old shape still works: a phone that never migrated must connect exactly
// as it did.
func TestTheAppStillConnectsOnTheOldShape(t *testing.T) {
	dir := t.TempDir()
	cfgPath, stateDir := paths(dir)
	nk, _ := identity.NewNetworkKey()
	cfg := phoneDefaults()
	cfg.Name = "phone"
	cfg.NetworkKey = nk.String()
	if err := state.WriteConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	st, err := state.LoadOrCreateState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := OverlayAddress(dir), identity.OverlayAddr(nk, st.Identity.DevicePub).String(); got != want {
		t.Errorf("OverlayAddress = %q, want %q", got, want)
	}
	if OverlayV4(dir) == "" || DNSAddress(dir) == "" {
		t.Error("the old shape lost its IPv4 alias or resolver address")
	}
}

// Asking must not change the answer: these run before anything has started,
// and State.MeshState would create an entry and derive an identity.
func TestAskingForTheOverlayWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init("pocophone", "office", dir); err != nil {
		t.Fatal(err)
	}
	_, before, _ := load(dir)
	n := len(before.Meshes)
	OverlayAddress(dir)
	OverlayV4(dir)
	DNSAddress(dir)
	_, after, _ := load(dir)
	if len(after.Meshes) != n {
		t.Errorf("asking created state: %d mesh entries before, %d after", n, len(after.Meshes))
	}
}
