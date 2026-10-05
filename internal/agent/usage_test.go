package agent

import (
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Who asked is who pays: turns asked for over the mesh are the asking
// device's, turns from this machine's socket are "", each with the tokens its
// result reported and what it added to the running cost — the session's first
// result only setting where that count starts.
func TestUsageIsCountedPerDevice(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.Create("proj", t.TempDir())
	s, _ := m.Get("proj")
	who := func(a netip.Addr) string { return "phone.office" }
	srv := httptest.NewServer(Handler(slog.New(slog.DiscardHandler), m, who))
	t.Cleanup(srv.Close)

	results := 0
	turn := func(send func()) {
		send()
		results++
		waitFor(t, s, 0, func(e Event) bool { return claudeType(e) == "result/success" && countResults(s) == results })
	}
	turn(func() { postJSON(t, srv.URL+"/v1/sessions/proj/messages", `{"text":"one"}`) })
	turn(func() { postJSON(t, srv.URL+"/v1/sessions/proj/messages", `{"text":"two"}`) })
	turn(func() { s.Send("from this machine", "") })

	get := func() map[string]UsageRow {
		r, err := http.Get(srv.URL + "/v1/usage")
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var out struct {
			Machine string
			Rows    []UsageRow
		}
		json.NewDecoder(r.Body).Decode(&out)
		if out.Machine == "" {
			t.Error("no machine named")
		}
		rows := map[string]UsageRow{}
		for _, row := range out.Rows {
			rows[row.By] = row
		}
		return rows
	}
	rows := get()
	phone, local := rows["phone.office"], rows[""]
	if phone.Turns != 2 || phone.Output != 100 || phone.CacheRead != 2000 || phone.Input != 20 || !near(phone.CostUSD, 0.01) {
		t.Errorf("the phone's: %+v", phone)
	}
	if local.Turns != 1 || local.Output != 50 || !near(local.CostUSD, 0.01) {
		t.Errorf("this machine's: %+v", local)
	}
	if phone.Session != "proj" || phone.Day != time.Now().Format("2006-01-02") || phone.Model == "" {
		t.Errorf("labels: %+v", phone)
	}
	// Read again, and after one more turn: what was counted is not counted twice.
	if again := get(); again["phone.office"].Turns != 2 {
		t.Errorf("read twice: %+v", again["phone.office"])
	}
	turn(func() { postJSON(t, srv.URL+"/v1/sessions/proj/messages", `{"text":"three"}`) })
	if after := get(); after["phone.office"].Turns != 3 || !near(after["phone.office"].CostUSD, 0.02) {
		t.Errorf("after another turn: %+v", after["phone.office"])
	}
}

func countResults(s *Session) int {
	ev, ch := s.Since(0)
	s.Unsubscribe(ch)
	n := 0
	for _, e := range ev {
		if claudeType(e) == "result/success" {
			n++
		}
	}
	return n
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// The rules a log is read by, on one written out: a harness whose result has
// no tokens has its messages' summed; the first result only sets where the
// running cost starts; a turn's cost is what it took the total above its
// highest — a restart does not reset it (Claude Code carries it over) and a
// dip adds nothing; a turn left waiting for hours is not hours of work; days
// are the machine's local dates.
func TestUsageReadsALog(t *testing.T) {
	day1 := time.Date(2026, 10, 4, 23, 50, 0, 0, time.Local)
	day2 := day1.Add(20 * time.Minute) // past midnight
	var b strings.Builder
	seq := 0
	ev := func(at time.Time, kind, by, data string) {
		seq++
		e := map[string]any{"seq": seq, "time": at, "kind": kind, "by": by, "data": json.RawMessage(data)}
		line, _ := json.Marshal(e)
		b.Write(line)
		b.WriteByte('\n')
	}
	ev(day1, "claude", "", `{"type":"system","subtype":"init","model":"ollama/qwen3"}`)
	ev(day1, "message", "phone.office", `{"text":"taken over from a terminal"}`)
	ev(day1, "claude", "", `{"type":"result","subtype":"success","total_cost_usd":400,"usage":{"output_tokens":1}}`)
	ev(day1, "message", "phone.office", `{"text":"hi"}`)
	ev(day1.Add(time.Second), "claude", "", `{"type":"assistant","message":{"usage":{"input_tokens":100,"output_tokens":7}}}`)
	ev(day1.Add(2*time.Second), "claude", "", `{"type":"assistant","message":{"usage":{"input_tokens":120,"output_tokens":9}}}`)
	ev(day1.Add(3*time.Second), "claude", "", `{"type":"result","subtype":"success","total_cost_usd":400.05}`)
	ev(day1.Add(4*time.Second), "stopped", "", `{"reason":"idle"}`)
	ev(day2, "message", "laptop.home", `{"text":"again"}`)
	ev(day2.Add(5*time.Second), "claude", "", `{"type":"result","subtype":"success","total_cost_usd":399.9,"usage":{"input_tokens":1,"output_tokens":2}}`)
	ev(day2.Add(time.Minute), "message", "laptop.home", `{"text":"left waiting"}`)
	ev(day2.Add(5*time.Hour), "claude", "", `{"type":"result","subtype":"success","total_cost_usd":400.35,"duration_ms":4000}`)
	path := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(path, []byte(b.String()), 0o600)

	sc := &usageScan{rows: map[usageKey]*UsageRow{}}
	sc.read(path, "s", "pi")
	got := map[string]UsageRow{}
	for _, r := range sc.rows {
		got[r.Day+" "+r.By] = *r
	}
	first := got["2026-10-04 phone.office"]
	// The first result: its turn and tokens, none of the 400 it arrived with.
	if first.Turns != 2 || first.Input != 220 || first.Output != 17 || !near(first.CostUSD, 0.05) || first.BusyMs != 3000 ||
		first.Model != "ollama/qwen3" || first.Harness != "pi" {
		t.Errorf("summed from its messages: %+v", first)
	}
	second := got["2026-10-05 laptop.home"]
	// After the restart the total dips to 399.9: nothing. Then 400.35 is
	// 0.30 above the peak of 400.05 — not 0.45 above the dip. The turn left
	// waiting five hours counts its own 4 s, not five hours.
	if second.Turns != 2 || second.Output != 2 || !near(second.CostUSD, 0.3) || second.BusyMs != 5000+4000 {
		t.Errorf("after a restart, past midnight: %+v", second)
	}

	// Only what the log gained is read again.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	b.Reset()
	ev(day2.Add(6*time.Hour), "message", "phone.office", `{"text":"later"}`)
	ev(day2.Add(6*time.Hour+time.Second), "claude", "", `{"type":"result","subtype":"success","total_cost_usd":400.45,"usage":{"output_tokens":5}}`)
	f.WriteString(b.String())
	f.Close()
	sc.read(path, "s", "pi")
	if r := sc.rows[usageKey{"2026-10-05", "phone.office", "ollama/qwen3"}]; r == nil || r.Turns != 1 || r.Output != 5 {
		t.Errorf("the appended turn: %+v", r)
	}
	if r := sc.rows[usageKey{"2026-10-04", "phone.office", "ollama/qwen3"}]; r.Turns != 2 {
		t.Errorf("counted again: %+v", r)
	}
}
