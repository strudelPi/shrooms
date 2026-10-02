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

Detect it, then move: when a mesh has had **no completed handshake with any
peer** for a few minutes after a network change, while delivery works and
handshakes are being sent, rebind that mesh's WireGuard socket to a new port
and announce it. Peers follow the announce, as they do after any endpoint
change.

What the detection must not do is fire on its own failures (a mesh whose peers
are all genuinely offline also has no handshakes), so it counts only peers
that are announcing — `online` but `stale`.

**Decision: what about a port that is pinned in the config?** A pinned port
may have a router port-forward behind it (Core nodes, relays), and moving it
would quietly break inbound reachability while fixing outbound.

1. **Move only ports shrooms chose itself**; leave pinned ones and report the
   problem in `shrooms status`. Safe, but on the laptop as configured right now
   (pinned during the test) it would never fire.
2. **Move any port, for this session only**, and return to the pinned one at
   the next start. Fixes the laptop either way; a port-forwarded Core node
   could lose inbound until it restarts.
3. **Move unpinned ports; for pinned ones, move only on Edge nodes** (which
   rely on outbound anyway). Most specific, one more rule to explain.

Recommendation: **3**, and remove the two `port =` lines added during the test
so the laptop is back to ports shrooms chooses.
