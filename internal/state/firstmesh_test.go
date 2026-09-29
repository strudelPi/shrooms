package state

import (
	"path/filepath"
	"strings"
	"testing"
)

// What a joining device calls the mesh (docs/one-kind-of-mesh.md, 2026-09-29).
// The fallback always exists: by the time the suggestion is read the invite
// has been spent, so failing then would spend it for nothing.
func TestChooseMeshLabel(t *testing.T) {
	taken := map[string]bool{"home": true, "mesh-abc123": true}
	isTaken := func(l string) bool { return taken[l] }
	for _, tc := range []struct {
		name, asked, suggested, id string
		want, from                 string
	}{
		{"asked wins", "work", "office", "abc123def", "work", LabelAsked},
		{"the inviter's name", "", "office", "abc123def", "office", LabelInvite},
		{"default is not a name", "", "default", "xyz789", "mesh-xyz789", LabelFallback},
		{"not a label", "", "Office Net", "xyz789", "mesh-xyz789", LabelFallback},
		{"too long", "", strings.Repeat("a", 64), "xyz789", "mesh-xyz789", LabelFallback},
		{"already used here", "", "home", "xyz789", "mesh-xyz789", LabelFallback},
		{"nothing suggested", "", "", "q2w3e4r5", "mesh-q2w3e4", LabelFallback},
		{"fallback taken too", "", "", "abc123zz", "mesh-abc123-2", LabelFallback},
		{"no usable id", "", "", "!!!", "mesh", LabelFallback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, from := ChooseMeshLabel(tc.asked, tc.suggested, tc.id, isTaken)
			if got != tc.want || from != tc.from {
				t.Errorf("got %q (%s), want %q (%s)", got, from, tc.want, tc.from)
			}
			if err := ValidMeshLabel(got); err != nil {
				t.Errorf("chose a name that is not a label: %v", err)
			}
		})
	}
}

// A first mesh written this way loads as the named mesh that holds the base
// identity, pinned to the device's interface and port, and keeps what the
// top-level fields said about it.
func TestWithFirstMesh(t *testing.T) {
	c := DefaultConfig()
	c.Interface, c.ListenPort = "shrooms0", 51999
	c.NetworkKey = KeyPlaceholder
	c.Services = []string{"immich:2283"}
	c.AnnounceBound = true

	out := c.WithFirstMesh("office", Mesh{NetworkKey: testKey})
	if out.NetworkKey != "" || len(out.Services) != 0 || out.AnnounceBound {
		t.Errorf("mesh settings left in the top-level fields: %+v", out)
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteConfig(path, out); err != nil {
		t.Fatal(err)
	}
	back, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	ms := back.Meshes()
	if len(ms) != 1 || ms[0].Label != "office" {
		t.Fatalf("got %+v", ms)
	}
	m := ms[0]
	if !m.InheritsIdentity {
		t.Error("lost the base identity: the daemon would derive new keys for it")
	}
	if m.Interface != "shrooms0" || m.ListenPort != 51999 {
		t.Errorf("not pinned to the device's own: %s:%d", m.Interface, m.ListenPort)
	}
	if len(m.Services) != 1 || !m.AnnounceBound {
		t.Errorf("dropped what the top-level fields said: %+v", m)
	}
}

// The mesh package learns its own name only through ForMesh.
func TestForMeshCarriesTheLabel(t *testing.T) {
	c := DefaultConfig()
	if got := c.ForMesh(Mesh{Label: "office"}, 51820).MeshLabel; got != "office" {
		t.Errorf("MeshLabel = %q", got)
	}
}
