# Shrooms on iOS: does the delivery node fit?

**Status:** measured 2026-10-01; not built. The earlier "no" rested on a library
we do not use.

## The constraint

On iOS the tunnel runs as a packet tunnel extension, and an extension gets
**50 MiB** — iOS 16 to 26, confirmed by Apple DTS in October 2025 (the common
"15 MB" figure is stale). The limit counts *dirty* memory (and compressed
memory), not clean pages such as a library's code mapped from its file, which
the system can drop and reload.

## Why we said it would not fit

The analysis was about **go-libp2p**: about 12 MB of resource-manager
reservations before a single peer, and QUIC's 15 MB per-connection receive
window. It concluded: libp2p in the containing app, WireGuard in the
extension — which on iOS means no rendezvous while the app is suspended.

Shrooms does not use go-libp2p. The delivery plane is **liblogosdelivery**,
written in Nim on nim-libp2p, so the old numbers say nothing about it.

## What an Edge node actually costs

One delivery node in Edge mode — the phone's configuration — run alone in a
Go test process on amd64, against the `logos.test` fleet, sampled every five
minutes. The test process alone is about 3.8 MB dirty, included below.

| dirty (anon) memory | discovery on (as shipped) | discovery off |
|---|---|---|
| created | 12.7 MB | 12.6 MB |
| 5 min | 16.5 MB | 16.1 MB |
| 10 min | 17.2 MB | 16.1 MB |
| 25 min | 17.8 MB | 16.2 MB |
| trend at the end | +0.1 MB / 5 min, slowing | flat |

So the node itself costs **about 12 MB** with discovery off, and holds there.
`discv5Discovery = false` and `rendezvous = false` are accepted at our pinned
revision and the node starts and runs without them: a phone finds its service
node through entry points (the public fleet, plus our own Core nodes per
ADR-031), so it does not need discovery.

Resident memory is higher — about 45 MB — but most of the difference is the
library's code mapped from the file, which iOS does not count.

## What this does not settle

- **The rest of shrooms.** The laptop daemon, also an Edge node, holds about
  130 MB dirty after hours with three meshes. The node is about 12 of that.
  The rest — WireGuard buffers, three meshes' state, logs — is unaccounted for,
  and is the real question for an extension. WireGuard's own iOS app runs in
  the same 50 MiB, so WireGuard alone fits; ours has not been measured.
- **The fleet was degraded.** Five of six `logos.test` nodes were down during
  these runs (the v0.39 rollout), so the node had fewer peers than usual.
- **Which library.** Measured with the desktop build (the July pin), not an
  iOS one. Upstream says v0.39 "restored" Android and iOS builds, but v0.39
  also rewrote the C interface our bindings use.
- **Measured on Linux.** The final check has to be an iPhone's own footprint.

## Next measurements, in order

1. The Android app's real footprint (`dumpsys meminfo` on a phone running it),
   which is the closest thing to the extension we can run today.
2. Where the laptop daemon's 130 MB goes, by Go heap profile and the library's
   share, so the unaccounted part has a name.
3. A lean phone config (discovery off) shipped and measured on Android.
