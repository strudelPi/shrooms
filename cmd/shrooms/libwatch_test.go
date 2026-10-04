package main

import (
	"testing"
	"time"

	"github.com/vpavlin/shrooms/internal/wg"
)

// The library watcher ends the process. These pin when it may, and — the half
// the user asked for — when it must not: never on a live library, never faster
// than libraryRestartFloor, and never by forgetting its history across the
// restart it caused.

func deadLibrary(time.Time) (bool, string) { return true, "unreachable for 6m across 14 calls" }
func liveLibrary(time.Time) (bool, string) { return false, "" }
func always() bool                         { return true }

func TestADeadLibraryRestartsTheProcess(t *testing.T) {
	dir := t.TempDir()
	w := &libraryWatch{verdict: deadLibrary, restarts: loadRestartLog(dir), restartable: always}
	now := time.Unix(1_800_000_000, 0)
	restart, _, msg, ev := w.decide(now)
	if !restart {
		t.Fatalf("a dead library did not restart the process (%q)", msg)
	}
	if ev == "" {
		t.Error("the restart carries no evidence for the log")
	}
	// Written down, so the next process knows.
	if got := loadRestartLog(dir); got.Count != 1 || !got.Last.Equal(now) {
		t.Errorf("the restart was not recorded: %+v", got)
	}
}

func TestALiveLibraryNeverRestarts(t *testing.T) {
	w := &libraryWatch{verdict: liveLibrary, restarts: loadRestartLog(t.TempDir()), restartable: always}
	now := time.Unix(1_800_000_000, 0)
	for i := 0; i < 500; i++ {
		if restart, _, _, _ := w.decide(now.Add(time.Duration(i) * 30 * time.Second)); restart {
			t.Fatal("restarted with a live library")
		}
	}
}

// The case a crash loop would exploit: the process restarted, the library is
// dead again in the new one, and the decision is made by a fresh process with
// fresh memory. Only the history on disk stands between that and a loop.
func TestTheFloorHoldsAcrossTheRestartItCaused(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Unix(1_800_000_000, 0)
	first := &libraryWatch{verdict: deadLibrary, restarts: loadRestartLog(dir), restartable: always}
	if restart, _, _, _ := first.decide(t0); !restart {
		t.Fatal("setup: the first restart did not happen")
	}

	// A new process, ten minutes later, still dead.
	second := &libraryWatch{verdict: deadLibrary, restarts: loadRestartLog(dir), restartable: always}
	for _, after := range []time.Duration{time.Minute, 10 * time.Minute, 29 * time.Minute} {
		if restart, _, _, _ := second.decide(t0.Add(after)); restart {
			t.Fatalf("restarted again %s after the last, inside the %s floor", after, libraryRestartFloor)
		}
	}
	// Past the floor, and the backoff for a second consecutive restart.
	if restart, _, _, _ := second.decide(t0.Add(libraryRestartFloor + time.Minute)); !restart {
		t.Error("a still-dead library was never restarted again after the floor")
	}

	// And the third waits longer than the second did.
	third := &libraryWatch{verdict: deadLibrary, restarts: loadRestartLog(dir), restartable: always}
	at := t0.Add(libraryRestartFloor + time.Minute)
	if restart, _, _, _ := third.decide(at.Add(libraryRestartFloor + time.Minute)); restart {
		t.Error("the third consecutive restart did not back off beyond the floor")
	}
}

// A restart for another reason counts too: one budget for both watchdogs.
func TestARecentRendezvousRestartHoldsThisOneBack(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Unix(1_800_000_000, 0)
	other := loadRestartLog(dir)
	other.note(t0) // watchRendezvous restarted us

	w := &libraryWatch{verdict: deadLibrary, restarts: loadRestartLog(dir), restartable: always}
	if restart, _, _, _ := w.decide(t0.Add(5 * time.Minute)); restart {
		t.Error("restarted five minutes after the rendezvous watchdog had")
	}
}

// Run by hand, nothing would bring the daemon back: say so, keep running, and
// do not repeat it every tick.
func TestWithNothingToRestartItItOnlySaysSo(t *testing.T) {
	never := func() bool { return false }
	w := &libraryWatch{verdict: deadLibrary, restarts: loadRestartLog(t.TempDir()), restartable: never}
	t0 := time.Unix(1_800_000_000, 0)
	restart, _, msg, _ := w.decide(t0)
	if restart || msg == "" {
		t.Fatalf("restart=%v msg=%q: want no restart and an explanation", restart, msg)
	}
	if _, _, msg, _ := w.decide(t0.Add(time.Minute)); msg != "" {
		t.Errorf("repeated itself a minute later: %q", msg)
	}
}

// pi5 and proteus on 2026-10-01: they had restarted themselves while the fleet
// was down, the backoff then held every further attempt towards two hours, and
// a restart by hand fixed both the moment the fleet was back. The backoff may
// now be cut to the floor — only with proof the machine's network works.
func TestTheBackoffShortensOnlyWithInternetEvidence(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	history := func(ago time.Duration) *restartLog {
		r := loadRestartLog(t.TempDir())
		r.Count, r.Last = 5, now.Add(-ago) // deep into the backoff
		return r
	}
	const online = "handshake with 128.140.55.128:51822 12s ago"
	for _, tc := range []struct {
		name    string
		r       *restartLog
		stalled bool
		online  string
		want    bool
	}{
		{"the pi5 case: down outright, network proven, last restart 31m ago", history(31 * time.Minute), true, online, true},
		{"offline: no handshake anywhere", history(31 * time.Minute), true, "", false},
		{"network up, but the last restart was 20m ago", history(20 * time.Minute), true, online, false},
		{"quiet or deaf rather than down", history(31 * time.Minute), false, online, false},
		{"recovered after the last restart (not stalled any more)", history(2 * time.Hour), false, "", false},
		{"no restart in the history at all", loadRestartLog(t.TempDir()), true, online, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ev := rendezvousBackoffOverride(tc.r, now, tc.stalled, tc.online)
			if got != tc.want {
				t.Errorf("override = %v (%q), want %v", got, ev, tc.want)
			}
		})
	}
}

// A LAN peer proves the LAN, not the internet the fleet is on. Only a handshake
// over a public or relayed endpoint, recent enough to be a live session, counts.
func TestOnlyAPublicHandshakeProvesTheNetwork(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	recent, stale := now.Add(-20*time.Second), now.Add(-5*time.Minute)
	for _, tc := range []struct {
		name string
		ep   string
		at   time.Time
		want bool
	}{
		{"a peer on the LAN", "192.168.10.32:51821", recent, false},
		{"carrier-grade NAT space", "100.70.1.2:51820", recent, false},
		{"a public peer", "178.213.45.235:51820", recent, true},
		{"through a relay", "relay:95bc41e9@128.140.55.128:51821", recent, true},
		{"a public peer, but the session is old", "178.213.45.235:51820", stale, false},
		{"never handshook", "178.213.45.235:51820", time.Time{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := map[string]wg.PeerStat{"p": {Endpoint: tc.ep, LastHandshake: tc.at}}
			if got := onlineEvidenceIn(stats, now) != ""; got != tc.want {
				t.Errorf("evidence = %v, want %v", got, tc.want)
			}
		})
	}
}

// A network change earns one restart past the backoff, when every restart in
// the history was made before it — as on 2026-10-04, four made while the wifi
// was switching, then the hotspot came up and the backoff held.
func TestANetworkChangeEarnsOneRestart(t *testing.T) {
	now := time.Date(2026, 10, 4, 8, 30, 0, 0, time.UTC)
	r := &restartLog{Count: 4, Last: now.Add(-7 * time.Minute)}
	changed := now.Add(-1 * time.Minute)
	if !networkChangeOverride(r, now, changed) {
		t.Error("restarts from before the network appeared held the restart back")
	}
	// Once restarted on this network, its history counts again.
	r.Last = now.Add(-30 * time.Second)
	if networkChangeOverride(r, now.Add(5*time.Minute), changed) {
		t.Error("a second early restart on the same network")
	}
	// A wifi flapping every minute does not get a restart every minute.
	r.Last = now.Add(-90 * time.Second)
	if networkChangeOverride(r, now, now.Add(-30*time.Second)) {
		t.Error("restarted within the floor of the last one")
	}
	if networkChangeOverride(r, now, time.Time{}) || networkChangeOverride(&restartLog{}, now, changed) {
		t.Error("no change, or no history, is nothing to override")
	}
}

// No default route is no network: bridges like docker0 have addresses and
// routes, but nowhere to send the rest. Tables as the laptop had them.
func TestNoDefaultRouteIsNoNetwork(t *testing.T) {
	head := "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"
	bridges := head +
		"docker0\t000011AC\t00000000\t0001\t0\t0\t0\t0000FFFF\t0\t0\t0\n" +
		"br-f5b174195d43\t000012AC\t00000000\t0001\t0\t0\t0\t0000FFFF\t0\t0\t0\n"
	wifi := head + "wlo1\t00000000\t6B5DE20A\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" + bridges[len(head):]
	unreachable6 := "00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 ffffffff 00000001 00000000 00200200       lo\n"
	default6 := "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe80000000000000000000000000006b 00000400 00000001 00000000 00000003     wlo1\n"
	mesh6 := "fd3bffe90f8181a718bc69b109bb7e69 80 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000001 00000000 00000001   logos0\n"
	if defaultRouteIn(bridges, unreachable6+mesh6) {
		t.Error("docker's bridges and the kernel's unreachable ::/0 were taken for a network")
	}
	// 0.0.0.0/8 starts at zero and is not everything.
	if defaultRouteIn(head+"eth0\t00000000\t00000000\t0001\t0\t0\t0\t000000FF\t0\t0\t0\n", "") {
		t.Error("a route to 0.0.0.0/8 was taken for a default route")
	}
	if !defaultRouteIn(wifi, "") {
		t.Error("an IPv4 default route was missed")
	}
	if !defaultRouteIn(bridges, mesh6+default6) {
		t.Error("an IPv6-only default route was missed")
	}
}
