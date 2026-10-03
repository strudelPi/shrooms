# 035. An Edge node's port is nobody's contract

**Status:** accepted, built 2026-10-02 — details in
[docs/stale-tether-nat.md](../stale-tether-nat.md)

## Context

A laptop tethered through an Android phone lost every tunnel after the tether
link renewed, twice: over the Wi-Fi hotspot on 2026-10-01 and over USB on
2026-10-02. Delivery kept working, so peers showed `online`; every tunnel went
`stale`. The VPS received every handshake and answered each one to the exact
address and port it came from; the laptop never saw an answer. Restarting on
the same ports did not help, nor did minutes of silence on them, nor did the
phone's own shrooms being down. Moving the meshes to ports the phone had never
seen fixed it within a minute. The phone keeps a stale translation for ports it
has seen, however long they go quiet.

The obvious rule — move the ports shrooms chose itself, leave pinned ones —
covers almost nothing: joining a mesh writes its port into the config, so that
a rename cannot reshuffle ports, and nearly every port is "pinned" without a
person having chosen it. And a move that lasts only until the next restart is
undone by the daemon's own restart after a network change, which came straight
back on the stranded ports.

## Decision

**Which ports may move is decided by role, not by the config.** The ports that
must stay are the ones other machines are told to reach: a relay's, an
advertised endpoint's (usually a port-forward), and — conservatively — any
delivery Core node's. Everything else is an Edge node's port, reached by its own
outgoing traffic and by whatever it announces, and is nobody's contract:

- bound to a free port at every start, never the configured one;
- rebound in place on every network change (`listen_port` through
  wireguard-go, so peers and sessions survive), what peers observed of the old
  port forgotten, and the new one announced at once.

Unset mode is Core, so a config that never chose stays put.

## Consequences

- A tethered laptop recovers on its own after the link renews.
- Edge nodes no longer have a stable WireGuard port. A hand-made port-forward
  to an Edge node without `advertise` breaks; the fix is to say `advertise`,
  which is also what tells peers about it.
- After a network change our address changed anyway, so peers lose nothing
  they had: what they cached for us was already useless.
- Phones are Edge and would benefit most, but are not covered yet: a rebound
  socket must be protected from the VPN again on Android.

## What would change our mind

A NAT that maps a fresh port worse than a known one — rate-limiting new
mappings, say — so that moving costs connectivity on networks that never
strand a port. None has been seen.
