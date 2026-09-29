package mesh

import (
	"testing"

	"github.com/vpavlin/shrooms/internal/identity"
	"github.com/vpavlin/shrooms/internal/state"
)

// The invite offers the joiner this device's name for the mesh, so both call
// it the same thing (docs/one-kind-of-mesh.md, 2026-09-29) — except "default",
// which is not a name but what the old unlabelled config shape was called.
// Passing it on would hand the joiner the one label nothing else answers to.
func TestTheInviteSuggestsTheMeshName(t *testing.T) {
	nk, err := identity.NewNetworkKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ label, want string }{
		{"office", "office"},
		{state.DefaultLabel, ""},
		{"", ""},
	} {
		m := &Mesh{nk: nk, cfg: state.DefaultConfig().ForMesh(state.Mesh{Label: tc.label}, 51820)}
		if got := m.inviteResponse().Label; got != tc.want {
			t.Errorf("a mesh called %q suggested %q, want %q", tc.label, got, tc.want)
		}
	}
}
