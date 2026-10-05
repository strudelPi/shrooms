package mesh

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// What the replay guard has been doing, per device, for diagnostics.
//
// Added for a phone that heard every announce on one mesh and accepted none
// (2026-10-04): "rejected replayed or stale announce" by the hundred, and no
// way to tell from outside whether they were all genuinely old or the guard's
// marks — kept on disk across restarts — had got ahead of the peers. The
// report puts the mark beside the newest number rejected and when anything
// was last accepted, which tells the two apart at a glance.
type replayStats struct {
	mu   sync.Mutex
	devs map[string]*replayStat // hex(devicePub)
}

type replayStat struct {
	rejected     int
	maxRejected  uint64
	lastRejected time.Time
	accepted     int
	lastAccepted time.Time
}

func (r *replayStats) get(key string) *replayStat {
	if r.devs == nil {
		r.devs = make(map[string]*replayStat)
	}
	s := r.devs[key]
	if s == nil {
		s = &replayStat{}
		r.devs[key] = s
	}
	return s
}

func (r *replayStats) reject(devicePub []byte, seq uint64, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.get(hex.EncodeToString(devicePub))
	s.rejected++
	s.lastRejected = now
	if seq > s.maxRejected {
		s.maxRejected = seq
	}
}

func (r *replayStats) accept(devicePub []byte, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.get(hex.EncodeToString(devicePub))
	s.accepted++
	s.lastAccepted = now
}

// ReplayReport is one line per device this mesh has a mark for or has heard:
// its name if known, the mark (the highest number accepted, from disk or this
// run), and since this process started how many announces were rejected, the
// newest number among them, and when one was last accepted. A mark above the
// newest number rejected, with nothing accepted, is a mark ahead of the peer.
func (m *Mesh) ReplayReport(now time.Time) string {
	marks := m.guard.Snapshot()
	names := map[string]string{}
	for _, p := range m.roster.Peers() {
		names[hex.EncodeToString(p.DevicePub)] = p.Name
	}
	m.replays.mu.Lock()
	stats := map[string]replayStat{}
	for k, v := range m.replays.devs {
		stats[k] = *v
	}
	m.replays.mu.Unlock()

	keys := map[string]bool{}
	for k := range marks {
		keys[k] = true
	}
	for k := range stats {
		keys[k] = true
	}
	order := make([]string, 0, len(keys))
	for k := range keys {
		order = append(order, k)
	}
	sort.Strings(order)

	ago := func(t time.Time) string {
		if t.IsZero() {
			return "never"
		}
		return now.Sub(t).Round(time.Second).String() + " ago"
	}
	var b strings.Builder
	for _, k := range order {
		name := names[k]
		if name == "" {
			name = "?"
		}
		mark, known := marks[k]
		markS := "-"
		if known {
			markS = fmt.Sprint(mark)
		}
		s := stats[k]
		fmt.Fprintf(&b, "%s %-12s mark=%s accepted=%d (last %s) rejected=%d (newest %d, last %s)\n",
			k[:16], name, markS, s.accepted, ago(s.lastAccepted), s.rejected, s.maxRejected, ago(s.lastRejected))
	}
	return b.String()
}
