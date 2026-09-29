package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vpavlin/shrooms/internal/cred"
	"github.com/vpavlin/shrooms/internal/identity"
	"github.com/vpavlin/shrooms/internal/invite"
	"github.com/vpavlin/shrooms/internal/state"
)

// A device's first mesh is named and keeps the device's own keys
// (docs/one-kind-of-mesh.md, 2026-09-29).
//
// Found on a tablet that joined "office" by invite. The invite carried no
// name, so the tablet wrote the old top-level shape, its only mesh became
// "default", and it answered peer.mesh while every other device used
// peer.office.mesh. These drive both first-join paths — the waiting daemon's
// joinHere through a real invite exchange, and the command line's
// installFirstMesh — rather than a copy of either.

// inviter is the admitting side of an exchange: it opens each request and
// answers with a credential for the keys in it, the way a mesh member does.
type inviter struct {
	s     invite.Secret
	nk    identity.NetworkKey
	admin *cred.Admin
	auth  *cred.Authority
	label string
	msgs  chan invite.Message
}

func newInviter(t *testing.T, label string) *inviter {
	t.Helper()
	s, err := invite.New()
	if err != nil {
		t.Fatal(err)
	}
	nk, err := identity.NewNetworkKey()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := cred.NewAdmin()
	if err != nil {
		t.Fatal(err)
	}
	auth, err := cred.NewAuthority(admin.Pub)
	if err != nil {
		t.Fatal(err)
	}
	return &inviter{s: s, nk: nk, admin: admin, auth: auth, label: label, msgs: make(chan invite.Message, 8)}
}

func (i *inviter) Subscribe(string) error          { return nil }
func (i *inviter) Unsubscribe(string) error        { return nil }
func (i *inviter) Messages() <-chan invite.Message { return i.msgs }

func (i *inviter) Send(topic string, payload []byte, _ bool) (string, error) {
	req, err := invite.OpenRequest(i.s, payload, time.Now())
	if err != nil {
		return "", nil // not a request: nothing to answer
	}
	raw, err := cred.IssueFor(i.admin, i.auth, req.DevicePub, req.WGPub, req.SealPub,
		req.Name, 7, time.Now(), 30*24*time.Hour)
	if err != nil {
		return "", err
	}
	blob, err := invite.SealResponse(i.s, req.EphPub, i.response(raw))
	if err != nil {
		return "", err
	}
	i.msgs <- invite.Message{Topic: topic, Payload: blob}
	return "h", nil
}

func (i *inviter) response(credential []byte) *invite.Response {
	return &invite.Response{
		NetworkKey: i.nk[:],
		MeshID:     state.NetworkID(i.nk),
		AdminKeys:  [][]byte{i.admin.Pub},
		Credential: credential,
		Label:      i.label,
		Timestamp:  time.Now().Unix(),
	}
}

// checkFirstMesh asserts what every first join must leave behind.
func checkFirstMesh(t *testing.T, cfgPath, stateDir, want string, i *inviter) {
	t.Helper()
	cfg, err := state.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NetworkKey != "" {
		t.Errorf("wrote the old top-level shape: network_key = %q", cfg.NetworkKey)
	}
	m, ok := cfg.MeshSet[want]
	if !ok {
		t.Fatalf("no mesh called %q; have %v", want, cfg.Meshes())
	}
	if !m.InheritsIdentity {
		t.Error("the first mesh does not say it holds the base identity")
	}
	if len(m.AdminKeys) != 1 {
		t.Errorf("admin keys are not on the mesh entry: %v", m.AdminKeys)
	}
	auth, err := m.Authority()
	if err != nil || auth == nil || auth.ID() != i.auth.ID() {
		t.Fatalf("the mesh does not trust the inviting authority: %v", err)
	}

	// The regression that matters: loaded the way the daemon loads it, the
	// mesh announces with the device's base keys, and those are the keys its
	// credential names. With the flag unset it would derive fresh ones, beside
	// a credential naming the old — and every peer would refuse the device.
	st, err := state.LoadOrCreateState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	loaded := cfg.Meshes()[0]
	ms, err := st.MeshState(state.NetworkID(i.nk), loaded.InheritsIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ms.Identity.DevicePub, st.Identity.DevicePub) {
		t.Error("the mesh does not announce with the device's base identity")
	}
	c, err := cred.UnmarshalCredential(ms.Credential)
	if err != nil {
		t.Fatalf("no credential where the mesh looks for it: %v", err)
	}
	if !bytes.Equal(c.DevicePub, ms.Identity.DevicePub) {
		t.Error("the credential names keys the mesh does not announce with")
	}
}

func TestTheWaitingDaemonNamesTheFirstMesh(t *testing.T) {
	for _, tc := range []struct {
		name, suggested, asked, want string
	}{
		{"the inviter's name", "office", "", "office"},
		{"asked beats suggested", "office", "work", "work"},
		{"no suggestion", "", "", "fallback"},
		{"default is never adopted", "default", "", "fallback"},
		{"not a label", "Office Net", "", "fallback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.toml")
			stateDir := filepath.Join(dir, "state")
			st, err := state.LoadOrCreateState(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			i := newInviter(t, tc.suggested)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			res, err := joinHere(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), i,
				cfgPath, st, stateDir, i.s.String(), "", "x6", tc.asked, 51820, "", false)
			if err != nil {
				t.Fatalf("join: %v", err)
			}
			want := tc.want
			if want == "fallback" {
				if !strings.HasPrefix(res.Mesh, "mesh-") {
					t.Fatalf("named %q, want a name derived from the mesh id", res.Mesh)
				}
				want = res.Mesh
			}
			if res.Mesh != want {
				t.Fatalf("named %q, want %q", res.Mesh, want)
			}
			checkFirstMesh(t, cfgPath, stateDir, want, i)
		})
	}
}

// "default" is refused as a name before anything is exchanged, where refusing
// costs nothing — after the second round it would cost the invite.
func TestDefaultIsRefusedBeforeTheExchange(t *testing.T) {
	dir := t.TempDir()
	st, _ := state.LoadOrCreateState(filepath.Join(dir, "state"))
	i := newInviter(t, "office")
	_, err := joinHere(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), i,
		filepath.Join(dir, "config.toml"), st, filepath.Join(dir, "state"),
		i.s.String(), "", "x6", "default", 51820, "", false)
	if err == nil || !strings.Contains(err.Error(), "not a mesh name") {
		t.Fatalf("got %v, want a refusal of the name", err)
	}
	select {
	case <-i.msgs:
		t.Error("the exchange ran anyway")
	default:
	}
}

// The command line's half: what it writes once the exchange has answered.
func TestTheCommandLineNamesTheFirstMesh(t *testing.T) {
	quiet(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	stateDir := filepath.Join(dir, "state")
	st, err := state.LoadOrCreateState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	i := newInviter(t, "office")
	raw, err := cred.IssueFor(i.admin, i.auth, st.Identity.DevicePub, st.Identity.WGPub[:],
		st.Identity.SealPub[:], "x6", 7, time.Now(), 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := installFirstMesh(cfgPath, stateDir, st, i.response(raw), "", "x6",
		51820, "", false, nil); err != nil {
		t.Fatal(err)
	}
	checkFirstMesh(t, cfgPath, stateDir, "office", i)
}

// `key show` printed the top-level field, which a named first mesh leaves
// empty — so it printed a blank line on every device set up since.
func TestKeyShowFindsTheNamedMeshsKey(t *testing.T) {
	nk, _ := identity.NewNetworkKey()
	nk2, _ := identity.NewNetworkKey()
	one := state.DefaultConfig().WithFirstMesh("office", state.Mesh{NetworkKey: nk.String()})
	if got, err := meshKeyFor(one, ""); err != nil || got != nk.String() {
		t.Errorf("one named mesh: %q, %v", got, err)
	}

	two := one
	two.MeshSet = map[string]state.Mesh{
		"office": one.MeshSet["office"],
		"home":   {NetworkKey: nk2.String()},
	}
	if _, err := meshKeyFor(two, ""); err == nil || !strings.Contains(err.Error(), "--mesh") {
		t.Errorf("several meshes and no name: %v, want a request for --mesh", err)
	}
	if got, err := meshKeyFor(two, "home"); err != nil || got != nk2.String() {
		t.Errorf("named: %q, %v", got, err)
	}

	legacy := state.DefaultConfig()
	legacy.NetworkKey = nk.String()
	if got, err := meshKeyFor(legacy, ""); err != nil || got != nk.String() {
		t.Errorf("an old top-level config: %q, %v", got, err)
	}
}
