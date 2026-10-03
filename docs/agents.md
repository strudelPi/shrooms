# Agents on the mesh

**Status:** stage 1 (the server, `shrooms-agent`) built 2026-10-03 and run
end to end: a session on the laptop, driven from the VPS over the office mesh,
waited on a real Claude Code permission prompt, was approved remotely, and
finished — every action attributed to `vps.office`. Stage 2 (the Android
"Agents" screen) built the same day; not yet run on a phone.

Talk to the Claude Code sessions on every machine you own, from the phone,
over the mesh — and approve what they want to do from there. It replaces `cl`
(tmux sessions reached over SSH), whose phone interface is a terminal.

The point is privacy and independence: nothing leaves the mesh. Claude Code's
own Remote Control does much of this through a cloud relay; this does not.

## Shape

```
phone (shrooms app, "Agents")  ── HTTP over the mesh ──▶  shrooms-agent (each machine)
                                                              │ stream-json, one process per session
                                                              ▼
                                                           claude -p
```

- **`shrooms-agent`** is a separate binary, not part of the daemon. Shrooms is
  the network; something that runs code on request does not belong in its
  most privileged process.
- **The mobile app is a screen in the shrooms Android app**, not a second app.
  The shrooms app already knows the peers and what they announce, so it finds
  every agent with nothing configured. No web UI (decided 2026-10-03).

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

| | |
|---|---|
| `GET /v1/sessions` | list: name, directory, state (idle / working / waiting), pending prompts |
| `POST /v1/sessions` | `{name, dir}` — create |
| `DELETE /v1/sessions/{name}` | stop and forget |
| `GET /v1/sessions/{name}/events?after=N` | the session's events, then a live stream (SSE) |
| `POST /v1/sessions/{name}/messages` | `{text}` — a user turn |
| `POST /v1/sessions/{name}/prompts/{id}` | `{allow, message?}` — answer a permission prompt |
| `POST /v1/sessions/{name}/interrupt` | stop the current turn |

Events are numbered per session and kept on disk, so a phone that was away
catches up from the last number it saw.

## Stages

1. **Server** — `shrooms-agent`: sessions, the Claude Code process protocol,
   permission prompts, the API, binding to overlay addresses. Tested against a
   fake `claude` that speaks the protocol, plus one real run.
2. **Android** — an "Agents" screen: agents found from announced ports, a
   session list, a conversation view, approve / deny.
3. **Background** — notify the phone when a session is waiting for approval or
   has finished, without the app open.

## Open

- Notifications without the app open (stage 3): a long-lived connection from
  the shrooms VPN service is the obvious place, since it is already running.
- Basecamp: the desktop module and the Android app are meant to match; an
  Agents view there would follow stage 2.
