package waku

import (
	"strings"
	"testing"
	"time"
)

// The verdict ends a process and every tunnel in it, and the requirement is
// that it happen rarely and never by accident. These walk the cases that must
// NOT reach it as carefully as the one that must.

const deadMsg = "error in sendRequestToFFIThread: Couldn't send a request to the ffi thread"

// What the VPS logged on 2026-10-01, compressed: subscribe and publish both
// refused by a dead request thread at every announce interval, repairs failing
// the same way, nothing succeeding.
func TestASustainedDeadLibraryIsCalledDead(t *testing.T) {
	l := &Liveness{}
	t0 := time.Unix(1_800_000_000, 0)
	var now time.Time
	for i := 0; i < 8; i++ {
		now = t0.Add(time.Duration(i) * 45 * time.Second)
		l.record("subscribe", callDead, now)
		l.record("send", callDead, now)
		if i == 2 {
			l.RepairFailed(now)
		}
	}
	dead, ev := l.Verdict(now)
	if !dead {
		t.Fatalf("five minutes of a dead request thread was not called dead: %s", ev)
	}
	for _, want := range []string{"unreachable for", "16 calls", "send, subscribe", "a repair failed"} {
		if !strings.Contains(ev, want) {
			t.Errorf("evidence %q does not say %q", ev, want)
		}
	}
}

func TestNothingShortOfTheWholePictureIsDead(t *testing.T) {
	t0 := time.Unix(1_800_000_000, 0)
	feed := func(l *Liveness, n int, step time.Duration, ops ...string) time.Time {
		var now time.Time
		for i := 0; i < n; i++ {
			now = t0.Add(time.Duration(i) * step)
			for _, op := range ops {
				l.record(op, callDead, now)
			}
		}
		return now
	}
	for _, tc := range []struct {
		name string
		run  func(l *Liveness) time.Time
	}{{
		"a single failure",
		func(l *Liveness) time.Time {
			l.record("send", callDead, t0)
			l.RepairFailed(t0)
			return t0.Add(time.Hour)
		},
	}, {
		"many failures, all in one moment",
		func(l *Liveness) time.Time {
			now := feed(l, 30, 0, "send", "subscribe")
			l.RepairFailed(now)
			return now
		},
	}, {
		"sustained, but only one kind of call",
		func(l *Liveness) time.Time {
			now := feed(l, 20, 30*time.Second, "send")
			l.RepairFailed(now)
			return now
		},
	}, {
		"sustained across kinds, but nobody tried to repair it",
		func(l *Liveness) time.Time { return feed(l, 10, 45*time.Second, "send", "subscribe") },
	}, {
		"sustained, then the library answered once",
		func(l *Liveness) time.Time {
			now := feed(l, 10, 45*time.Second, "send", "subscribe")
			l.RepairFailed(now)
			l.record("peers_in_mesh", callOK, now)
			return now
		},
	}, {
		// An offline node, a node with no peers, a fleet outage: the library
		// answers and the answer is a failure. That is not the request thread.
		"ordinary failures for a long time",
		func(l *Liveness) time.Time {
			var now time.Time
			for i := 0; i < 40; i++ {
				now = t0.Add(time.Duration(i) * 30 * time.Second)
				l.record("send", callOtherly, now)
				l.record("subscribe", callOtherly, now)
			}
			l.RepairFailed(now)
			return now
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			l := &Liveness{}
			now := tc.run(l)
			if dead, ev := l.Verdict(now); dead {
				t.Errorf("called dead: %s", ev)
			}
		})
	}
}

// A repair that failed for some other reason, before this run of failures
// began, must not count towards it.
func TestARepairBeforeTheRunDoesNotCount(t *testing.T) {
	l := &Liveness{}
	t0 := time.Unix(1_800_000_000, 0)
	l.RepairFailed(t0) // nothing failing yet: ignored
	var now time.Time
	for i := 0; i < 10; i++ {
		now = t0.Add(time.Duration(i) * 45 * time.Second)
		l.record("send", callDead, now)
		l.record("subscribe", callDead, now)
	}
	if dead, ev := l.Verdict(now); dead {
		t.Errorf("a repair from before the outage counted: %s", ev)
	}
}

// Calls that never return are the stronger evidence, and the case that hid the
// fault: they count, and the evidence says so.
func TestCallsThatNeverReturnCount(t *testing.T) {
	l := &Liveness{}
	t0 := time.Unix(1_800_000_000, 0)
	var now time.Time
	for i := 0; i < 6; i++ {
		now = t0.Add(time.Duration(i) * time.Minute)
		l.record("unsubscribe", callHung, now)
		l.record("send", callDead, now)
	}
	l.RepairFailed(now)
	dead, ev := l.Verdict(now)
	if !dead || !strings.Contains(ev, "6 never returned") {
		t.Errorf("dead=%v evidence=%q", dead, ev)
	}
}

// The signature is matched here and nowhere else, and only the signature.
func TestOnlyTheDeadThreadMessageIsTheSignature(t *testing.T) {
	if !isDeadThread(deadMsg) {
		t.Error("the message the VPS logged was not recognised")
	}
	for _, other := range []string{
		"no peers to publish to",
		"ffi callback timed out",
		"connection refused",
		"",
	} {
		if isDeadThread(other) {
			t.Errorf("%q was taken for a dead request thread", other)
		}
	}
}
