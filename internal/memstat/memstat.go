// Package memstat says where a shrooms process's memory goes.
//
// The question it exists for: could shrooms run inside an iOS packet tunnel
// extension, which gets 50 MiB (docs/shrooms-on-ios.md)? A lone Edge delivery
// node measured about 12 MB of dirty memory, while the laptop daemon — also an
// Edge node — held about 130 MB. Nothing reported which part of a running
// process the rest belonged to: the daemon runs as root, so its /proc entries
// are closed to the user asking, and a phone shows nothing without adb.
//
// So every process reports it about itself: the resident set from the kernel,
// and how much of it Go's runtime accounts for. The difference is memory
// nothing in Go allocated — in shrooms, overwhelmingly the native delivery
// library, which keeps its own heap.
package memstat

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Snapshot is one reading, in bytes.
type Snapshot struct {
	// From the kernel (/proc/self/status). Zero where that file does not
	// exist, which is every platform but Linux and Android.
	RSS     uint64 `json:"rss"`
	RSSAnon uint64 `json:"rss_anon"` // dirty memory: what an iOS limit would count
	RSSFile uint64 `json:"rss_file"` // mapped files, chiefly code; can be dropped and reloaded

	// From Go's runtime.
	GoHeapInuse uint64 `json:"go_heap_inuse"`
	GoStacks    uint64 `json:"go_stacks"`
	// GoRetained is everything Go holds from the OS and has not given back:
	// Sys minus HeapReleased. An upper bound on Go's share of RSSAnon — some
	// of it may be idle and paged out — so Native below is a lower bound.
	GoRetained uint64 `json:"go_retained"`

	// AppRuntime is the Android runtime's dirty memory — the Java heap and
	// everything else ART maps — summed from the regions it names
	// "[anon:dalvik-…]". The app's screens run in the same process as the
	// tunnel, so on a phone this is part of RSSAnon that an iOS extension,
	// which has no UI, would not carry. Zero off Android.
	AppRuntime uint64 `json:"app_runtime,omitempty"`

	// Native is RSSAnon minus GoRetained and AppRuntime, floored at zero:
	// dirty memory neither Go nor the Android runtime allocated. In shrooms
	// that is mostly the delivery library. Zero when the kernel figure is
	// unavailable, rather than a negative guess.
	Native uint64 `json:"native"`
}

// Read takes a snapshot of this process.
func Read() Snapshot {
	var s Snapshot
	if f, err := os.Open("/proc/self/status"); err == nil {
		s.RSS, s.RSSAnon, s.RSSFile = parseStatus(f)
		f.Close()
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	s.GoHeapInuse = m.HeapInuse
	s.GoStacks = m.StackSys
	if m.Sys > m.HeapReleased {
		s.GoRetained = m.Sys - m.HeapReleased
	}
	if f, err := os.Open("/proc/self/smaps"); err == nil {
		s.AppRuntime = parseAppRuntime(f)
		f.Close()
	}
	if s.RSSAnon > s.GoRetained+s.AppRuntime {
		s.Native = s.RSSAnon - s.GoRetained - s.AppRuntime
	}
	return s
}

// parseAppRuntime sums the Anonymous pages of the Android runtime's regions in
// /proc/self/smaps. Anonymous rather than Rss, so it counts what RssAnon
// counts: on Android 9 and older ART's heap lives in ashmem, which is shared
// memory, outside RssAnon, and reports Anonymous 0 — subtracting its Rss there
// would take away memory that was never in the total.
func parseAppRuntime(r interface{ Read([]byte) (int, error) }) uint64 {
	var total uint64
	inRuntime := false
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// A region header starts with its address range ("7f..-7f.."); its
		// properties ("Rss:", "Anonymous:") end their first field in a colon.
		if !strings.HasSuffix(fields[0], ":") && strings.Contains(fields[0], "-") {
			name := ""
			if len(fields) >= 6 {
				name = strings.Join(fields[5:], " ")
			}
			inRuntime = strings.HasPrefix(name, "[anon:dalvik-") ||
				strings.HasPrefix(name, "/dev/ashmem/dalvik-")
			continue
		}
		if inRuntime && fields[0] == "Anonymous:" && len(fields) >= 2 {
			if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				total += kb * 1024
			}
		}
	}
	return total
}

// parseStatus reads the three resident-set lines from /proc/self/status, whose
// values are in kB.
func parseStatus(r interface{ Read([]byte) (int, error) }) (rss, anon, file uint64) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(val)
		if len(fields) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "VmRSS":
			rss = kb * 1024
		case "RssAnon":
			anon = kb * 1024
		case "RssFile":
			file = kb * 1024
		}
	}
	return rss, anon, file
}

// Format is a snapshot for people, one figure per line: what `shrooms memory`
// prints and what a phone's diagnostics include.
func Format(m Snapshot) string {
	var b strings.Builder
	if m.RSS == 0 {
		b.WriteString("resident    (not available on this platform)\n")
	} else {
		fmt.Fprintf(&b, "resident    %s\n", MiB(m.RSS))
		fmt.Fprintf(&b, "  dirty     %s   what an iOS extension limit counts\n", MiB(m.RSSAnon))
		fmt.Fprintf(&b, "  files     %s   mostly code, can be dropped and reloaded\n", MiB(m.RSSFile))
	}
	fmt.Fprintf(&b, "go          %s held from the OS\n", MiB(m.GoRetained))
	fmt.Fprintf(&b, "  heap      %s in use\n", MiB(m.GoHeapInuse))
	fmt.Fprintf(&b, "  stacks    %s\n", MiB(m.GoStacks))
	if m.AppRuntime != 0 {
		fmt.Fprintf(&b, "app runtime %s   Android's, for the screens; an iOS extension has none\n", MiB(m.AppRuntime))
	}
	if m.RSS != 0 {
		fmt.Fprintf(&b, "native      %s   dirty memory Go did not allocate: mostly the delivery library\n", MiB(m.Native))
	}
	if m.AppRuntime != 0 && m.RSSAnon > m.AppRuntime {
		fmt.Fprintf(&b, "without UI  %s   dirty, less the app runtime: roughly what an extension would hold\n",
			MiB(m.RSSAnon-m.AppRuntime))
	}
	return b.String()
}

// MiB renders bytes for people.
func MiB(b uint64) string {
	return strconv.FormatFloat(float64(b)/(1<<20), 'f', 1, 64) + " MiB"
}
