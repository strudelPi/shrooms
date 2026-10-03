package agent

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Transcriber turns a voice note into text on this machine, with whisper.cpp:
// the audio never leaves the owner's devices, whatever speech engine the phone
// happens to ship (docs/agents.md).
type Transcriber struct {
	Whisper string // whisper-cli
	Model   string // a ggml model file
	FFmpeg  string
	FFprobe string
	Threads int
}

var validLang = regexp.MustCompile(`^(auto|[a-z]{2,3})$`)

// Transcribe returns the text of an audio file in any format ffmpeg reads.
//
// lang is a language code, or "auto" to let the model detect it — which costs
// a whole extra pass of the encoder, doubling the time (measured 2026-10-03:
// 19 s against 6 s for an 11-second clip), so the phone names it.
//
// The encoder is given a window as long as the clip rather than the 30
// seconds whisper always pads to (--audio-ctx): 13 s → 5.4 s on the same clip,
// the same words.
func (t *Transcriber) Transcribe(ctx context.Context, audio, lang string) (string, error) {
	if lang == "" {
		lang = "auto"
	}
	if !validLang.MatchString(lang) {
		return "", fmt.Errorf("language %q", lang)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	wav := strings.TrimSuffix(audio, filepath.Ext(audio)) + ".16k.wav"
	defer os.Remove(wav)
	if out, err := exec.CommandContext(ctx, t.FFmpeg, "-loglevel", "error", "-y", "-i", audio,
		"-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", wav).CombinedOutput(); err != nil {
		return "", fmt.Errorf("converting the audio: %v: %s", err, bytes.TrimSpace(out))
	}

	args := []string{"-m", t.Model, "-f", wav, "-l", lang, "-nt", "-np", "-t", strconv.Itoa(t.threads())}
	if secs, err := t.duration(ctx, wav); err == nil {
		args = append(args, "-ac", strconv.Itoa(audioCtx(secs)))
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, t.Whisper, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("transcribing: %v: %s", err, lastLine(stderr.String()))
	}
	return strings.Join(strings.Fields(stdout.String()), " "), nil
}

func (t *Transcriber) threads() int {
	if t.Threads > 0 {
		return t.Threads
	}
	return 8
}

func (t *Transcriber) duration(ctx context.Context, wav string) (float64, error) {
	out, err := exec.CommandContext(ctx, t.FFprobe, "-v", "error", "-show_entries", "format=duration",
		"-of", "csv=p=0", wav).Output()
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}

// audioCtx is the encoder window for a clip: 1500 frames is whisper's full 30
// seconds, so the clip's share of that, plus a margin — a window cut exactly
// at the last word loses it.
func audioCtx(secs float64) int {
	n := int(math.Ceil(secs/30*1500)) + 128
	return min(max(n, 256), 1500)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
