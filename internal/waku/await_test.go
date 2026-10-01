package waku

import (
	"errors"
	"testing"
	"time"
)

// A call into the library that never returns used to hang its caller for
// ever: the timeout only started once the C call had returned. These drive
// await, which call delegates to, with an invoke that blocks.

func TestACallThatNeverReturnsGivesUpAtTheTimeout(t *testing.T) {
	lib = &Liveness{}
	block := make(chan struct{})
	defer close(block)

	start := time.Now()
	_, err, settled := await("unsubscribe", func() int { <-block; return 0 }, make(chan result, 1), 50*time.Millisecond)
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("waited %s for a call that never returns", took)
	}
	if !errors.Is(err, ErrLibraryUnreachable) {
		t.Errorf("err = %v, want ErrLibraryUnreachable", err)
	}
	// The library may still call back with this handle, so it must not be
	// freed: a deleted handle looked up later is a panic.
	if settled {
		t.Error("reported settled, which would free a handle the library may still use")
	}
	if _, ev := lib.Verdict(time.Now()); ev == "" {
		t.Error("the hung call left no evidence")
	}
}

func TestADeadRequestThreadIsReportedAsSuch(t *testing.T) {
	lib = &Liveness{}
	ch := make(chan result, 1)
	ch <- result{ret: 1, msg: deadMsg}
	_, err, settled := await("subscribe", func() int { return 1 }, ch, time.Second)
	if !errors.Is(err, ErrLibraryUnreachable) || !settled {
		t.Errorf("err=%v settled=%v", err, settled)
	}
}

// An ordinary failure is not the library being dead, and must not say so.
func TestAnOrdinaryFailureIsNotUnreachable(t *testing.T) {
	lib = &Liveness{}
	ch := make(chan result, 1)
	ch <- result{ret: 1, msg: "no peers to publish to"}
	_, err, _ := await("send", func() int { return 1 }, ch, time.Second)
	if err == nil || errors.Is(err, ErrLibraryUnreachable) {
		t.Errorf("err = %v: want a plain failure", err)
	}
	if _, ev := lib.Verdict(time.Now()); ev != "" {
		t.Errorf("an ordinary failure was counted: %s", ev)
	}
}

// One answer from the library clears everything gathered against it.
func TestAnAnswerClearsTheEvidence(t *testing.T) {
	lib = &Liveness{}
	lib.record("send", callDead, time.Now())
	ch := make(chan result, 1)
	ch <- result{ret: 0, msg: "ok"}
	if _, err, settled := await("peer_id", func() int { return 0 }, ch, time.Second); err != nil || !settled {
		t.Fatalf("err=%v settled=%v", err, settled)
	}
	if _, ev := lib.Verdict(time.Now()); ev != "" {
		t.Errorf("evidence survived a successful call: %s", ev)
	}
}
