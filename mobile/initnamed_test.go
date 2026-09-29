package mobile

import (
	"strings"
	"testing"

	"github.com/vpavlin/shrooms/internal/state"
)

// "create one" on the join screen makes a named mesh holding the base
// identity, like every first mesh (docs/one-kind-of-mesh.md, 2026-09-29), and
// refuses "default", which is not a name.
func TestInitNamesTheMesh(t *testing.T) {
	dir := t.TempDir()
	key, err := Init("x6", "office", dir)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath, _ := paths(dir)
	cfg, err := state.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := cfg.MeshSet["office"]
	if !ok || cfg.NetworkKey != "" {
		t.Fatalf("not a named mesh: %+v", cfg.Meshes())
	}
	if m.NetworkKey != key {
		t.Error("returned a different key from the one written")
	}
	if !m.InheritsIdentity {
		t.Error("the first mesh does not hold the base identity")
	}

	if _, err := Init("x6", state.DefaultLabel, t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "not a mesh name") {
		t.Errorf("got %v, want default refused", err)
	}
}
