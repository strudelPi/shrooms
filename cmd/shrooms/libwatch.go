package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/vpavlin/shrooms/internal/waku"
	"github.com/vpavlin/shrooms/internal/wg"
)

// Restart when the delivery library itself is dead — rarely, and never by
// accident.
//
// On 2026-10-01 the VPS spent hours with a library that refused every request
// ("Couldn't send a request to the ffi thread") while reporting itself
// Connected. watchRendezvous logged nothing for over two hours — no repair, no
// restart, no backoff line — and the explanation the code allowed is a library
// call in its repair step that never returned, hanging the goroutine that would
// have restarted the process (waku.call could not time out a C call that did
// not return; it can now). A restart by hand fixed it at once.
//
// So this watcher is built not to share that fate: it calls nothing in the
// library and takes no lock any library caller holds. It reads one verdict,
// assembled by the waku package from the outcome of every call.
//
// The verdict needs five minutes of the dead-thread signature, from at least
// ten calls of at least two kinds, with nothing succeeding in between and a
// repair having failed the same way (waku.DeadAfter and friends). After that,
// three more conditions before the process is ended:
//
//   - something will start it again (restartable): a daemon run by hand in a
//     terminal is told, and left running;
//   - the restart history allows it: the history is the one watchRendezvous
//     writes, so both watchers spend one budget, and it survives the restart —
//     a crash loop cannot reset it;
//   - at least libraryRestartFloor has passed since the last restart, for any
//     reason. Consecutive restarts back off from there, doubling to the
//     two-hour ceiling.
//
// What it cannot do: notice a library that is alive and merely unhelpful. That
// is watchRendezvous's job, and the two do not overlap — this one only ever
// acts on the library refusing requests.

// libraryRestartFloor is the least time between two self-restarts when this
// watcher is the one asking. Also the base of its backoff: 30 minutes, then
// an hour, then two.
const libraryRestartFloor = 30 * time.Minute

// libraryDeferredLogEvery is how often to say that a restart is wanted and
// being held back, so the log is not one line every tick for two hours.
const libraryDeferredLogEvery = 10 * time.Minute

// libraryWatch is the decision, separated from the goroutine so it can be
// driven directly.
type libraryWatch struct {
	verdict     func(time.Time) (bool, string)
	restarts    *restartLog
	restartable func() bool
	lastSaid    time.Time
}

// decide reports whether to restart now, and what to log either way. An empty
// message means there is nothing worth saying.
func (w *libraryWatch) decide(now time.Time) (restart bool, level slog.Level, msg string, evidence string) {
	dead, evidence := w.verdict(now)
	if !dead {
		return false, 0, "", ""
	}
	if !w.restartable() {
		if now.Sub(w.lastSaid) < libraryDeferredLogEvery {
			return false, 0, "", evidence
		}
		w.lastSaid = now
		return false, slog.LevelError,
			"the delivery library is dead and nothing would restart me; restart this daemon", evidence
	}
	// The floor is the backoff's base: any restart already in the history —
	// this watcher's or watchRendezvous's — makes the next one wait at least
	// libraryRestartFloor, doubling per consecutive restart.
	if ok, _ := w.restarts.ready(now, libraryRestartFloor); !ok {
		if now.Sub(w.lastSaid) < libraryDeferredLogEvery {
			return false, 0, "", evidence
		}
		w.lastSaid = now
		return false, slog.LevelWarn,
			"the delivery library is dead; holding the restart back because the last one was recent", evidence
	}
	w.restarts.note(now)
	return true, slog.LevelWarn, "the delivery library is dead; restarting the process", evidence
}

// watchLibrary runs decide every half minute and ends the process when it says
// so, through the same channel as every other requested restart.
func watchLibrary(ctx context.Context, log *slog.Logger, stateDir string, errs chan<- error) {
	w := &libraryWatch{
		verdict:     waku.LibraryVerdict,
		restarts:    loadRestartLog(stateDir),
		restartable: restartable,
	}
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			restart, level, msg, evidence := w.decide(now)
			if msg != "" {
				log.Log(ctx, level, msg, "evidence", evidence, "history", w.restarts.String())
			}
			if restart {
				errs <- errors.New(evidence)
				return
			}
		}
	}
}

// rendezvousBackoffOverride lets watchRendezvous restart before its backoff
// has run out — in one case, on evidence.
//
// On 2026-10-01 pi5 and proteus logged "the rendezvous plane needs a restart,
// but the last one did not help" for a long time. They had restarted
// themselves while the fleet was down — every fleet node they knew refused
// connections in the v0.39 rollout — so the restart could not help, and each
// one doubled the next wait towards the two-hour ceiling. When the fleet came
// back they sat out the backoff; a restart by hand brought both up at once.
//
// The backoff exists for a node whose network is gone, which a restart cannot
// fix and which should not tear down tunnels in a loop. A node that can prove
// its own network works has a different problem, and trying again at the
// floor is the right cadence. So the wait is capped at libraryRestartFloor —
// still at most one self-restart per half hour — only when all of:
//
//   - the plane is down outright (stalled), not merely quiet or deaf;
//   - the history holds a restart that did not help, at least the floor ago;
//   - a tunnel completed a handshake within wg.RejectAfter over a public or
//     relayed endpoint (onlineEvidence). A LAN peer does not count: it proves
//     the LAN works, and the fleet is on the internet.
//
// Offline, none of this applies and the backoff stands.
func rendezvousBackoffOverride(restarts *restartLog, now time.Time, stalled bool, online string) (bool, string) {
	if !stalled || online == "" {
		return false, ""
	}
	if restarts.Last.IsZero() || now.Sub(restarts.Last) < libraryRestartFloor {
		return false, ""
	}
	return true, online
}

// onlineEvidence names a tunnel that proves this machine reaches the internet
// right now, or "" if none does. Read from WireGuard, never from the delivery
// library: the watchdog must not call into the thing it is judging.
func onlineEvidence(instances []*instance, now time.Time) string {
	for _, in := range instances {
		stats, err := in.mesh.PeerStats()
		if err != nil {
			continue
		}
		if ev := onlineEvidenceIn(stats, now); ev != "" {
			return ev
		}
	}
	return ""
}

func onlineEvidenceIn(stats map[string]wg.PeerStat, now time.Time) string {
	for _, st := range stats {
		if st.LastHandshake.IsZero() || now.Sub(st.LastHandshake) >= wg.RejectAfter {
			continue
		}
		if ap, ok := endpointAddr(st.Endpoint); ok && publicAddr(ap.Addr()) {
			return fmt.Sprintf("handshake with %s %s ago", st.Endpoint, now.Sub(st.LastHandshake).Round(time.Second))
		}
	}
	return ""
}

// endpointAddr reads a WireGuard endpoint, plain ("ip:port") or relayed
// ("relay:<key>@ip:port") — a relayed one proves the relay is reachable, which
// is the internet.
func endpointAddr(ep string) (netip.AddrPort, bool) {
	if i := strings.LastIndex(ep, "@"); i >= 0 {
		ep = ep[i+1:]
	}
	ap, err := netip.ParseAddrPort(ep)
	return ap, err == nil
}

// publicAddr is an address on the internet: not loopback, link-local, private
// or carrier-grade NAT space.
func publicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return false
	}
	return !netip.MustParsePrefix("100.64.0.0/10").Contains(a)
}
