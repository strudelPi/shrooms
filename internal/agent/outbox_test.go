package agent

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func postJSON(t *testing.T, url, body string) (int, string) {
	t.Helper()
	r, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b := new(strings.Builder)
	buf := make([]byte, 4096)
	n, _ := r.Body.Read(buf)
	b.Write(buf[:n])
	return r.StatusCode, b.String()
}

func countKind(s *Session, kind string) int {
	ev, ch := s.Since(0)
	s.Unsubscribe(ch)
	n := 0
	for _, e := range ev {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// A device's outbox sends again when it cannot tell whether the first try
// arrived; the id makes that harmless — once, even across an agent restart.
func TestAMessageSentTwiceIsSentOnce(t *testing.T) {
	state := t.TempDir()
	m := newTestManager(t, state)
	m.Create("proj", t.TempDir())
	s, _ := m.Get("proj")
	srv := httptest.NewServer(Handler(slog.New(slog.DiscardHandler), m, nil))
	t.Cleanup(srv.Close)
	url := srv.URL + "/v1/sessions/proj/messages"

	if code, _ := postJSON(t, url, `{"text":"hello","id":"m-1"}`); code != http.StatusAccepted {
		t.Fatalf("first: %d", code)
	}
	waitFor(t, s, 0, func(e Event) bool { return claudeType(e) == "result/success" })
	if code, body := postJSON(t, url, `{"text":"hello","id":"m-1"}`); code != http.StatusOK || !strings.Contains(body, `"duplicate":true`) {
		t.Errorf("again: %d %s", code, body)
	}
	if n := countKind(s, "message"); n != 1 {
		t.Errorf("%d messages, want 1", n)
	}
	// Without an id, as an older app sends: every one is a message.
	postJSON(t, url, `{"text":"hello"}`)
	if n := countKind(s, "message"); n != 2 {
		t.Errorf("%d messages after one without an id", n)
	}

	// The ids come back from the log after a restart.
	s.stop()
	m2, _ := NewManager(t.Context(), slog.New(slog.DiscardHandler), state, fakeClaudeBin(t))
	t.Cleanup(func() {
		if s2, ok := m2.Get("proj"); ok {
			s2.stop()
		}
	})
	s2, _ := m2.Get("proj")
	if dup, err := s2.SendID("hello", "", "m-1"); !dup || err != nil {
		t.Errorf("after a restart: duplicate %v, %v", dup, err)
	}
}

// A voice note is a turn: kept, transcribed here, and what was said sent as
// the device's message — with no round trip to read it first.
func TestAVoiceNoteIsSentAsWhatWasSaid(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.Create("proj", t.TempDir())
	s, _ := m.Get("proj")
	m.STT, _ = fakeTools(t, "4")
	srv := httptest.NewServer(Handler(slog.New(slog.DiscardHandler), m, nil))
	t.Cleanup(srv.Close)
	url := srv.URL + "/v1/sessions/proj/voice?name=note.m4a&id=v-1"

	r, err := http.Post(url, "audio/mp4", strings.NewReader("aac"))
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ Path string }
	json.NewDecoder(r.Body).Decode(&out)
	r.Body.Close()
	if r.StatusCode != http.StatusAccepted || !strings.HasSuffix(out.Path, "-note.m4a") {
		t.Fatalf("%s %+v", r.Status, out)
	}
	msg := waitFor(t, s, 0, func(e Event) bool { return e.Kind == "message" })
	var d struct{ Text, Voice, ID string }
	json.Unmarshal(msg.Data, &d)
	if d.Text != "ahoj, tady Vašek" || d.Voice != out.Path || d.ID != "v-1" {
		t.Errorf("the turn: %s", msg.Data)
	}
	waitFor(t, s, 0, func(e Event) bool { return assistantText(e) == "echo: ahoj, tady Vašek" })
	if countKind(s, "voice") != 1 {
		t.Error("no transcribing event first")
	}
	// Sent again: not again.
	r, _ = http.Post(url, "audio/mp4", strings.NewReader("aac"))
	r.Body.Close()
	if r.StatusCode != http.StatusOK || countKind(s, "message") != 1 {
		t.Errorf("again: %s, %d messages", r.Status, countKind(s, "message"))
	}
}

// One that cannot be transcribed says so, rather than vanishing.
func TestAVoiceNoteThatFailsSaysSo(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.Create("proj", t.TempDir())
	s, _ := m.Get("proj")
	tr, _ := fakeTools(t, "4")
	bad := filepath.Join(t.TempDir(), "whisper")
	os.WriteFile(bad, []byte("#!/bin/sh\necho 'model not found' >&2\nexit 3\n"), 0o755)
	tr.Bin = bad
	m.STT = tr
	srv := httptest.NewServer(Handler(slog.New(slog.DiscardHandler), m, nil))
	t.Cleanup(srv.Close)
	r, _ := http.Post(srv.URL+"/v1/sessions/proj/voice?name=note.m4a&id=v-2", "audio/mp4", strings.NewReader("aac"))
	r.Body.Close()
	failed := waitFor(t, s, 0, func(e Event) bool { return e.Kind == "voice" && strings.Contains(string(e.Data), `"failed"`) })
	if !strings.Contains(string(failed.Data), "model not found") {
		t.Errorf("why it failed is not said: %s", failed.Data)
	}
	if countKind(s, "message") != 0 {
		t.Error("a failed note was sent anyway")
	}

	// Kept, so it can be tried again: once the model is there, it goes.
	if code, _ := postJSON(t, srv.URL+"/v1/sessions/proj/voice/v-1/retry", ""); code != http.StatusConflict {
		t.Errorf("retrying one that does not exist: %d", code)
	}
	good, _ := fakeTools(t, "4")
	tr.Bin = good.Bin
	if code, body := postJSON(t, srv.URL+"/v1/sessions/proj/voice/v-2/retry", ""); code != http.StatusAccepted {
		t.Fatalf("retry: %d %s", code, body)
	}
	msg := waitFor(t, s, 0, func(e Event) bool { return e.Kind == "message" })
	if !strings.Contains(string(msg.Data), `"id":"v-2"`) || !strings.Contains(string(msg.Data), "ahoj, tady") {
		t.Errorf("the retried note: %s", msg.Data)
	}
	if code, _ := postJSON(t, srv.URL+"/v1/sessions/proj/voice/v-2/retry", ""); code != http.StatusConflict {
		t.Errorf("retrying one that went: %d", code)
	}
}

// Silence is not a message.
func TestAnEmptyTranscriptIsAFailure(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.Create("proj", t.TempDir())
	s, _ := m.Get("proj")
	tr, _ := fakeTools(t, "4")
	quiet := filepath.Join(t.TempDir(), "whisper")
	os.WriteFile(quiet, []byte("#!/bin/sh\necho '   '\n"), 0o755)
	tr.Bin = quiet
	m.STT = tr
	srv := httptest.NewServer(Handler(slog.New(slog.DiscardHandler), m, nil))
	t.Cleanup(srv.Close)
	r, _ := http.Post(srv.URL+"/v1/sessions/proj/voice?name=note.m4a&id=v-3", "audio/mp4", strings.NewReader("aac"))
	r.Body.Close()
	waitFor(t, s, 0, func(e Event) bool { return e.Kind == "voice" && strings.Contains(string(e.Data), "nothing was heard") })
	if countKind(s, "message") != 0 {
		t.Error("silence was sent")
	}
}
