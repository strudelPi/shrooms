package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Port is where shrooms-agent listens on each mesh address. Fixed, so a phone
// finds agents by looking for it among the ports peers announce (ADR-026).
const Port = 7387

// Who names the device a request came from, by its source address.
//
// On the mesh the source address is the overlay address, which is derived
// from the device's key — so this is attribution, not a claim the caller
// makes. Unknown addresses get "" and are still served: only members can
// reach the address this listens on at all (docs/agents.md).
type Who func(netip.Addr) string

// Handler serves the agent API.
func Handler(log *slog.Logger, m *Manager, who Who) http.Handler {
	h := &handler{log: log, m: m, who: who}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sessions", h.list)
	mux.HandleFunc("POST /v1/sessions", h.create)
	mux.HandleFunc("DELETE /v1/sessions/{name}", h.remove)
	mux.HandleFunc("PATCH /v1/sessions/{name}", h.update)
	// The same, for clients that cannot send PATCH (Android's HttpURLConnection).
	mux.HandleFunc("POST /v1/sessions/{name}/settings", h.update)
	mux.HandleFunc("GET /v1/sessions/{name}/history", h.history)
	mux.HandleFunc("POST /v1/sessions/{name}/files", h.upload)
	mux.HandleFunc("POST /v1/sessions/{name}/transcribe", h.transcribe)
	mux.HandleFunc("GET /v1/sessions/{name}/events", h.events)
	mux.HandleFunc("POST /v1/sessions/{name}/messages", h.message)
	mux.HandleFunc("POST /v1/sessions/{name}/prompts/{id}", h.answer)
	mux.HandleFunc("POST /v1/sessions/{name}/interrupt", h.interrupt)
	return mux
}

type handler struct {
	log *slog.Logger
	m   *Manager
	who Who
}

func (h *handler) caller(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	a, err := netip.ParseAddr(host)
	if err != nil || h.who == nil {
		return ""
	}
	return h.who(a.Unmap())
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (h *handler) session(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	s, ok := h.m.Get(r.PathValue("name"))
	if !ok {
		fail(w, http.StatusNotFound, fmt.Errorf("no session called %q", r.PathValue("name")))
	}
	return s, ok
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": h.m.List()})
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Dir         string `json:"dir"`
		AutoApprove *bool  `json:"auto_approve"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	in, err := h.m.Create(req.Name, req.Dir)
	if err == nil && req.AutoApprove != nil {
		if s, ok := h.m.Get(in.Name); ok {
			err = s.SetAutoApprove(*req.AutoApprove, h.caller(r))
			in = s.Info()
		}
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	h.log.Info("session created", "session", in.Name, "dir", in.Dir, "by", h.caller(r))
	writeJSON(w, http.StatusCreated, in)
}

func (h *handler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.m.Remove(r.PathValue("name")); err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	h.log.Info("session removed", "session", r.PathValue("name"), "by", h.caller(r))
	w.WriteHeader(http.StatusNoContent)
}

// update changes a session's settings: for now, whether it asks.
func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	var req struct {
		AutoApprove *bool `json:"auto_approve"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if req.AutoApprove != nil {
		if err := s.SetAutoApprove(*req.AutoApprove, h.caller(r)); err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		h.log.Info("auto-approve changed", "session", s.name, "on", *req.AutoApprove, "by", h.caller(r))
	}
	writeJSON(w, http.StatusOK, s.Info())
}

// MaxUpload bounds one uploaded file: photos, screenshots, logs and PDFs, not
// disk images.
const MaxUpload = 50 << 20

// upload keeps a file sent from a device on this machine, for the session to
// read: ?name=<original name>, the bytes as the body. Returns {"path"}, which
// the device puts in its next message — Claude Code reads images, PDFs and
// text from a path like any other file.
func (h *handler) upload(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	path, err := s.Upload(r.URL.Query().Get("name"), http.MaxBytesReader(w, r.Body, MaxUpload))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	h.log.Info("file received", "session", s.name, "path", path, "by", h.caller(r))
	writeJSON(w, http.StatusCreated, map[string]string{"path": path})
}

// transcribe keeps a voice note like any other file and returns its text,
// transcribed on this machine: ?name=<file name>&lang=<code or auto>. The
// phone puts the text in the message box, to be read and corrected first.
func (h *handler) transcribe(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	if h.m.STT == nil {
		fail(w, http.StatusNotImplemented, fmt.Errorf("no speech-to-text on this machine: see shrooms-agent --help (-stt-model)"))
		return
	}
	path, err := s.Upload(r.URL.Query().Get("name"), http.MaxBytesReader(w, r.Body, MaxUpload))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	start := time.Now()
	text, err := h.m.STT.Transcribe(r.Context(), path, r.URL.Query().Get("lang"))
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	h.log.Info("voice note transcribed", "session", s.name, "took", time.Since(start).Round(time.Millisecond),
		"words", len(strings.Fields(text)), "by", h.caller(r))
	writeJSON(w, http.StatusOK, map[string]string{"path": path, "text": text})
}

// history is the conversation before this agent's own events, from Claude
// Code's transcript: ?before=<RFC 3339>&limit=N (default 30, at most 200).
func (h *handler) history(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	limit := 30
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fail(w, http.StatusBadRequest, fmt.Errorf("limit: %q", v))
			return
		}
		limit = min(n, 200)
	}
	var before time.Time
	if v := r.URL.Query().Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			fail(w, http.StatusBadRequest, fmt.Errorf("before: %w", err))
			return
		}
		before = t
	}
	said, err := s.History(before, limit)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if said == nil {
		said = []Said{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": said})
}

func (h *handler) message(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	var req struct{ Text string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.Send(req.Text, h.caller(r)); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *handler) answer(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	var req struct {
		Allow   bool   `json:"allow"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.Answer(r.PathValue("id"), req.Allow, req.Message, h.caller(r)); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) interrupt(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := s.Interrupt(h.caller(r)); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// events is the session's history after ?after=N, then everything that
// follows, as server-sent events: one `data:` line of JSON per event, with
// the sequence number as its id. A comment is sent every 20 seconds so a
// phone on mobile data notices a dead connection.
func (h *handler) events(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	after := uint64(0)
	if v := r.URL.Query().Get("after"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			fail(w, http.StatusBadRequest, fmt.Errorf("after: %w", err))
			return
		}
		after = n
	}
	// The standard reconnect header wins over the query: it is what an SSE
	// client sends on its own after a drop.
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64); err == nil {
			after = n
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	backlog, ch := s.Since(after)
	defer s.Unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	send := func(e Event) error {
		b, _ := json.Marshal(e)
		_, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Seq, b)
		return err
	}
	last := after
	for _, e := range backlog {
		if err := send(e); err != nil {
			return
		}
		last = e.Seq
	}
	flusher.Flush()

	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-ch:
			if !open {
				// Fell behind: end the stream; the client reconnects with
				// Last-Event-ID and loses nothing.
				return
			}
			if e.Kind == "partial" {
				// Live only and unnumbered: no id, so a reconnect does not
				// resume from it.
				b, _ := json.Marshal(e)
				if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
					return
				}
				flusher.Flush()
				continue
			}
			if e.Seq <= last {
				continue // already in the backlog
			}
			if err := send(e); err != nil {
				return
			}
			last = e.Seq
			flusher.Flush()
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
