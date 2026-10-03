# Agents on the mesh

**Status (2026-10-03):** in daily use from a phone and from Basecamp, on the
day it was built. **Shrooms Agents** is its own app and its own Basecamp
module, beside shrooms (decided 2026-10-03, below).

Talk to the Claude Code sessions on every machine you own, from the phone,
over the mesh — and approve what they want to do from there. It replaces `cl`
(tmux sessions reached over SSH), whose phone interface is a terminal.

The point is privacy and independence: nothing leaves the mesh. Claude Code's
own Remote Control does much of this through a cloud relay; this does not.

## Shape

```
Shrooms Agents (Android)  ─┐
Shrooms Agents (Basecamp) ─┼─ HTTP over the mesh ──▶  shrooms-agent (each machine)
                           │                              │ stream-json, one process per session
                           │                              ▼
                           │                           claude -p
shrooms (VPN, the mesh) ───┘  the network both run over
```

- **`shrooms-agent`** is a separate binary, not part of the daemon. Shrooms is
  the network; something that runs code on request does not belong in its
  most privileged process. It runs as the user (a systemd user unit, started
  through a login shell so sessions find what a terminal finds).
- **Shrooms Agents is a separate app, and a separate Basecamp module**
  (decided 2026-10-03). Agents grow out of the mesh and are not the mesh: the
  two change at very different speeds — the network should be boring, the
  agents UI changed a dozen times on its first day — and an app that records
  audio and drives machines that run code is a bigger target bundled into a
  VPN. The dependency runs one way: agents need the mesh for addresses, access
  and discovery; the mesh does not need agents. Its mark is the mushrooms
  alone (assets/agents_logo.py), shrooms' is the mycelium.
  - **Android:** the "agents" build of the same code
    (xyz.vpavlin.shrooms.agents), signed with its own key, kept outside the
    repository (~/apk-signing/shrooms-agents) with its password beside it. It
    is no mesh client: the shrooms app's "agents" link opens it and hands over
    the peers it can reach, and an agent lists the mesh as its machine sees it
    (/v1/peers), so knowing one finds the rest.
  - **Basecamp:** `shrooms_agents`, a view of its own that shares
    `shrooms_core` — which does all the networking, since Basecamp's sandbox
    forbids it in a view, on threads of its own so no call freezes the window.
- One repository, one set of tools.

## Access: the bind is the access control

`shrooms-agent` listens on this device's **overlay address** on each mesh it
serves, on a fixed port (**7387**), and nowhere else. Per ADR-026, only mesh
members can route to that address: the WireGuard tunnel admits configured
members only. So:

- reachable by members of those meshes, not by the LAN or the internet;
- the caller's source address *is* its device key's overlay address, so every
  request is attributed to a named device with no logins and no tokens;
- the port appears in peers' view through `announce_bound`, which is how the
  phone discovers it.

**Decision (2026-10-03): who may talk to an agent = the members of the meshes
listed in its config.** Every device on the meshes is the owner's today; the
list is the rail for the day a mesh is shared. To change later: a per-device
allowlist by key.

Another local user on the same machine can also connect to the overlay
address. That machine is already theirs to run code on, so it is not a new
exposure.

## Talking to Claude Code

Each session is a `claude -p` process run with `--input-format stream-json
--output-format stream-json --verbose --permission-prompt-tool stdio`, in the
session's directory. Observed on 2.1.287:

- user turns go in on stdin as `{"type":"user","message":{...}}`;
- assistant text, tool use and tool results come out as `assistant` / `user`
  messages, a turn ends with `result`;
- **a permission prompt arrives as a `control_request` with subtype
  `can_use_tool`** (tool, input, description, suggested "always allow" rules)
  and is answered with a `control_response` — `allow` (with the input) or
  `deny` (with a reason). Without `--permission-prompt-tool stdio`, prompts are
  denied automatically;
- `AskUserQuestion` reaches the host the same way, so multiple-choice
  questions can be answered from the phone too;
- every session has an id, and `--resume <id>` continues it after the process
  has gone.

The user's own Claude Code settings apply (permission rules, model, hooks), so
a session behaves as it would in a terminal.

A process lives while the session is in use and is stopped after it has been
idle for a while; the next message resumes it by id. A session is a name and a
directory — the same thing `cl` keys on.

## API (HTTP + JSON, server-sent events)

On each machine's overlay addresses, port 7387.

| | |
|---|---|
| `GET /v1/sessions` | list: name, directory, state (idle / working / waiting), pending prompts, context used and window, model, the last reply, auto-approve |
| `POST /v1/sessions` | `{name, dir, auto_approve?}` — create; `{name, resume: id}` — continue an existing conversation, in the directory it ran in |
| `DELETE /v1/sessions/{name}` | stop and forget |
| `PATCH /v1/sessions/{name}`, `POST …/settings` | `{auto_approve}` (POST for clients that cannot send PATCH) |
| `GET /v1/sessions/{name}/events?after=N[&tail=T]` | the session's events, then a live stream (SSE); `partial` events carry reply text as it is written, unnumbered and never kept. `tail=T` with `after=0` starts at the last T events instead of the first: both apps open a session at its last 300 and offer to load the rest |
| `GET /v1/sessions/{name}/history?limit=N` | what was said before this agent had the conversation, from Claude Code's transcript (its last 4 MB) |
| `POST /v1/sessions/{name}/messages` | `{text}` — a user turn |
| `POST /v1/sessions/{name}/prompts/{id}` | `{allow, message?}` — answer a permission prompt |
| `POST /v1/sessions/{name}/interrupt` | stop the current turn |
| `POST /v1/sessions/{name}/files?name=` | the bytes of a file (50 MB at most); kept under the agent's own directory; returns `{path}` for the next message to name |
| `POST /v1/sessions/{name}/transcribe?name=&lang=` | a voice note, kept like a file and transcribed by whisper.cpp; returns `{path, text}`. Naming the language halves the time |
| `GET /v1/conversations?limit=N` | this machine's Claude Code conversations, newest first: where each ran, its last exchange, the session continuing it, and any terminal `claude` open in the same directory (with its tmux session) |
| `POST /v1/terminals/{pid}/stop` | end a terminal's `claude` so its conversation can be continued here; only Claude Code run by this user by hand |
| `GET /v1/peers` | the mesh as this machine sees it, so a client that knows one agent finds the rest |

Events are numbered per session and kept on disk, so a phone that was away
catches up from the last number it saw.

### Taking over a terminal's conversation

What replaces `cl`. Resuming a conversation keeps writing to the same
transcript, so a session that continues one started in a terminal carries it
on rather than copying it. A terminal `claude` does not keep its transcript
open, so which conversation it holds cannot be known — only that one is open
in the same directory, and which tmux session it is. The list says so, with
"stop it": two writers on one conversation cannot see each other's turns, and
the transcript branches.

## What it does

On both: every machine running shrooms-agent and its sessions (NEEDS YOU when a
prompt waits, context use, model, the last reply); a conversation with the
transcript's earlier history, streamed replies, markdown, permission prompts
with the command in full, auto-approve per session (the desktop's
--dangerously-skip-permissions), stop, new sessions, copy; files (📎, or dropped
on Basecamp) kept on the agent's machine and named by path in the next
message; voice notes, recorded on the device and transcribed on the agent's
machine by whisper.cpp, so no audio reaches a speech service. The phone
notifies when a session needs you or replied.

A conversation opens at its last 300 events, with a link to load the rest: a
long session holds thousands, and replaying them all made the phone scroll for
ten seconds before settling at the end. The phone also applies arriving events
in batches every 120 ms rather than one by one. In Basecamp, the core answers
the view in pieces of about half a megabyte and the view reads on until caught
up: a session's whole backlog in one reply through Basecamp's IPC is the likely
reason some conversations showed empty after switching to them (not proven).

## Open

- Publishing: the LAN F-Droid and Basecamp repositories live on jimmy-crib,
  which was down when this was built; the agents packages go there with the
  next shrooms release.
- Share to Shrooms Agents from any Android app; several files at once.
  (Pasting an image into Basecamp is built; it needs wl-clipboard or xclip.)
