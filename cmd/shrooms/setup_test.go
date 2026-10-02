package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vpavlin/shrooms/internal/cred"
	"github.com/vpavlin/shrooms/internal/state"
)

// Creating a mesh is one command. It used to be three — init, admin init, admin
// issue — and every one of them was a chance to end up with a config that
// names an authority nobody holds, or an authority no config trusts.
func TestInitMintsAndEnrols(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "shrooms.toml")
	stateDir := filepath.Join(dir, "state")
	adminDir := filepath.Join(dir, "admin")

	quiet(t)
	withStdin(t, "hunter2 hunter2\nhunter2 hunter2\n")
	if err := cmdInit([]string{
		"--name", "laptop",
		"--mesh", "home",
		"--config", cfgPath,
		"--state", stateDir,
		"--admin-dir", adminDir,
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := state.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	// The mesh is named, and its authority lives on its own entry.
	home, ok := cfg.MeshSet["home"]
	if !ok {
		t.Fatalf("init wrote no mesh called home: %+v", cfg.Meshes())
	}
	if cfg.NetworkKey != "" {
		t.Error("init still wrote the old top-level shape")
	}
	auth, err := home.Authority()
	if err != nil {
		t.Fatal(err)
	}
	if auth == nil {
		t.Fatal("init wrote no admin_keys; the mesh trusts nobody")
	}
	if len(auth.Keys) != 2 {
		t.Fatalf("authority has %d keys, want the signing key and a recovery key", len(auth.Keys))
	}

	// The point of folding these together: the device that ran init is already
	// a member, and can prove it to the mesh it just created.
	st, err := state.LoadOrCreateState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	// Under the base identity: this is the device's first mesh, so it keeps
	// the keys it was generated with rather than deriving new ones.
	netID, err := home.NetworkID()
	if err != nil {
		t.Fatal(err)
	}
	ms, err := st.MeshState(netID, home.InheritsIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if !home.InheritsIdentity || !bytes.Equal(ms.Identity.DevicePub, st.Identity.DevicePub) {
		t.Fatal("the first mesh does not hold the device's base identity")
	}
	if len(ms.Credential) == 0 {
		t.Fatal("init issued no credential; this device could not join its own mesh")
	}
	c, err := cred.UnmarshalCredential(ms.Credential)
	if err != nil {
		t.Fatal(err)
	}
	if err := cred.VerifyBy(auth, c, time.Now()); err != nil {
		t.Fatalf("own credential does not verify: %v", err)
	}
	if c.MeshID != auth.ID() {
		t.Errorf("credential names mesh %s, config trusts %s", c.MeshID, auth.ID())
	}
	if c.Name != "laptop" {
		t.Errorf("credential names %q, want laptop", c.Name)
	}

	// The admin key stays encrypted at rest: init read a passphrase.
	raw, err := os.ReadFile(filepath.Join(adminDir, "admin-home.json"))
	if err != nil {
		t.Fatal(err)
	}
	var af adminFile
	if err := json.Unmarshal(raw, &af); err != nil {
		t.Fatal(err)
	}
	if !af.Encrypted {
		t.Error("admin key was written in the clear")
	}
}

// --no-admin is for joining a mesh that already exists — minting an authority
// there would create a second mesh nobody asked for.
func TestInitNoAdmin(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "shrooms.toml")
	adminDir := filepath.Join(dir, "admin")

	quiet(t)
	if err := cmdInit([]string{
		"--name", "laptop",
		"--mesh", "home",
		"--config", cfgPath,
		"--state", filepath.Join(dir, "state"),
		"--admin-dir", adminDir,
		"--no-admin",
	}); err != nil {
		t.Fatal(err)
	}
	if found, _ := filepath.Glob(filepath.Join(adminDir, "admin*.json")); len(found) != 0 {
		t.Errorf("--no-admin minted an authority anyway: %v", found)
	}
	cfg, err := state.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AdminKeys) != 0 || len(cfg.MeshSet["home"].AdminKeys) != 0 {
		t.Error("--no-admin wrote admin_keys")
	}
}

// A first mesh is named, always: names are qualified everywhere, so a mesh
// without one could be reached by no name at all. Refused before anything is
// written, and "default" refused with it — it is what the old unlabelled shape
// was called, not a name (docs/one-kind-of-mesh.md, 2026-09-29).
func TestInitNeedsAName(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no name", nil, "name the mesh"},
		{"default", []string{"--mesh", "default"}, "not a mesh name"},
		{"not a label", []string{"--mesh", "Home Net"}, "lower-case"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "shrooms.toml")
			args := append([]string{"--name", "laptop", "--config", cfgPath,
				"--state", filepath.Join(dir, "state"), "--no-admin"}, tc.args...)
			err := cmdInit(args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error mentioning %q", err, tc.want)
			}
			if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
				t.Error("a refused init wrote a config anyway")
			}
		})
	}
}

// quiet swallows what init prints. A recovery key scrolling past in CI output
// is noise at best and a bad habit at worst.
func quiet(t *testing.T) {
	t.Helper()
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = null
	t.Cleanup(func() {
		os.Stdout = old
		null.Close()
	})
}

// withStdin points the shared reader at a script for the duration of a test.
func withStdin(t *testing.T, script string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(script); err != nil {
		t.Fatal(err)
	}
	w.Close()

	oldFile, oldReader := os.Stdin, stdinReader
	os.Stdin, stdinReader = r, nil
	t.Cleanup(func() {
		r.Close()
		os.Stdin, stdinReader = oldFile, oldReader
	})
}

// The three outcomes of nudging a daemon need three different instructions, and
// the wrong one is worse than none.
//
// This is the bug the first person outside the project hit: init wrote the
// config, the nudge did not land, and the advice printed was
// "sudo systemctl enable --now shrooms" — which on an already-running service
// does nothing at all. Following it exactly left a config, a daemon still
// waiting, and no sign of which was wrong. The invite then failed with
// "404 page not found".
//
// Driven over a real unix socket, because that is what nudgeDaemon dials and a
// TCP stand-in would exercise a path the daemon never uses.
func TestNudgeOutcomesAreDistinguished(t *testing.T) {
	t.Run("waiting daemon accepts the reload", func(t *testing.T) {
		sock := serveWaiting(t, true)
		if !nudgeDaemon(sock, waitingConfig) {
			t.Error("a daemon that accepted /reload was not treated as nudged")
		}
	})

	t.Run("waiting daemon refuses the reload", func(t *testing.T) {
		sock := serveWaiting(t, false)
		if nudgeDaemon(sock, waitingConfig) {
			t.Error("a daemon that refused /reload was reported as nudged")
		}
		// And it is still reachable and still waiting, which is what tells
		// reportNext to say "restart" rather than "enable --now".
		st, err := fetchStatus(sock)
		if err != nil || !st.Waiting {
			t.Errorf("status after a refused reload: %+v, err %v", st, err)
		}
	})

	t.Run("no daemon", func(t *testing.T) {
		if nudgeDaemon(filepath.Join(t.TempDir(), "absent.sock"), waitingConfig) {
			t.Error("an absent daemon was reported as nudged")
		}
	})

	t.Run("a daemon running another config is left alone", func(t *testing.T) {
		sock := serveWaiting(t, true)
		if nudgeDaemon(sock, filepath.Join(t.TempDir(), "config.toml")) {
			t.Error("a daemon that never read this config was nudged to reload it")
		}
	})
}

// waitingConfig is the config the fake daemons below say they run.
const waitingConfig = "/etc/shrooms/config.toml"

// serveWaiting runs a daemon with no mesh on a unix socket and returns its path.
func serveWaiting(t *testing.T, acceptReload bool) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(statusPayload{Waiting: true, Config: waitingConfig})
	})
	mux.HandleFunc("/reload", func(w http.ResponseWriter, _ *http.Request) {
		if !acceptReload {
			http.Error(w, "not now", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return sock
}
