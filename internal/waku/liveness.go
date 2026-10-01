package waku

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Whether the delivery library itself is still alive.
//
// On 2026-10-01 the VPS ran for hours — how many is unknown, the journal had
// rotated — with every call into the library failing the same way:
//
//	ffi call failed (rc=1 ret=1): error in sendRequestToFFIThread:
//	Couldn't send a request to the ffi thread
//
// on subscribe and on publish alike, while the node went on reporting
// status=Connected. The library's request thread was gone; nothing short of a
// new process could bring it back, and `systemctl restart shrooms` did, with no
// such error afterwards. Every watchdog in the daemon reasons from the
// connection status and the traffic that arrives, so none of them could see a
// node that was "connected" and could no longer be asked to do anything.
//
// This keeps that verdict, in one place, from the outcome of every call — so
// the evidence is the library failing at the one thing it is for, not anybody's
// reading of a status string.

// ErrLibraryUnreachable is wrapped into every error that means the library
// could not be asked at all: its request thread refused the request, or the
// call never returned. Callers test for it with errors.Is rather than matching
// the library's wording, which lives here and nowhere else.
var ErrLibraryUnreachable = errors.New("the delivery library is not accepting requests")

// deadThreadSignature is how the library says its request thread is gone. It is
// emitted when handing a request to that thread fails, which is not a network
// condition: a node with no peers, no route or no fleet still accepts requests
// and fails them later, with other messages.
const deadThreadSignature = "Couldn't send a request to the ffi thread"

func isDeadThread(msg string) bool { return strings.Contains(msg, deadThreadSignature) }

// The thresholds for calling the library dead. All of them must hold, because
// the action this justifies — ending the process — costs every tunnel on every
// mesh, and the requirement it answers is "rarely, and never by accident".
const (
	// DeadAfter is how long the failures must have gone on, with nothing
	// succeeding in between. Longer than any fleet wobble seen so far, and
	// several announce intervals, so it spans many independent attempts.
	DeadAfter = 5 * time.Minute

	// DeadCalls is how many failing calls that must include. At the announce
	// cadence of a two-mesh node that is a little under five minutes' worth;
	// a burst of failures from one moment cannot reach it alone.
	DeadCalls = 10

	// DeadOps is how many different kinds of call must have failed. One
	// operation failing repeatedly is a fault in that operation; subscribe
	// AND publish failing is the library.
	DeadOps = 2
)

// outcome is what one call came to, as far as liveness is concerned.
type outcome int

const (
	callOK      outcome = iota // answered: the library is alive, whatever it said
	callDead                   // refused by a dead request thread
	callHung                   // never returned, or never called back
	callOtherly                // failed for some other reason: says nothing either way
)

// Liveness accumulates the evidence. Safe for concurrent use: every goroutine
// that talks to the library records into it.
type Liveness struct {
	mu sync.Mutex
	// since is when the current run of failures began; zero when the last call
	// that said anything succeeded.
	since time.Time
	last  time.Time
	calls int
	hung  int
	ops   map[string]bool
	// repairFailed is set when somebody tried to fix the plane during this run
	// and that attempt failed the same way — so the conclusion rests on a
	// repair having been tried, not only on things being quiet.
	repairFailed bool
}

// record notes one call's outcome.
//
// A success resets everything: a library that answered anything is alive. A
// failure for any other reason is ignored rather than counted or reset — a
// timeout to a fleet peer or "no peers to publish to" proves nothing about
// the request thread in either direction.
func (l *Liveness) record(op string, o outcome, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch o {
	case callOK:
		l.since, l.last = time.Time{}, time.Time{}
		l.calls, l.hung = 0, 0
		l.ops = nil
		l.repairFailed = false
	case callDead, callHung:
		if l.since.IsZero() {
			l.since = now
		}
		l.last = now
		l.calls++
		if o == callHung {
			l.hung++
		}
		if l.ops == nil {
			l.ops = map[string]bool{}
		}
		l.ops[op] = true
	}
}

// RepairFailed records that an attempt to repair the rendezvous plane failed
// because the library could not be reached. It only counts during a run of
// such failures, so a repair that failed for an ordinary reason earlier cannot
// be held against a later, different, outage.
func (l *Liveness) RepairFailed(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.since.IsZero() {
		l.repairFailed = true
	}
}

// Verdict says whether the library is dead, and on what evidence. The evidence
// is written for the log line an operator reads afterwards, so it states the
// numbers that crossed the thresholds rather than a conclusion.
func (l *Liveness) Verdict(now time.Time) (dead bool, evidence string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.since.IsZero() {
		return false, ""
	}
	span := now.Sub(l.since)
	ops := make([]string, 0, len(l.ops))
	for op := range l.ops {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	evidence = fmt.Sprintf("delivery library unreachable for %s across %d calls (%s)",
		span.Round(time.Second), l.calls, strings.Join(ops, ", "))
	if l.hung > 0 {
		evidence += fmt.Sprintf(", %d never returned", l.hung)
	}
	if l.repairFailed {
		evidence += "; a repair failed the same way"
	}
	dead = span >= DeadAfter && l.calls >= DeadCalls && len(l.ops) >= DeadOps && l.repairFailed
	return dead, evidence
}

// lib is the process's verdict. One, because the library is one per process:
// its state is process-global, which is also why a dead one cannot be revived
// without a new process.
var lib = &Liveness{}

// LibraryVerdict reports whether this process's delivery library is dead. See
// Liveness.Verdict.
func LibraryVerdict(now time.Time) (bool, string) { return lib.Verdict(now) }

// LibraryRepairFailed records a failed repair. See Liveness.RepairFailed.
func LibraryRepairFailed(now time.Time) { lib.RepairFailed(now) }
