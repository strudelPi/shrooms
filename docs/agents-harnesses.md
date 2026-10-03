# Shrooms Agents: logging in, and harnesses other than Claude Code

**Status:** for discussion, 2026-10-03. Nothing here is built.

## 1. A machine whose Claude Code is not logged in

Seen on the VPS: a session answers every message with "Not logged in · Please
run /login", and `/login` sent from the app answers "/login isn't available in
this environment". The agent runs `claude -p`, which has no slash commands;
logging in is something done once on the machine, outside any session.

**Today, by hand** — either of:

- `ssh` in, `su - agent`, run `claude`, then `/login`: it prints a URL to open
  in a browser anywhere, and takes the code back. Stored in `~agent/.claude`,
  refreshed by Claude Code itself.
- `claude setup-token` (on the machine, or on any machine logged in to the
  same account): prints a long-lived OAuth token (a year) for headless use,
  which goes in `CLAUDE_CODE_OAUTH_TOKEN` in the agent's environment — for the
  VPS, a line in the user unit's drop-in. Does not refresh: it has to be
  replaced when it expires.

**What the apps could do:**

- **(a) Say so.** The agent recognises "Not logged in" in a turn's result and
  reports it on the session (`/v1/sessions` → `"logged_in": false`); the apps
  show a banner on that machine saying how to log it in, instead of letting the
  person type `/login` into a box that cannot take it. Small; no secrets move.
- **(b) Log in from the app.** The agent runs `claude setup-token` (or the
  interactive login under a pseudo-terminal), hands the URL to the app, which
  opens it in the phone's browser; the person pastes the code back; the agent
  feeds it in and keeps the token. Convenient, but the agent then handles an
  account credential and drives an interactive flow whose prompts are not a
  stable interface — the first Claude Code update that rewords them breaks it.

**Recommendation:** (a) now; (b) only if logging machines in turns out to be
frequent — for a handful of machines it is a once-a-year chore.

## 2. Harnesses other than Claude Code

What Shrooms Agents needs from a harness: start a conversation in a directory,
or resume one by id; send a turn; a stream of what happens (text as it is
written, whole messages, tool calls and their output, the end of a turn and
what it cost); permission prompts it can wait on and be answered; questions;
interrupt; and its transcripts on disk for history, search and taking over a
terminal's conversation.

All of that is Claude Code's stream-json today, in `internal/agent`
(proc.go starts it, session.go reads it, history.go and search.go read its
transcripts, conversations.go finds its terminals). The apps read Claude
Code's messages almost verbatim (AgentChat.kt, Main.qml `chatItems`).

### Options

- **(A) One adapter per harness, behind an interface in the agent.** A
  `Harness` interface — start/resume, send, interrupt, answer, history,
  conversations — with Claude Code as the first implementation, and the agent
  translating each harness's events into one event shape of its own that the
  apps render. Codex (`codex exec --json`, or its app-server protocol), Gemini
  CLI (stream-json), OpenCode (an HTTP server with SSE) would each be an
  adapter. Full control and each harness's best features; one adapter of work
  per harness, and the apps must move off Claude Code's message shapes first.

- **(B) ACP, the Agent Client Protocol.** Zed's JSON-RPC-over-stdio protocol
  for editors to drive coding agents, now community-governed
  (github.com/agentclientprotocol). Gemini CLI speaks it natively; Claude Code
  and Codex through adapters (Zed's); OpenCode, Goose and others list support.
  The agent would become an ACP *client* — speaking one protocol to any of
  them — and its sessions, prompts (ACP has permission requests) and streaming
  map onto ACP's. One integration for many harnesses; the cost is the lowest
  common denominator (does every agent expose context use, cost, model,
  questions, resume-by-id?) and, for Claude Code, an adapter between us and it
  that we do not control. Not yet verified hands-on: the state above is from
  the protocol's site and third-party write-ups, 2026-10-03.

- **(C) Both:** the interface of (A), with Claude Code kept native (it is the
  daily driver, and stream-json gives everything) and one *ACP* implementation
  covering every other harness.

### What moves regardless

- **The apps stop reading Claude Code's messages directly.** The agent
  normalises events (said, tool, output, prompt, question, done, stopped) and
  the apps render those. Needed for any option; also makes the apps simpler.
- **History, search and takeover** are per harness: each keeps its
  transcripts differently, if at all.
- **A session records its harness**, and "+ session" offers the ones the
  machine has.

**Recommendation:** (C) — normalise the events first, keep Claude Code
native, and add ACP for the rest, after a spike that drives Gemini CLI (native
ACP) and Codex (adapter) from a small Go client to see what they actually
expose. Decision wanted before any of it is built.
