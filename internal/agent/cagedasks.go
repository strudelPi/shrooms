package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Tasks from caged agents the owner has not decided on (ADR-070).
//
// accept_caged was all or nothing: a session took tasks from every caged
// agent or from none. The publisher has to take them from the one cage that
// builds an app and from no other, so a caged sender the session does not
// already accept is held, not refused: its task waits in A2A's own
// auth-required state, the apps show it as a request, and the owner allows the
// sender (a standing rule, as for files) or declines. Allowing releases every
// task held from that sender; declining rejects them, and the asker is told.

// taskAuthRequired is A2A's state for a task waiting on someone's
// permission: here, the owner's.
const taskAuthRequired = "TASK_STATE_AUTH_REQUIRED"

// TaskAsk is a task held for the owner's decision, as the apps show it.
type TaskAsk struct {
	Task  string    `json:"task"`
	From  string    `json:"from"` // MACHINE/SESSION, as the sender's own agent says it
	Title string    `json:"title,omitempty"`
	At    time.Time `json:"at"`
}

// allowsCaged is whether the session takes tasks from a caged sender,
// "machine/session": all of them (accept_caged), or this one by name or by
// its machine with "/*". Caller holds s.mu.
func (s *Session) allowsCaged(sender string) bool {
	if s.acceptsCaged() {
		return true
	}
	machine, _, _ := strings.Cut(sender, "/")
	for _, a := range s.acceptCagedFrom {
		if a == sender || a == machine+"/*" {
			return true
		}
	}
	return false
}

// AllowsCaged is allowsCaged, for a caller without the lock.
func (s *Session) AllowsCaged(sender string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.allowsCaged(sender)
}

// SetAcceptCagedFrom replaces the caged agents the session takes tasks from,
// and releases the tasks held from those now allowed.
func (s *Session) SetAcceptCagedFrom(list []string, by string) error {
	clean, err := senderList(list)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.acceptCagedFrom = clean
	s.record("setting", by, map[string][]string{"accept_caged_from": clean})
	s.mu.Unlock()
	s.m.mu.Lock()
	err = s.m.save()
	s.m.mu.Unlock()
	s.m.releaseCaged(s)
	return err
}

// senderList checks a list of MACHINE/SESSION or MACHINE/* entries.
func senderList(list []string) ([]string, error) {
	clean := []string{}
	for _, a := range list {
		a = strings.TrimSpace(a)
		machine, session, ok := strings.Cut(a, "/")
		if !ok || machine == "" || session == "" || strings.ContainsAny(a, " \t\n") {
			return nil, fmt.Errorf("%q: want MACHINE/SESSION or MACHINE/*", a)
		}
		clean = append(clean, a)
	}
	return clean, nil
}

// SubmitHeld records a task from a caged sender the session does not take
// tasks from yet: kept, not started, until the owner decides.
func (m *Manager) SubmitHeld(s *Session, messageID, from, sender, text, title string, refs []string) (Task, error) {
	id := s.Name() + ":" + messageID
	if t, ok := m.tasks.get(id); ok {
		return t, nil
	}
	now := time.Now()
	t := &Task{ID: id, Session: s.Name(), MessageID: messageID, From: from, Request: text, Title: title, Refs: refs,
		Created: now, Updated: now, State: taskAuthRequired, CagedFrom: sender,
		Summary: "held until the owner allows tasks from " + sender + " (a caged agent)"}
	m.tasks.mu.Lock()
	m.tasks.tasks[id] = t
	err := m.tasks.save()
	m.tasks.mu.Unlock()
	s.mu.Lock()
	s.record("task-request", sender, TaskAsk{Task: id, From: sender, Title: title, At: now})
	s.mu.Unlock()
	return *t, err
}

// taskAsks is the tasks held for the owner's decision on session, oldest first.
func (m *Manager) taskAsks(session string) []TaskAsk {
	var out []TaskAsk
	for _, t := range m.tasks.list(session) {
		if t.State == taskAuthRequired {
			out = append(out, TaskAsk{Task: t.ID, From: t.CagedFrom, Title: t.Title, At: t.Created})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// releaseCaged queues the held tasks whose sender the session now takes tasks
// from, and starts the next if it is free.
func (m *Manager) releaseCaged(s *Session) {
	released := 0
	for _, a := range m.taskAsks(s.Name()) {
		if !s.AllowsCaged(a.From) {
			continue
		}
		if _, err := m.tasks.change(a.Task, func(t *Task) error {
			if t.State != taskAuthRequired {
				return fmt.Errorf("task %s is %s", t.ID, stateWord(t.State))
			}
			t.State, t.Summary = taskSubmitted, ""
			return nil
		}); err == nil {
			released++
		}
	}
	if released > 0 {
		m.dispatch(s)
	}
}

// DeclineCaged rejects every task held from sender, and tells each asker. The
// sender is not remembered: its next task is a new request.
func (m *Manager) DeclineCaged(s *Session, sender, by string) int {
	n := 0
	for _, a := range m.taskAsks(s.Name()) {
		if a.From != sender {
			continue
		}
		t, err := m.tasks.change(a.Task, func(t *Task) error {
			if t.State != taskAuthRequired {
				return fmt.Errorf("task %s is %s", t.ID, stateWord(t.State))
			}
			t.State, t.Summary = taskRejected, "declined by "+ownerWord(by)+": "+s.Name()+" takes no tasks from "+sender
			return nil
		})
		if err != nil {
			continue
		}
		n++
		s.mu.Lock()
		s.record("task", by, map[string]string{"id": t.ID, "state": stateWord(t.State), "summary": t.Summary})
		s.mu.Unlock()
		go m.tellAsker(t)
	}
	return n
}

// ownerWord names who decided, for the asker.
func ownerWord(by string) string {
	if by == "" {
		return "the owner"
	}
	return "the owner (" + by + ")"
}
