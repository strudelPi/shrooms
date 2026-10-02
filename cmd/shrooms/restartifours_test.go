package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// A command that changed one config restarts only the daemon running it.
//
// On 2026-10-02 every run of the test suite restarted the system daemon twice:
// a test ran `init --config <temp> --mesh shared`, init found a daemon on the
// default socket, and asked it to restart — dropping every tunnel on a laptop
// whose daemon had never read that file. Driven over a real unix socket,
// counting the restarts that arrive.
func TestRestartReachesOnlyTheDaemonRunningThatConfig(t *testing.T) {
	cases := []struct {
		name    string
		runs    string // what the daemon says it runs; "" for one too old to say
		changed string
		want    bool
	}{
		{"the daemon running this config restarts", "/etc/shrooms/config.toml", "/etc/shrooms/config.toml", true},
		{"a daemon running another config is left alone", "/etc/shrooms/config.toml", "/tmp/test/config.toml", false},
		{"a daemon too old to say is left alone", "", "/etc/shrooms/config.toml", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sock, restarts := serveRunning(t, c.runs)
			got := restartIfOurs(sock, c.changed)
			if got != c.want || (restarts.Load() > 0) != c.want {
				t.Errorf("restartIfOurs = %v with %d restart requests, want %v", got, restarts.Load(), c.want)
			}
		})
	}
}

// The same file named two ways is the same config.
func TestRunsConfigFollowsTheFileNotTheSpelling(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !runsConfig(statusPayload{Config: cfg}, filepath.Join(dir, ".", "config.toml")) {
		t.Error("the same file through a different path was not recognised")
	}
}

// serveRunning runs a fake daemon that says it runs config, and counts the
// restart requests it receives.
func serveRunning(t *testing.T, config string) (string, *atomic.Int32) {
	t.Helper()
	// Short, because a unix socket path is limited to about a hundred bytes and
	// t.TempDir() is named after the subtest.
	dir, err := os.MkdirTemp("", "sk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var restarts atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(statusPayload{Name: "laptop", Config: config})
	})
	mux.HandleFunc("/restart", func(w http.ResponseWriter, _ *http.Request) {
		restarts.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return sock, &restarts
}
