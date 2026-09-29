package hosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The failure this exists for, taken from a real machine: /etc/hosts said a
// peer was at an address it had not held for weeks, and because
// systemd-resolved answers from that file ahead of any registered resolver, the
// stale answer won. Nothing anywhere reported it.
func TestAMovedPeerIsReported(t *testing.T) {
	p := write(t, Begin+"\n"+
		"fd3b::old  nothing.office.mesh\n"+
		End+"\n")

	bad := Stale(p, []Entry{{Name: "nothing", Mesh: "office", Addr: "fd3b::new"}}, "mesh")
	if len(bad) != 1 {
		t.Fatalf("reported %d disagreements, want 1: %+v", len(bad), bad)
	}
	if bad[0].Has != "fd3b::old" || bad[0].Wants != "fd3b::new" {
		t.Errorf("got %+v", bad[0])
	}
}

// One device appears on several lines — its overlay address and its IPv4
// alias. Counting each as its own problem makes a single stale entry read as
// two, which misrepresents how much is wrong.
func TestNameVariantsCollapseToOneProblem(t *testing.T) {
	// Written by the real writer, so the test exercises what it produces
	// rather than a shape invented here.
	was := []Entry{{Name: "nas", Mesh: "home", Addr: "fd3b::old"}}
	p := write(t, Render(was, "mesh"))

	bad := Stale(p, []Entry{{Name: "nas", Mesh: "home", Addr: "fd3b::new"}}, "mesh")
	if len(bad) != 1 {
		t.Fatalf("one moved device produced %d problems: %+v", len(bad), bad)
	}
	if bad[0].Name != "nas.home.mesh" {
		t.Errorf("reported under %q, want its qualified name", bad[0].Name)
	}
}

// A block written before names were qualified still answers peer.mesh, which
// nothing else does any more. It is reported, so `status` says to rewrite it.
func TestAShortNameFromAnOlderBuildIsReported(t *testing.T) {
	p := write(t, Begin+"\n"+"fd3b::1  nas nas.mesh\n"+End+"\n")
	bad := Stale(p, []Entry{{Name: "nas", Mesh: "home", Addr: "fd3b::1"}}, "mesh")
	if len(bad) != 1 {
		t.Fatalf("reported %d, want 1: %+v", len(bad), bad)
	}
	if bad[0].Wants != "" {
		t.Errorf("a short name should want nothing, got %q", bad[0].Wants)
	}
}

// A block that agrees is silent. This is the common case and a warning that
// cried wolf would be worse than none.
func TestAnAgreeingBlockIsSilent(t *testing.T) {
	entries := []Entry{{Name: "nas", Mesh: "home", Addr: "fd3b::1", AddrV4: "198.19.0.1"}}
	p := write(t, Render(entries, "mesh"))
	if !strings.Contains(string(mustRead(t, p)), "nas.home.mesh") {
		t.Fatal("the writer produced nothing to compare, so this would pass vacuously")
	}
	if bad := Stale(p, entries, "mesh"); len(bad) != 0 {
		t.Errorf("a freshly written block reported as stale: %+v", bad)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A name the mesh no longer serves at all — a peer that left, or a block
// written under a different suffix. Still worth reporting: the file is still
// answering for it.
func TestANameTheMeshNoLongerServesIsReported(t *testing.T) {
	p := write(t, Begin+"\n"+"fd3b::1  ghost ghost.mesh\n"+End+"\n")
	bad := Stale(p, []Entry{{Name: "nas", Mesh: "home", Addr: "fd3b::2"}}, "mesh")
	if len(bad) != 1 {
		t.Fatalf("reported %d, want 1: %+v", len(bad), bad)
	}
	if bad[0].Wants != "" {
		t.Errorf("a departed peer should want nothing, got %q", bad[0].Wants)
	}
}

// The self entry deliberately omits the bare name, because it would shadow the
// machine's own hostname — a host resolves that to 127.0.1.1 and daemons expect
// a local address there. A block from before that rule still carries it, and
// that is worth reporting rather than treating as cosmetic.
func TestABareSelfNameLeftByAnOlderBuildIsReported(t *testing.T) {
	p := write(t, Begin+"\n"+"fd3b::1  laptop laptop.mesh\n"+End+"\n")
	bad := Stale(p, []Entry{{Name: "laptop", Mesh: "home", Addr: "fd3b::1", Self: true}}, "mesh")
	if len(bad) != 1 {
		t.Fatalf("reported %d, want 1: %+v", len(bad), bad)
	}
	if bad[0].Name != "laptop" {
		t.Errorf("reported %q, want the bare name", bad[0].Name)
	}
}

// No file, no block, no problem. A machine that has never had one managed must
// not be told anything is wrong with it.
func TestNoFileAndNoBlockAreQuiet(t *testing.T) {
	if bad := Stale(filepath.Join(t.TempDir(), "absent"), nil, "mesh"); bad != nil {
		t.Errorf("a missing file reported %+v", bad)
	}
	p := write(t, "127.0.0.1 localhost\n")
	if bad := Stale(p, []Entry{{Name: "nas", Addr: "fd3b::1"}}, "mesh"); bad != nil {
		t.Errorf("a file with no managed block reported %+v", bad)
	}
}
