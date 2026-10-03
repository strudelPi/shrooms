package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Event is one numbered entry in a session's history. A phone that was away
// asks for everything after the last number it saw.
type Event struct {
	Seq  uint64    `json:"seq"`
	Time time.Time `json:"time"`
	// Kind is one of:
	//   claude   a message from Claude Code, verbatim in Data
	//   message  a user turn sent from a device: {"text"}
	//   answer   a permission prompt answered: {"prompt","allow","message"}
	//   stopped  the process ended: {"reason"}
	//   partial  reply text as it is written: {"text"}. Live only — never
	//            kept or numbered (its Seq is the last real event's), since
	//            the whole message follows as a claude event.
	Kind string `json:"kind"`
	// By is the device that caused it, for message and answer.
	By   string          `json:"by,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// State is what a session is doing.
type State string

const (
	Idle    State = "idle"    // nothing running, or a turn has finished
	Working State = "working" // a turn is in progress
	Waiting State = "waiting" // a permission prompt needs an answer
)

// Info is a session as the list shows it.
type Info struct {
	Name     string    `json:"name"`
	Dir      string    `json:"dir"`
	State    State     `json:"state"`
	Pending  int       `json:"pending"`
	Running  bool      `json:"running"`
	LastSeq  uint64    `json:"last_seq"`
	LastTime time.Time `json:"last_time,omitempty"`

	// AutoApprove: nothing asks, as with --dangerously-skip-permissions.
	AutoApprove bool `json:"auto_approve"`
	// Context is how much of the model's context window the conversation
	// fills, as of the last reply: what decides when it will be compacted.
	ContextUsed   uint64 `json:"context_used,omitempty"`
	ContextWindow uint64 `json:"context_window,omitempty"`
	// Preview is the start of the last thing the model said.
	Preview string `json:"preview,omitempty"`
}

// Session is one conversation in one directory.
type Session struct {
	name, dir string
	m         *Manager

	mu       sync.Mutex
	claudeID string
	events   []Event // in memory: the tail; the whole history is on disk
	seq      uint64
	subs     map[chan Event]struct{}
	pending  map[string]json.RawMessage // request_id → the can_use_tool request
	state    State
	proc     *proc
	lastUsed time.Time

	autoApprove bool
	ctxUsed     uint64
	ctxWindow   uint64
	preview     string
}

// memoryEvents bounds what a session keeps in memory. Older events are on
// disk and served from there.
const memoryEvents = 2000

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// Manager owns every session on this machine.
type Manager struct {
	log    *slog.Logger
	dir    string // state: sessions.json and one event log per session
	claude string // the claude binary

	// IdleStop is how long a session's process may sit with nothing to do
	// before it is stopped. The conversation is kept and resumed by id.
	IdleStop time.Duration

	ctx context.Context

	mu       sync.Mutex
	sessions map[string]*Session
}

type record struct {
	Name        string `json:"name"`
	Dir         string `json:"dir"`
	ClaudeID    string `json:"claude_id,omitempty"`
	AutoApprove bool   `json:"auto_approve,omitempty"`
}

// NewManager loads the sessions kept in stateDir.
func NewManager(ctx context.Context, log *slog.Logger, stateDir, claudeBin string) (*Manager, error) {
	if err := os.MkdirAll(filepath.Join(stateDir, "events"), 0o700); err != nil {
		return nil, err
	}
	m := &Manager{log: log, dir: stateDir, claude: claudeBin, IdleStop: 30 * time.Minute,
		ctx: ctx, sessions: map[string]*Session{}}
	b, err := os.ReadFile(m.registryPath())
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		var recs []record
		if err := json.Unmarshal(b, &recs); err != nil {
			return nil, fmt.Errorf("%s: %w", m.registryPath(), err)
		}
		for _, r := range recs {
			s := m.newSession(r.Name, r.Dir)
			s.claudeID = r.ClaudeID
			s.autoApprove = r.AutoApprove
			s.loadEvents()
			m.sessions[r.Name] = s
		}
	}
	go m.reap()
	return m, nil
}

func (m *Manager) registryPath() string { return filepath.Join(m.dir, "sessions.json") }

func (m *Manager) newSession(name, dir string) *Session {
	return &Session{name: name, dir: dir, m: m, subs: map[chan Event]struct{}{},
		pending: map[string]json.RawMessage{}, state: Idle}
}

// save writes the registry. Called with m.mu held.
func (m *Manager) save() error {
	recs := make([]record, 0, len(m.sessions))
	for _, s := range m.sessions {
		s.mu.Lock()
		recs = append(recs, record{Name: s.name, Dir: s.dir, ClaudeID: s.claudeID, AutoApprove: s.autoApprove})
		s.mu.Unlock()
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	b, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.registryPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.registryPath())
}

// Create adds a session for a directory.
func (m *Manager) Create(name, dir string) (Info, error) {
	if !validName.MatchString(name) {
		return Info{}, fmt.Errorf("a session name is letters, digits, dot, dash and underscore: %q", name)
	}
	// "~" is this machine's home: a phone cannot know where that is, and
	// shrooms-agent runs as the user whose home it is.
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Info{}, err
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Info{}, err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return Info{}, fmt.Errorf("%s is not a directory on this machine", abs)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[name]; ok {
		return Info{}, fmt.Errorf("there is already a session called %q", name)
	}
	s := m.newSession(name, abs)
	m.sessions[name] = s
	if err := m.save(); err != nil {
		delete(m.sessions, name)
		return Info{}, err
	}
	return s.Info(), nil
}

// Get returns a session by name.
func (m *Manager) Get(name string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[name]
	return s, ok
}

// List returns every session, by name.
func (m *Manager) List() []Info {
	m.mu.Lock()
	ss := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		ss = append(ss, s)
	}
	m.mu.Unlock()
	out := make([]Info, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Remove stops a session and forgets it. Its event log is deleted; the
// Claude Code transcript stays where Claude Code keeps it.
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	s, ok := m.sessions[name]
	if ok {
		delete(m.sessions, name)
	}
	err := m.save()
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no session called %q", name)
	}
	s.stop()
	os.Remove(s.eventsPath())
	return err
}

// reap stops processes that have been idle longer than IdleStop.
func (m *Manager) reap() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case now := <-t.C:
			m.mu.Lock()
			ss := make([]*Session, 0, len(m.sessions))
			for _, s := range m.sessions {
				ss = append(ss, s)
			}
			m.mu.Unlock()
			for _, s := range ss {
				s.stopIfIdle(now, m.IdleStop)
			}
		}
	}
}

func (s *Session) eventsPath() string {
	return filepath.Join(s.m.dir, "events", s.name+".jsonl")
}

// Info reports the session for the list.
func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	in := Info{Name: s.name, Dir: s.dir, State: s.state, Pending: len(s.pending),
		Running: s.proc != nil, LastSeq: s.seq, AutoApprove: s.autoApprove,
		ContextUsed: s.ctxUsed, ContextWindow: s.ctxWindow, Preview: s.preview}
	if n := len(s.events); n > 0 {
		in.LastTime = s.events[n-1].Time
	}
	return in
}

func (s *Session) loadEvents() {
	f, err := os.Open(s.eventsPath())
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		s.seq = e.Seq
		if e.Kind == "claude" {
			s.observe(e.Data)
		}
		s.events = append(s.events, e)
		if len(s.events) > memoryEvents {
			s.events = s.events[len(s.events)-memoryEvents:]
		}
	}
}

// record appends an event, writes it to disk, and hands it to every listener.
// Called with s.mu held.
func (s *Session) record(kind, by string, data any) Event {
	var raw json.RawMessage
	switch d := data.(type) {
	case nil:
	case json.RawMessage:
		raw = d
	default:
		raw, _ = json.Marshal(d)
	}
	s.seq++
	e := Event{Seq: s.seq, Time: time.Now(), Kind: kind, By: by, Data: raw}
	s.events = append(s.events, e)
	if len(s.events) > memoryEvents {
		s.events = s.events[len(s.events)-memoryEvents:]
	}
	if f, err := os.OpenFile(s.eventsPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		b, _ := json.Marshal(e)
		f.Write(append(b, '\n'))
		f.Close()
	} else {
		s.m.log.Warn("could not keep an event", "session", s.name, "err", err)
	}
	s.broadcast(e)
	return e
}

// broadcast hands an event to every listener. Called with s.mu held.
func (s *Session) broadcast(e Event) {
	for ch := range s.subs {
		select {
		case ch <- e:
		default:
			// A listener that cannot keep up is dropped rather than allowed
			// to stall the session; it reconnects and catches up by number.
			delete(s.subs, ch)
			close(ch)
		}
	}
}

// observe keeps what the session list shows about a Claude Code message: how
// full the context is, and the start of the last reply. Called with s.mu held,
// for live messages and for those loaded from disk alike.
func (s *Session) observe(raw json.RawMessage) {
	var m struct {
		Type       string  `json:"type"`
		ParentTool *string `json:"parent_tool_use_id"`
		Message    struct {
			Content []struct{ Type, Text string } `json:"content"`
			Usage   struct {
				Input       uint64 `json:"input_tokens"`
				CacheRead   uint64 `json:"cache_read_input_tokens"`
				CacheCreate uint64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		ModelUsage map[string]struct {
			ContextWindow uint64 `json:"contextWindow"`
		} `json:"modelUsage"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	switch m.Type {
	case "assistant":
		// A subagent's messages carry the tool use they belong to; their
		// usage is the subagent's context, not this conversation's.
		if m.ParentTool != nil && *m.ParentTool != "" {
			return
		}
		if u := m.Message.Usage; u.Input+u.CacheRead+u.CacheCreate > 0 {
			s.ctxUsed = u.Input + u.CacheRead + u.CacheCreate
		}
		var b strings.Builder
		for _, c := range m.Message.Content {
			if c.Type == "text" {
				b.WriteString(c.Text)
			}
		}
		if t := strings.TrimSpace(b.String()); t != "" {
			if r := []rune(t); len(r) > 200 {
				t = string(r[:200]) + "…"
			}
			s.preview = t
		}
	case "result":
		// The main model has the largest window; a helper model used for a
		// quick task is listed too, with a smaller one.
		for _, u := range m.ModelUsage {
			if u.ContextWindow > s.ctxWindow {
				s.ctxWindow = u.ContextWindow
			}
		}
	}
}

// Since returns the events after seq, and a channel for those that follow.
// The channel is closed when the listener falls behind or Unsubscribe is
// called; either way, asking again from the last seq seen loses nothing.
func (s *Session) Since(seq uint64) ([]Event, chan Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Event
	if len(s.events) > 0 && s.events[0].Seq > seq+1 {
		out = s.fromDisk(seq, s.events[0].Seq)
	}
	for _, e := range s.events {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	ch := make(chan Event, 256)
	s.subs[ch] = struct{}{}
	return out, ch
}

// Unsubscribe stops sending to a listener.
func (s *Session) Unsubscribe(ch chan Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[ch]; ok {
		delete(s.subs, ch)
		close(ch)
	}
}

// fromDisk reads the events with after < seq < before.
func (s *Session) fromDisk(after, before uint64) []Event {
	f, err := os.Open(s.eventsPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Seq > after && e.Seq < before {
			out = append(out, e)
		}
	}
	return out
}

// ensureRunning starts the process if there is none. Called with s.mu held.
func (s *Session) ensureRunning() error {
	if s.proc != nil {
		return nil
	}
	p, err := startProc(s.m.ctx, s.m.log.With("session", s.name), s.m.claude, s.dir, s.claudeID, s.autoApprove)
	if err != nil {
		return err
	}
	s.proc = p
	go s.read(p)
	return nil
}

// Send is a user turn from a device.
func (s *Session) Send(text, by string) error {
	if text == "" {
		return errors.New("an empty message")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRunning(); err != nil {
		return err
	}
	msg := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}}
	if err := s.proc.write(msg); err != nil {
		return err
	}
	s.record("message", by, map[string]string{"text": text})
	s.state = Working
	s.lastUsed = time.Now()
	return nil
}

// Answer answers a permission prompt. Allowing runs the tool with the input
// it asked for; denying tells the model why, so it can do something else.
func (s *Session) Answer(prompt string, allow bool, message, by string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.answer(prompt, allow, message, by)
}

// answer is Answer with s.mu held.
func (s *Session) answer(prompt string, allow bool, message, by string) error {
	req, ok := s.pending[prompt]
	if !ok {
		return fmt.Errorf("no prompt %q is waiting — it may have been answered already", prompt)
	}
	var r struct {
		Input json.RawMessage `json:"input"`
	}
	json.Unmarshal(req, &r)
	resp := map[string]any{"behavior": "deny", "message": message}
	if allow {
		input := r.Input
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		resp = map[string]any{"behavior": "allow", "updatedInput": input}
	} else if message == "" {
		resp["message"] = "The user declined this from their phone."
	}
	if s.proc == nil {
		return errors.New("the session's process has stopped; the prompt can no longer be answered")
	}
	if err := s.proc.write(map[string]any{
		"type":     "control_response",
		"response": map[string]any{"subtype": "success", "request_id": prompt, "response": resp},
	}); err != nil {
		return err
	}
	delete(s.pending, prompt)
	s.record("answer", by, map[string]any{"prompt": prompt, "allow": allow, "message": message})
	if len(s.pending) == 0 {
		s.state = Working
	}
	s.lastUsed = time.Now()
	return nil
}

// interrupts numbers interrupt requests, which need ids of their own. Not the
// event counter: a gap there would read to a phone as a lost event.
var interrupts atomic.Uint64

// Interrupt stops the turn in progress.
func (s *Session) Interrupt(by string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == nil {
		return errors.New("nothing is running")
	}
	return s.proc.write(map[string]any{
		"type":       "control_request",
		"request_id": fmt.Sprintf("interrupt-%d", interrupts.Add(1)),
		"request":    map[string]string{"subtype": "interrupt"},
	})
}

// read follows the process's output until it ends.
func (s *Session) read(p *proc) {
	for raw := range p.out {
		var head struct {
			Type      string `json:"type"`
			Subtype   string `json:"subtype"`
			SessionID string `json:"session_id"`
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype string `json:"subtype"`
			} `json:"request"`
		}
		json.Unmarshal(raw, &head)

		if head.Type == "stream_event" {
			s.partial(raw)
			continue
		}

		s.mu.Lock()
		s.observe(raw)
		autoAnswer := ""
		switch {
		case head.Type == "system" && head.Subtype == "init" && head.SessionID != "" && head.SessionID != s.claudeID:
			// The id to resume by. Saved at once: a crash before the
			// first turn ends would otherwise lose the conversation.
			s.claudeID = head.SessionID
			s.mu.Unlock()
			s.m.mu.Lock()
			if err := s.m.save(); err != nil {
				s.m.log.Warn("could not save the session list", "err", err)
			}
			s.m.mu.Unlock()
			s.mu.Lock()
		case head.Type == "control_request" && head.Request.Subtype == "can_use_tool":
			var full struct {
				Request json.RawMessage `json:"request"`
			}
			json.Unmarshal(raw, &full)
			s.pending[head.RequestID] = full.Request
			s.state = Waiting
			if s.autoApprove {
				autoAnswer = head.RequestID
			}
		case head.Type == "result":
			s.state = Idle
		}
		if s.state != Idle {
			s.lastUsed = time.Now()
		}
		s.record("claude", "", raw)
		if autoAnswer != "" {
			// Switched on after this process started, so it still asks.
			if err := s.answer(autoAnswer, true, "", "auto-approve"); err != nil {
				s.m.log.Warn("could not auto-approve", "session", s.name, "err", err)
			}
		}
		s.mu.Unlock()
	}
	<-p.done

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == p {
		s.proc = nil
	}
	reason := "finished"
	if p.err != nil {
		reason = p.err.Error()
	}
	// A prompt cannot be answered by a process that has gone; leaving it
	// listed would invite an answer that goes nowhere.
	for id := range s.pending {
		delete(s.pending, id)
	}
	s.state = Idle
	s.record("stopped", "", map[string]string{"reason": reason})
}

// partial passes reply text on to listeners as it is written. Only text: the
// model's thinking and tool input arrive whole in the message that follows.
func (s *Session) partial(raw json.RawMessage) {
	var m struct {
		Event struct {
			Type  string                      `json:"type"`
			Delta struct{ Type, Text string } `json:"delta"`
		} `json:"event"`
		ParentTool *string `json:"parent_tool_use_id"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Event.Type != "content_block_delta" ||
		m.Event.Delta.Type != "text_delta" || m.Event.Delta.Text == "" ||
		(m.ParentTool != nil && *m.ParentTool != "") {
		return
	}
	data, _ := json.Marshal(map[string]string{"text": m.Event.Delta.Text})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcast(Event{Seq: s.seq, Time: time.Now(), Kind: "partial", Data: data})
}

// SetAutoApprove switches approvals off for this session — the desktop's
// --dangerously-skip-permissions — or back on. Prompts already waiting are
// allowed at once; a running process keeps going, and the session answers
// whatever it still asks (see read).
func (s *Session) SetAutoApprove(on bool, by string) error {
	s.mu.Lock()
	s.autoApprove = on
	if on {
		for id := range s.pending {
			if err := s.answer(id, true, "", "auto-approve"); err != nil {
				s.m.log.Warn("could not auto-approve", "session", s.name, "err", err)
			}
		}
	}
	s.record("setting", by, map[string]bool{"auto_approve": on})
	s.mu.Unlock()
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.m.save()
}

// stopIfIdle ends the process if nothing has happened for longer than after.
// A turn in progress or a prompt waiting is not idle, however long it takes.
func (s *Session) stopIfIdle(now time.Time, after time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc != nil && s.state == Idle && now.Sub(s.lastUsed) >= after {
		s.proc.close()
	}
}

// stop ends the process now.
func (s *Session) stop() {
	s.mu.Lock()
	p := s.proc
	s.mu.Unlock()
	if p != nil {
		p.close()
		if p.cmd.Process != nil {
			select {
			case <-p.done:
			case <-time.After(10 * time.Second):
				p.cmd.Process.Kill()
			}
		}
	}
}
