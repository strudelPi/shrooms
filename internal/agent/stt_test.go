package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTools writes stand-ins for ffmpeg, ffprobe and whisper-cli that record
// how they were called, so Transcribe itself is what is tested.
func fakeTools(t *testing.T, duration string) (*Transcriber, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := func(name, body string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("#!/bin/sh\necho \""+name+" $*\" >> "+log+"\n"+body), 0o755)
		return p
	}
	return &Transcriber{
		// ffmpeg's last argument is the output: make it exist.
		FFmpeg:  script("ffmpeg", `for a; do out=$a; done; : > "$out"`+"\n"),
		FFprobe: script("ffprobe", "echo "+duration+"\n"),
		Bin:     script("whisper", "echo '  ahoj, tady   Vašek  '\necho 'progress noise' >&2\n"),
		Model:   "/models/turbo.bin",
		Threads: 4,
	}, log
}

func TestTranscribeAsksForTheLanguageAndAWindowAsLongAsTheClip(t *testing.T) {
	tr, calls := fakeTools(t, "11.008")
	audio := filepath.Join(t.TempDir(), "note.m4a")
	os.WriteFile(audio, []byte("aac"), 0o600)

	text, err := tr.Transcribe(context.Background(), audio, "cs")
	if err != nil {
		t.Fatal(err)
	}
	if text != "ahoj, tady Vašek" {
		t.Errorf("text %q", text)
	}
	b, _ := os.ReadFile(calls)
	log := string(b)
	for _, want := range []string{"-ar 16000 -ac 1", "-l cs", "-ac 679", "-m /models/turbo.bin", "-t 4"} {
		if !strings.Contains(log, want) {
			t.Errorf("no %q in the calls:\n%s", want, log)
		}
	}
	if _, err := os.Stat(strings.TrimSuffix(audio, ".m4a") + ".16k.wav"); !os.IsNotExist(err) {
		t.Error("the converted wav was left behind")
	}
}

// Parakeet: the same conversion, but no language and no window — it takes
// neither, and detects the language itself.
func TestParakeetIsGivenNoLanguage(t *testing.T) {
	tr, calls := fakeTools(t, "11.008")
	tr.Model = "/models/ggml-parakeet-tdt-0.6b-v3-q4_k.bin"
	audio := filepath.Join(t.TempDir(), "note.m4a")
	os.WriteFile(audio, []byte("aac"), 0o600)
	text, err := tr.Transcribe(context.Background(), audio, "cs")
	if err != nil || text != "ahoj, tady Vašek" {
		t.Fatalf("%q %v", text, err)
	}
	b, _ := os.ReadFile(calls)
	log := string(b)
	if !strings.Contains(log, "-m /models/ggml-parakeet-tdt-0.6b-v3-q4_k.bin -f ") || !strings.Contains(log, "-t 4") {
		t.Errorf("calls:\n%s", log)
	}
	for _, not := range []string{"-l ", "-ac 679", "-nt", "ffprobe"} {
		if strings.Contains(log, not) {
			t.Errorf("parakeet was given %q:\n%s", not, log)
		}
	}
}

func TestTheEngineFollowsTheModel(t *testing.T) {
	for model, want := range map[string]string{
		"/x/ggml-parakeet-tdt-0.6b-v3-q4_k.bin": "parakeet-cli",
		"/x/GGML-Parakeet.bin":                  "parakeet-cli",
		"/x/ggml-large-v3-turbo-q5_0.bin":       "whisper-cli",
		"/parakeet/ggml-small.bin":              "whisper-cli",
	} {
		if got := EngineFor(model); got != want {
			t.Errorf("%s: %s, want %s", model, got, want)
		}
	}
}

func TestTranscribeRefusesALanguageThatIsNotOne(t *testing.T) {
	tr, _ := fakeTools(t, "3")
	if _, err := tr.Transcribe(context.Background(), "/x.m4a", "cs; rm -rf ~"); err == nil {
		t.Error("an argument smuggled in as a language was accepted")
	}
}

func TestTheWindowFollowsTheClip(t *testing.T) {
	for secs, want := range map[float64]int{1: 256, 11.008: 679, 30: 1500, 120: 1500} {
		if got := audioCtx(secs); got != want {
			t.Errorf("%.1f s: %d, want %d", secs, got, want)
		}
	}
}

// Over HTTP: the note is kept like any upload and its text comes back; with no
// model on this machine, the phone is told so rather than sent an error page.
func TestTheTranscribeEndpoint(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	m.Create("proj", t.TempDir())
	srv := httptest.NewServer(Handler(slog.New(slog.DiscardHandler), m, nil))
	t.Cleanup(srv.Close)
	post := func() *http.Response {
		r, err := http.Post(srv.URL+"/v1/sessions/proj/transcribe?name=note.m4a&lang=cs", "audio/mp4", strings.NewReader("aac"))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := post(); r.StatusCode != http.StatusNotImplemented {
		t.Errorf("without a model: %s", r.Status)
	}
	m.STT, _ = fakeTools(t, "4")
	r := post()
	var out struct{ Path, Text string }
	json.NewDecoder(r.Body).Decode(&out)
	if r.StatusCode != http.StatusOK || out.Text != "ahoj, tady Vašek" || !strings.HasSuffix(out.Path, "-note.m4a") {
		t.Errorf("%s: %+v", r.Status, out)
	}
}
