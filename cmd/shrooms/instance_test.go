package main

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/vpavlin/shrooms/internal/state"
)

// fixed stands in for mesh.Lookup and must answer the way it does, or these
// tests agree with nothing. It resolves a bare device name, and a service on a
// device as the label to its right — and, like the real one, refuses a name
// with trailing labels it cannot account for. An exact-match map instead of
// this is what let the fall-through bug pass its own guard test.
func fixed(addrs map[string]string) func(string) (netip.Addr, bool) {
	return func(host string) (netip.Addr, bool) {
		labels := strings.Split(host, ".")
		if len(labels) == 1 {
			if s, ok := addrs[labels[0]]; ok {
				return netip.MustParseAddr(s), true
			}
			return netip.Addr{}, false
		}
		if s, ok := addrs[labels[1]]; ok && len(labels) == 2 {
			return netip.MustParseAddr(s), true
		}
		return netip.Addr{}, false
	}
}

// A node with one mesh answers the qualified name and nothing shorter. It used
// to answer peer.mesh too, and that is what the decision of 2026-09-29 removed:
// a tablet whose only mesh was "default" answered peer.mesh while every
// device on more than one mesh used peer.office.mesh, and neither worked on
// the other (docs/one-kind-of-mesh.md).
func TestOneMeshAnswersOnlyTheQualifiedName(t *testing.T) {
	lookup := resolveAcross([]namedMesh{
		{label: "office", lookup: fixed(map[string]string{"vps": "fd00::1"})},
	})
	if addr, ok := lookup("vps.office"); !ok || addr.String() != "fd00::1" {
		t.Errorf("vps.office resolved to %v (%v)", addr, ok)
	}
	if addr, ok := lookup("vps"); ok {
		t.Errorf("the short form resolved, to %v", addr)
	}
}

// The whole point of qualifying: two meshes, both with a "vps".
func TestQualifiedNamePicksTheMesh(t *testing.T) {
	lookup := resolveAcross([]namedMesh{
		{label: "home", lookup: fixed(map[string]string{"vps": "fd00::1", "nas": "fd00::2"})},
		{label: "shared", lookup: fixed(map[string]string{"vps": "fd11::1"})},
	})

	if addr, ok := lookup("vps.home"); !ok || addr.String() != "fd00::1" {
		t.Errorf("vps.home resolved to %v (%v)", addr, ok)
	}
	if addr, ok := lookup("vps.shared"); !ok || addr.String() != "fd11::1" {
		t.Errorf("vps.shared resolved to %v (%v)", addr, ok)
	}
	// Neither a name on both meshes nor a name on one resolves unqualified.
	// Both did before: the first mesh in the list answered, which is how the
	// same name came to mean different machines on different devices.
	for _, short := range []string{"vps", "nas"} {
		if addr, ok := lookup(short); ok {
			t.Errorf("%s resolved unqualified, to %v", short, addr)
		}
	}
}

// A qualified name naming a mesh must not fall through to a device of the same
// name on another mesh — that is the one answer that is certainly wrong.
func TestQualifiedNameDoesNotFallThrough(t *testing.T) {
	lookup := resolveAcross([]namedMesh{
		{label: "home", lookup: fixed(map[string]string{"vps": "fd00::1"})},
		{label: "shared", lookup: fixed(map[string]string{})},
	})
	if addr, ok := lookup("vps.shared"); ok {
		t.Errorf("vps.shared resolved to %v, but shared has no vps", addr)
	}
	// And a mesh nobody has joined resolves nothing.
	if _, ok := lookup("vps.elsewhere"); ok {
		t.Error("resolved a name on a mesh this node is not in")
	}
	// The reported case: a mesh the config knows but which is not running,
	// because it was switched off, so it is absent from the slice. It used to
	// fall through to a second pass that handed the whole host to every mesh
	// and got back "vps" on home — `ssh vps.work.mesh` silently reached a
	// different machine. There is no second pass now.
	if addr, ok := lookup("vps.work"); ok {
		t.Errorf("vps.work resolved to %v with the work mesh switched off", addr)
	}
	// A service on a device resolves by its mesh, like everything else.
	if addr, ok := lookup("immich.vps.home"); !ok || addr.String() != "fd00::1" {
		t.Errorf("immich.vps.home resolved to %v (%v)", addr, ok)
	}
	if addr, ok := lookup("immich.vps"); ok {
		t.Errorf("a service with no mesh resolved, to %v", addr)
	}
}

func TestAliasAcrossMeshes(t *testing.T) {
	a := netip.MustParseAddr("fd00::1")
	b := netip.MustParseAddr("fd11::1")
	alias := aliasAcross([]namedMesh{
		{label: "home", alias: func(x netip.Addr) (netip.Addr, bool) {
			return netip.MustParseAddr("198.18.0.1"), x == a
		}},
		{label: "shared", alias: func(x netip.Addr) (netip.Addr, bool) {
			return netip.MustParseAddr("198.19.0.1"), x == b
		}},
	})
	if got, ok := alias(b); !ok || got.String() != "198.19.0.1" {
		t.Errorf("alias for the second mesh is %v (%v)", got, ok)
	}
	if _, ok := alias(netip.MustParseAddr("fd22::9")); ok {
		t.Error("invented an alias for an address in no mesh")
	}
}

// The first mesh keeps exactly the interface and port the config names, so a
// node that has always had one is untouched by any of this.
func TestFirstMeshKeepsTheConfiguredInterface(t *testing.T) {
	cfg := state.Config{
		Interface: "shrooms0", ListenPort: 51820, NetworkKey: "first",
		MeshSet: map[string]state.Mesh{"zzz": {NetworkKey: "second"}},
	}
	got := cfg.Meshes() // "default" sorts before "zzz"
	if len(got) != 2 {
		t.Fatalf("got %d meshes", len(got))
	}
	if got[0].Interface != "shrooms0" || got[0].ListenPort != 51820 {
		t.Errorf("first mesh got %s:%d", got[0].Interface, got[0].ListenPort)
	}
	if got[1].Interface != "shrooms01" || got[1].ListenPort != 51821 {
		t.Errorf("second mesh got %s:%d", got[1].Interface, got[1].ListenPort)
	}
}

// A bound port is reached as a name qualified by the mesh it is bound on —
// the only form that resolves. It once printed the short form, which named an
// address on the first mesh for a port bound on another, where nothing was
// listening. Driven through boundName, which is what `shrooms bound` prints.
func TestBoundNameCarriesTheMeshLabel(t *testing.T) {
	for _, tc := range []struct {
		name, host, label, want string
	}{
		{"one mesh is still qualified", "laptop", "office", "laptop.office.mesh"},
		{"a second mesh", "laptop", "test", "laptop.test.mesh"},
		{"a name needing sanitising", "Living Room NAS", "home", "living-room-nas.home.mesh"},
		{"no mesh, no name", "laptop", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := boundName(tc.host, tc.label); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The fingerprint must be stable when nothing has changed, or the daemon reads
// its own polling as a network change and restarts on a timer.
func TestLocalUnderlayIsStable(t *testing.T) {
	a := localUnderlay(nil)
	if b := localUnderlay(nil); a != b {
		t.Errorf("fingerprint moved with nothing changing:\n%q\n%q", a, b)
	}
}

// The mesh's own interfaces must be excluded. They are torn down and rebuilt by
// the restart this feeds, so counting them would make recovery look like a
// fresh network change and restart again — a loop, on a machine that had one
// wifi blip.
func TestLocalUnderlayIgnoresOurOwnInterfaces(t *testing.T) {
	all := localUnderlay(nil)
	if all == "" {
		t.Skip("no non-loopback interfaces with addresses here")
	}
	// Whichever interface the first entry belongs to, claim it as a mesh
	// device: its addresses must then disappear from the fingerprint.
	name, _, ok := strings.Cut(strings.Split(all, ",")[0], "=")
	if !ok {
		t.Fatalf("unexpected fingerprint shape: %q", all)
	}
	got := localUnderlay([]*instance{{iface: name}})
	if strings.Contains(got, name+"=") {
		t.Errorf("interface %q is ours and still counted:\n%q", name, got)
	}
	if got == all {
		t.Errorf("excluding %q changed nothing:\n%q", name, got)
	}
}
