package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// A peer with history looked connected when it was not. Pongs are never
// forgotten, so a phone that moved off Wi-Fi hours earlier still showed its
// Wi-Fi paths, and the one fact that explained the state — an announce with
// no endpoints in it — was printed only for peers with no history at all.
//
// Seen as four rows, all hours old, no "in use" marker, and nothing on the
// screen saying the phone was announcing nothing.

func peerPayload(lastPong string, endpoints string) string {
	return fmt.Sprintf(`{
	  "peers": [{
	    "name": "pixel", "overlay": "fd89::1",
	    "endpoints": %s,
	    "paths": [
	      {"addr": "192.168.7.36:51820", "rtt_ms": 89, "last_pong": %q, "selected": false},
	      {"addr": "[2001:db8::1c]:51820", "rtt_ms": 89, "last_pong": %q, "selected": false}
	    ]
	  }]
	}`, endpoints, lastPong, lastPong)
}

func TestPathsSaysWhenNoPathIsCurrent(t *testing.T) {
	old := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	sock := statusSocket(t, peerPayload(old, "[]"))

	out := captureStdout(t, func() {
		if err := cmdPaths([]string{"--socket", sock, "pixel"}); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out, "none of these is current") {
		t.Errorf("hours-old pongs were not called out:\n%s", out)
	}
	if !strings.Contains(out, "announced: nothing") {
		t.Errorf("the empty announce — the fact that explains the state — is not shown:\n%s", out)
	}
	if strings.Count(out, "stale") < 2 {
		t.Errorf("stale rows are not marked as such:\n%s", out)
	}
}

// With a real announce the line names it, so the reader can compare what was
// offered with what answered.
func TestPathsShowsWhatAStalePeerAnnounced(t *testing.T) {
	old := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	sock := statusSocket(t, peerPayload(old, `["[2a00:db8::7]:51820"]`))

	out := captureStdout(t, func() {
		if err := cmdPaths([]string{"--socket", sock}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "announced: [[2a00:db8::7]:51820]") {
		t.Errorf("the announced address is not shown:\n%s", out)
	}
}

// A peer that answered a moment ago is fine, and saying otherwise would turn
// every healthy table into a warning.
func TestPathsStaysQuietWhileAPathIsCurrent(t *testing.T) {
	fresh := time.Now().UTC().Format(time.RFC3339)
	sock := statusSocket(t, peerPayload(fresh, "[]"))

	out := captureStdout(t, func() {
		if err := cmdPaths([]string{"--socket", sock}); err != nil {
			t.Fatal(err)
		}
	})
	for _, bad := range []string{"none of these is current", "announced:", "stale"} {
		if strings.Contains(out, bad) {
			t.Errorf("a current path was reported as stale (%q):\n%s", bad, out)
		}
	}
}

// A peer with no history and an empty announce is the same fault seen
// earlier, and it was already half-reported: the line said what had not
// answered, and nothing said that nothing had been offered.
func TestPathsSaysNothingWasAnnouncedForAPeerWithNoHistory(t *testing.T) {
	sock := statusSocket(t, `{"peers": [{"name": "pixel", "overlay": "fd89::1", "endpoints": [], "paths": []}]}`)

	out := captureStdout(t, func() {
		if err := cmdPaths([]string{"--socket", sock}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "announced: nothing") {
		t.Errorf("an empty announce is not shown for a peer with no paths:\n%s", out)
	}
}
