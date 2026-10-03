package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"time"
	"unicode"
)

// Found is one turn of a conversation that matched a search: what somebody
// typed or what the model said. Seq is the event it is in, to jump to; 0 when
// it was said before this agent had the conversation (in a terminal, say) and
// is only in the transcript.
type Found struct {
	Seq     uint64    `json:"seq"`
	Time    time.Time `json:"time"`
	Role    string    `json:"role"` // user or assistant
	Snippet string    `json:"snippet"`
	// Text is the whole turn, up to maxFoundText, only for a turn there is no
	// event to jump to: the others are read where they are.
	Text string `json:"text,omitempty"`
}

const (
	maxFoundText   = 16 << 10
	snippetContext = 80 // runes either side of the match
)

// Search finds the turns of the conversation that contain q, newest first, at
// most limit of them. Case and Czech diacritics are ignored: "vasek" finds
// "Vašek". It reads everything on disk — the session's events and, for what
// came before them, the transcript — so it finds what the apps have not loaded.
func (s *Session) Search(q string, limit int) ([]Found, error) {
	needle := []rune(fold(strings.TrimSpace(q)))
	if len(needle) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	id := s.claudeID
	s.mu.Unlock()

	var out []Found
	first := time.Time{}
	if f, err := os.Open(s.eventsPath()); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			line := sc.Bytes()
			if !first.IsZero() && !mayHoldTurn(line) && !bytes.Contains(line, []byte(`"kind":"message"`)) {
				continue
			}
			var e Event
			if json.Unmarshal(line, &e) != nil {
				continue
			}
			if first.IsZero() && !e.Time.IsZero() {
				first = e.Time
			}
			for _, t := range eventTurns(e) {
				if f, ok := match(t, needle); ok {
					f.Seq = e.Seq
					f.Text = ""
					out = append(out, f)
				}
			}
		}
		f.Close()
	}

	// What was said before the first event: the same rule as the apps use to
	// show the transcript above the events.
	if id != "" {
		var earlier []Found
		if path, err := transcriptPath(id); err == nil && path != "" {
			if f, err := os.Open(path); err == nil {
				r := bufio.NewReaderSize(f, 1<<20)
				for {
					line, err := r.ReadBytes('\n')
					if !mayHoldTurn(line) {
						// most of a transcript: tool calls and their output
					} else if t, ok := parseTranscriptLine(line); ok && (first.IsZero() || t.Time.Before(first)) {
						if f, ok := match(t, needle); ok {
							earlier = append(earlier, f)
						}
					}
					if err != nil {
						break
					}
				}
				f.Close()
			}
		}
		out = append(earlier, out...)
	}

	// Newest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// mayHoldTurn is a quick look at a line of a transcript or of the events
// before parsing it: whether it can hold a typed message (string content) or
// the model's text. Most lines are tool calls and their output; parsing them
// all made a search of a 190 MB transcript take 2.6 seconds. Loose on purpose
// (key order is not relied on): a line it lets through is parsed properly.
func mayHoldTurn(line []byte) bool {
	return bytes.Contains(line, []byte(`"content":"`)) || bytes.Contains(line, []byte(`"type":"text"`))
}

// eventTurns is what an event holds that a person would search for: a message
// sent from a device, or the model's text. Tool calls and their output are not
// turns.
func eventTurns(e Event) []Said {
	switch e.Kind {
	case "message":
		var d struct{ Text string }
		if json.Unmarshal(e.Data, &d) == nil && d.Text != "" {
			return []Said{{Time: e.Time, Role: "user", Text: d.Text}}
		}
	case "claude":
		var d struct {
			Type    string
			Message struct {
				Content []struct{ Type, Text string }
			}
		}
		if json.Unmarshal(e.Data, &d) != nil || d.Type != "assistant" {
			return nil
		}
		var out []Said
		for _, c := range d.Message.Content {
			if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
				out = append(out, Said{Time: e.Time, Role: "assistant", Text: strings.TrimSpace(c.Text)})
			}
		}
		return out
	}
	return nil
}

// match reports whether the turn contains the folded needle, with a snippet
// around the first place it does.
func match(t Said, needle []rune) (Found, bool) {
	text := []rune(t.Text)
	hay := []rune(fold(t.Text))
	at := indexRunes(hay, needle)
	if at < 0 {
		return Found{}, false
	}
	from, to := max(0, at-snippetContext), min(len(text), at+len(needle)+snippetContext)
	snip := strings.Join(strings.Fields(string(text[from:to])), " ")
	if from > 0 {
		snip = "…" + snip
	}
	if to < len(text) {
		snip += "…"
	}
	full := t.Text
	if len(full) > maxFoundText {
		full = string([]rune(full[:maxFoundText])) + "…"
	}
	return Found{Time: t.Time, Role: t.Role, Snippet: snip, Text: full}, true
}

func indexRunes(hay, needle []rune) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		j := 0
		for j < len(needle) && hay[i+j] == needle[j] {
			j++
		}
		if j == len(needle) {
			return i
		}
	}
	return -1
}

// fold lowercases and drops the diacritics of Czech and the commoner Latin
// letters, rune for rune, so positions in the folded text are positions in
// the original.
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		r = unicode.ToLower(r)
		if f, ok := plain[r]; ok {
			return f
		}
		return r
	}, s)
}

var plain = map[rune]rune{
	'á': 'a', 'ä': 'a', 'à': 'a', 'â': 'a', 'č': 'c', 'ç': 'c', 'ď': 'd',
	'é': 'e', 'ě': 'e', 'ë': 'e', 'è': 'e', 'ê': 'e', 'í': 'i', 'ï': 'i',
	'ľ': 'l', 'ĺ': 'l', 'ň': 'n', 'ñ': 'n', 'ó': 'o', 'ö': 'o', 'ô': 'o',
	'ř': 'r', 'ŕ': 'r', 'š': 's', 'ß': 's', 'ť': 't', 'ú': 'u', 'ů': 'u',
	'ü': 'u', 'ý': 'y', 'ÿ': 'y', 'ž': 'z',
}
