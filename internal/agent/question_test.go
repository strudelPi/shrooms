package agent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A question from the model waits for the person even with auto-approve on —
// auto-approve answering it is what made one read to the model as "the user
// did not answer" (2026-10-03) — and the answer given reaches the model.
func TestAQuestionWaitsForItsAnswerEvenWithAutoApprove(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.Create("proj", t.TempDir())
	s, _ := m.Get("proj")
	if err := s.SetAutoApprove(true, ""); err != nil {
		t.Fatal(err)
	}
	s.Send("ask Deploy now?", "nothing")
	waitFor(t, s, 0, func(e Event) bool { return claudeType(e) == "control_request" })
	time.Sleep(200 * time.Millisecond)
	if in := s.Info(); in.State != Waiting || in.Pending != 1 {
		t.Fatalf("the question did not wait: %+v", in)
	}
	// Switching auto-approve on again (it allows what is waiting) leaves it too.
	s.SetAutoApprove(true, "")
	if in := s.Info(); in.Pending != 1 {
		t.Fatalf("auto-approve answered the question: %+v", in)
	}

	srv := httptest.NewServer(Handler(slog.New(slog.DiscardHandler), m, nil))
	t.Cleanup(srv.Close)
	post := func(body any) int {
		b, _ := json.Marshal(body)
		r, err := http.Post(srv.URL+"/v1/sessions/proj/prompts/req-q", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if code := post(map[string]any{"allow": true}); code != http.StatusConflict {
		t.Errorf("allowed without answers: %d", code)
	}
	if code := post(map[string]any{"allow": true, "answers": map[string]string{"Deploy now?": "yes"}}); code != http.StatusNoContent {
		t.Fatalf("answering: %d", code)
	}
	got := waitFor(t, s, 0, func(e Event) bool {
		t := assistantText(e)
		return t == "answered: yes" || t == "The user did not answer the questions."
	})
	if assistantText(got) != "answered: yes" {
		t.Errorf("the model got %q", assistantText(got))
	}
	ans := waitFor(t, s, 0, func(e Event) bool { return e.Kind == "answer" })
	var d struct {
		Answers map[string]string
	}
	json.Unmarshal(ans.Data, &d)
	if ans.By == "auto-approve" || d.Answers["Deploy now?"] != "yes" {
		t.Errorf("the answer event: by %q, %s", ans.By, ans.Data)
	}
}

// Declining a question works as declining any prompt.
func TestAQuestionCanBeDeclined(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.Create("proj", t.TempDir())
	s, _ := m.Get("proj")
	s.Send("ask Deploy now?", "")
	waitFor(t, s, 0, func(e Event) bool { return claudeType(e) == "control_request" })
	time.Sleep(100 * time.Millisecond)
	if err := s.Answer("req-q", false, "not tonight", nil, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, 0, func(e Event) bool { return assistantText(e) == "denied: not tonight" })
}
