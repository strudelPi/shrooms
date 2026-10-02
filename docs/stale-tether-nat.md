# A phone's tethering NAT can strand a WireGuard port

**Status:** cause confirmed 2026-10-02; fix needs a decision (below).

## What happens

A laptop tethered through an Android phone — over Wi-Fi hotspot on
2026-10-01, over USB on 2026-10-02 — loses every tunnel after the tethering
link changes underneath it (sleep, re-plug, the link renewing with a new
interface and address). Delivery (TCP) keeps working, so peers still show
`online`; every tunnel goes `stale` and stays that way.

## Evidence

- The VPS receives the laptop's handshake initiations and answers every one,
  to the exact public address and port they came from (raw-socket capture on
  the VPS: `IN … 148`, `OUT … 92`, four of each per mesh).
- The laptop never receives the answers (`tcpdump` on the tether interface:
  outgoing only).
- The phone's own tunnels, through the same carrier address to the same VPS
  ports, work throughout.
- Fresh UDP flows from the laptop work: any local port, any VPS port
  including 5182x, WireGuard-shaped payloads, and a 90-second flow at one
  packet a second (90/90).
- **Not fixed by:** restarting the daemon (same ports), 3½ minutes of silence
  on those ports, the phone's own shrooms being fully down, reconnecting to
  the hotspot.
- **Fixed by:** a new tethering link (sometimes), or **moving the meshes to
  ports the phone had not seen** — every tunnel up within a minute
  (2026-10-02 08:20, `port = "51832"` / `"51833"`).

So the phone holds a stale translation for the laptop's previous WireGuard
ports — most likely tied to the address the laptop had before the link
renewed — and keeps it however long the ports go quiet. Wi-Fi offload and
long-flow theories were tested and are wrong.

## The fix, and the decision it needs

### What was agreed first, and why it does not work

Agreed 2026-10-02: on every network change, move the ports shrooms chose
itself; leave pinned ones alone. Two things sink it:

- **"Pinned" does not mean "chosen by a person".** Creating or joining a mesh
  writes its port into the config (`cmd/shrooms/setup.go`, `joinmore.go`), so a
  later rename cannot reshuffle ports. Every config made since then pins every
  port; the rule would cover almost nothing, and would treat ports nobody
  forwards as if somebody did.
- **A session-only move does not survive a restart.** On 2026-10-02 07:38 the
  daemon restarted itself after a network change and came back on its
  configured ports — the very ones the phone had stranded.

### Proposed instead: decide by role

The ports that must not move are the ones other machines are told to reach:
a Core node or relay, or anything with `advertise` set (a static public
endpoint, usually a port-forward). An Edge node is reached by its own outgoing
traffic and by whatever it learns and announces, so its port is nobody's
contract.

- **Edge nodes without `advertise`:** the WireGuard port is ephemeral — a free
  port at every start, and a new one on every network change. The configured
  port stays in the config (it is harmless, and keeps the interface naming
  stable) but is not what such a node binds.
- **Core nodes, relays, and anything with `advertise`:** never move.
- **Phones:** Edge, so the same rule — a phone hops networks more than anything.

Things to verify while building, since code compares against our own port:
same-host neighbour detection (`bootstrapFrom`), the "allow inbound UDP <port>"
hint, port mappings (PCP/UPnP are already re-requested on a network change),
and the relay registration.

Peers lose nothing they had: after a network change our address changed
anyway, so cached endpoints for us were already useless; tunnels we start are
answered wherever our packets come from.
