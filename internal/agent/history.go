package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Said is one line of a conversation's history, from Claude Code's own
// transcript: what the user typed and what the model answered, without the
// tool calls in between.
type Said struct {
	Time time.Time `json:"time"`
	Role string    `json:"role"` // user or assistant
	Text string    `json:"text"`
}

// historyTail is how much of the end of a transcript is read. A long
// conversation's transcript runs to hundreds of megabytes — this one did, at
// 160 MB — and the phone wants the last few exchanges, not the whole file, and
// wants them quickly.
const historyTail = 4 << 20

// History returns up to limit of the conversation's last exchanges before
// `before` (zero for now), oldest first. Covers what was said before the
// session came to this agent — in a terminal, say — which its own events do
// not hold.
func (s *Session) History(before time.Time, limit int) ([]Said, error) {
	s.mu.Lock()
	id := s.claudeID
	s.mu.Unlock()
	if id == "" {
		return nil, nil
	}
	path, err := transcriptPath(id)
	if err != nil || path == "" {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readHistory(f, before, limit)
}

// transcriptPath finds a conversation's transcript under Claude Code's
// projects directory, by its id: the directory name is derived from the
// working directory by rules this should not have to copy.
func transcriptPath(id string) (string, error) {
	base, err := claudeDir()
	if err != nil {
		return "", err
	}
	m, err := filepath.Glob(filepath.Join(base, "projects", "*", id+".jsonl"))
	if err != nil || len(m) == 0 {
		return "", err
	}
	return m[0], nil
}

func readHistory(f *os.File, before time.Time, limit int) ([]Said, error) {
	return readTail(f, historyTail, before, limit)
}

// readTail reads the exchanges in the last `tail` bytes of a transcript.
func readTail(f *os.File, tail int64, before time.Time, limit int) ([]Said, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := st.Size() - tail
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(f, 1<<20)
	if start > 0 {
		r.ReadBytes('\n') // the first line is cut; skip it
	}
	var out []Said
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if s, ok := parseTranscriptLine(line); ok && (before.IsZero() || s.Time.Before(before)) {
				out = append(out, s)
			}
		}
		if err != nil {
			break
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// parseTranscriptLine keeps the turns a person would recognise: what was typed,
// and the model's text. Tool results come back as user messages with array
// content, and command output, reminders and notifications are user messages
// that start with a tag — none of those were typed by anybody.
func parseTranscriptLine(line []byte) (Said, bool) {
	var e struct {
		Type      string    `json:"type"`
		IsMeta    bool      `json:"isMeta"`
		Sidechain bool      `json:"isSidechain"`
		Timestamp time.Time `json:"timestamp"`
		Message   struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil || e.IsMeta || e.Sidechain {
		return Said{}, false
	}
	switch e.Type {
	case "user":
		var text string
		if json.Unmarshal(e.Message.Content, &text) != nil {
			return Said{}, false
		}
		text = strings.TrimSpace(text)
		if text == "" || strings.HasPrefix(text, "<") {
			return Said{}, false
		}
		return Said{Time: e.Timestamp, Role: "user", Text: text}, true
	case "assistant":
		var blocks []struct{ Type, Text string }
		if json.Unmarshal(e.Message.Content, &blocks) != nil {
			return Said{}, false
		}
		var b strings.Builder
		for _, c := range blocks {
			if c.Type == "text" {
				b.WriteString(c.Text)
			}
		}
		text := strings.TrimSpace(b.String())
		if text == "" {
			return Said{}, false
		}
		return Said{Time: e.Timestamp, Role: "assistant", Text: text}, true
	}
	return Said{}, false
}
