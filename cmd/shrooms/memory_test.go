package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vpavlin/shrooms/internal/memstat"
)

// The daemon runs as root, so its memory is visible to its user only through
// what it reports. This sends a real reading of this process through the
// daemon's own status type and reads it back the way the command does.
func TestMemoryReadsWhatTheDaemonReports(t *testing.T) {
	body, err := json.Marshal(statusPayload{Version: "test", Memory: memPtr(memstat.Read())})
	if err != nil {
		t.Fatal(err)
	}
	sock := statusSocket(t, string(body))

	out := captureStdout(t, func() {
		if err := cmdMemory([]string{"--socket", sock}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"resident", "dirty", "go ", "heap", "native"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, " 0.0 MiB held from the OS") {
		t.Errorf("Go's share came through as zero:\n%s", out)
	}
}

// A daemon from before this existed says nothing about memory. Printing a
// table of zeros would read as "uses no memory"; say what is actually wrong.
func TestMemoryFromADaemonThatDoesNotReportIt(t *testing.T) {
	sock := statusSocket(t, `{"version":"v0.9.0-8-g03b26b2"}`)
	err := cmdMemory([]string{"--socket", sock})
	if err == nil || !strings.Contains(err.Error(), "predates") {
		t.Errorf("want an error saying the daemon predates the command, got %v", err)
	}
}
