package memstat

import (
	"runtime"
	"strings"
	"testing"
)

// The kernel's own format, as /proc/self/status writes it.
const sampleStatus = `Name:	shrooms
VmPeak:	 2470144 kB
VmRSS:	  163632 kB
RssAnon:	  130528 kB
RssFile:	   33104 kB
RssShmem:	       0 kB
Threads:	27
`

func TestParseStatus(t *testing.T) {
	rss, anon, file := parseStatus(strings.NewReader(sampleStatus))
	if rss != 163632*1024 || anon != 130528*1024 || file != 33104*1024 {
		t.Errorf("parsed rss=%d anon=%d file=%d", rss, anon, file)
	}
}

// Read on the platform this runs on. On Linux the kernel figures must be
// there, Go must account for some of them, and the split must add up.
func TestReadSplitsGoFromTheRest(t *testing.T) {
	s := Read()
	if s.GoHeapInuse == 0 || s.GoRetained == 0 {
		t.Fatalf("no Go figures: %+v", s)
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		return
	}
	if s.RSS == 0 || s.RSSAnon == 0 {
		t.Fatalf("no kernel figures on %s: %+v", runtime.GOOS, s)
	}
	if s.RSSAnon > s.GoRetained+s.AppRuntime && s.Native != s.RSSAnon-s.GoRetained-s.AppRuntime {
		t.Errorf("native %d is not anon %d minus Go %d minus app runtime %d",
			s.Native, s.RSSAnon, s.GoRetained, s.AppRuntime)
	}
}

// Memory Go holds must not be counted as native. Otherwise "the delivery
// library uses N MB" would be inflated by whatever Go happens to have
// allocated. (The other direction — a C allocation appearing as native — needs
// cgo, which test files cannot use; the daemon's own figures check it.)
func TestGoMemoryIsNotCountedAsNative(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc/self/status")
	}
	// The race detector keeps shadow memory for every Go allocation — several
	// times its size — and allocates it outside Go's accounting. It really is
	// memory Go's runtime does not report, so native grows (by about 70 MB for
	// these 32 MB, found when `make test`, which runs -race, failed in CI).
	// The claim here holds only for the binaries shrooms ships, built without it.
	if raceDetector {
		t.Skip("-race: the detector's shadow memory is native, by design")
	}
	before := Read().Native
	buf := make([]byte, 32<<20)
	for i := range buf {
		buf[i] = 1
	}
	after := Read()
	if grew := int64(after.Native) - int64(before); grew > 8<<20 {
		t.Errorf("Go-held memory was counted as native: native grew by %d", grew)
	}
	runtime.KeepAlive(buf)
}

// Abridged from a real Android process: ART's regions are named, and only
// their Anonymous pages count. The ashmem region is how Android 9 and older
// hold the Java heap — shared memory, Anonymous 0, so it adds nothing.
const sampleSmaps = `12c00000-2ac00000 rw-p 00000000 00:00 0                                  [anon:dalvik-main space (region space)]
Size:             524288 kB
Rss:                8192 kB
Anonymous:          8000 kB
70a1c000-70a2c000 rw-p 00000000 00:00 0                                  [anon:dalvik-LinearAlloc]
Size:                 64 kB
Anonymous:            64 kB
7100000000-7100800000 rw-p 00000000 00:00 0                              [anon:scudo:primary]
Size:               8192 kB
Anonymous:          5000 kB
7200000000-7200400000 rw-p 00000000 00:00 0
Size:               4096 kB
Anonymous:          4096 kB
7300000000-7300100000 rw-s 00000000 00:05 1234                           /dev/ashmem/dalvik-main space (deleted)
Size:               1024 kB
Rss:                1024 kB
Anonymous:             0 kB
7400000000-7400010000 r-xp 00000000 fd:00 99                             /data/app/xyz.vpavlin.shrooms/lib/arm64/liblogosdelivery.so
Size:                 64 kB
Anonymous:             8 kB
`

func TestAppRuntimeCountsOnlyARTsAnonymousPages(t *testing.T) {
	got := parseAppRuntime(strings.NewReader(sampleSmaps))
	if want := uint64(8000+64) * 1024; got != want {
		t.Errorf("app runtime = %d kB, want %d kB: the malloc, unnamed and library regions are not ART's, and ashmem is not anonymous",
			got/1024, want/1024)
	}
}

// Off Android there is no ART, so nothing is taken away from native.
func TestNoAppRuntimeHere(t *testing.T) {
	if runtime.GOOS == "android" {
		t.Skip("this is Android")
	}
	if s := Read(); s.AppRuntime != 0 {
		t.Errorf("found %d bytes of Android runtime on %s", s.AppRuntime, runtime.GOOS)
	}
}
