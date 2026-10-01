package waku

import (
	"fmt"
	"time"
)

// await is call's waiting, without the cgo: run invoke on its own goroutine and
// wait at most timeout in total, for it to return and then for the callback on
// ch. Split out so the part that hung — waiting on a call that never returns —
// can be tested with an invoke that never returns, which a test file cannot
// build from C types.
//
// settled reports whether the callback arrived, which is the only case where
// the handle behind ch may be deleted: otherwise the library may still call
// back with it, and looking up a deleted handle is a panic.
func await(op string, invoke func() int, ch <-chan result, timeout time.Duration) (msg string, err error, settled bool) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	rcCh := make(chan int, 1)
	go func() { rcCh <- invoke() }()

	var rc int
	select {
	case rc = <-rcCh:
	case <-deadline.C:
		lib.record(op, callHung, time.Now())
		return "", fmt.Errorf("ffi %s did not return within %s: %w", op, timeout, ErrLibraryUnreachable), false
	}

	select {
	case r := <-ch:
		if rc != 0 || r.ret != 0 {
			if isDeadThread(r.msg) {
				lib.record(op, callDead, time.Now())
				return r.msg, fmt.Errorf("ffi call failed (rc=%d ret=%d): %s: %w",
					rc, r.ret, r.msg, ErrLibraryUnreachable), true
			}
			lib.record(op, callOtherly, time.Now())
			return r.msg, fmt.Errorf("ffi call failed (rc=%d ret=%d): %s", rc, r.ret, r.msg), true
		}
		lib.record(op, callOK, time.Now())
		return r.msg, nil, true
	case <-deadline.C:
		lib.record(op, callHung, time.Now())
		return "", fmt.Errorf("ffi %s: callback timed out: %w", op, ErrLibraryUnreachable), false
	}
}
