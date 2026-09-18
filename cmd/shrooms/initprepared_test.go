package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vpavlin/shrooms/internal/state"
)

// `prepare` then `init` on the machine that turns out to be the first one.
//
// Reported as #13 on 2026-08-24, by somebody following install.sh and then the
// site: install.sh prepares, the site says to create a mesh, and init answered
// "/etc/shrooms/config.toml already exists — remove it". Deleting it by hand
// throws away the name, port, mode and relay setting that were just chosen —
// and on a node that has been enrolled it would throw away far more.
func TestInitMintsIntoAPreparedConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	stateDir := filepath.Join(dir, "state")

	// What prepare leaves behind, including settings init has no flag for.
	prepared := state.DefaultConfig()
	prepared.Name = "nas"
	prepared.NetworkKey = state.KeyPlaceholder
	prepared.ListenPort = 51999
	prepared.Relay = true
	prepared.Mode = "Edge"
	prepared.Services = []string{"immich:2283"}
	if err := state.WriteConfig(cfgPath, prepared); err != nil {
		t.Fatal(err)
	}

	// --no-admin so this does not stop for a passphrase; the mesh is minted
	// either way, which is what this is about.
	if err := cmdInit([]string{"--config", cfgPath, "--state", stateDir, "--no-admin"}); err != nil {
		t.Fatalf("init over a prepared config: %v", err)
	}

	got, err := state.LoadConfigUnvalidated(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.NetworkKey == "" || got.NetworkKey == state.KeyPlaceholder {
		t.Errorf("no network key was minted: %q", got.NetworkKey)
	}
	// Everything the prepared config said, kept.
	if got.Name != "nas" {
		t.Errorf("name = %q, want the prepared %q", got.Name, "nas")
	}
	if got.ListenPort != 51999 {
		t.Errorf("listen port = %d, want the prepared 51999", got.ListenPort)
	}
	if !got.Relay {
		t.Error("relay was on in the prepared config and is off now")
	}
	if got.Mode != "Edge" {
		t.Errorf("mode = %q, want the prepared Edge — init has no flag for it, "+
			"so rebuilding from the defaults is how it would be lost", got.Mode)
	}
	if len(got.Services) != 1 || got.Services[0] != "immich:2283" {
		t.Errorf("services = %v, want the prepared one", got.Services)
	}
}

// A config already on a mesh is still refused: minting over it would leave the
// device holding a key none of its peers has heard of.
func TestInitRefusesAConfigThatIsAlreadyOnAMesh(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	live := state.DefaultConfig()
	live.Name = "laptop"
	live.NetworkKey = "GUPDIZSVIQRCBBC6APQFYDSDKTWK6Q5SYZY6ZLMVECQDQX3GIOYQ"
	if err := state.WriteConfig(cfgPath, live); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	err = cmdInit([]string{"--config", cfgPath, "--state", filepath.Join(dir, "state"), "--no-admin"})
	if err == nil {
		t.Fatal("init minted a new mesh over a config that was already on one")
	}
	after, _ := os.ReadFile(cfgPath)
	if string(before) != string(after) {
		t.Error("the refusal still rewrote the config")
	}
}
