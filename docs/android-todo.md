# Android, wanted next

Raised 2026-08-17, in the order I'd do them.

## 1. The app dies when the phone goes fully offline, and does not come back

Observed while testing BLE mesh with everything else switched off: the app
crashed and never auto-started. This is the one that matters — a VPN that needs
launching by hand after a bad network moment is a VPN you stop trusting.

Two halves, and they need separating before either is fixed: *why* it died (a
path that assumes some network exists), and *why nothing restarted it* (service
restart policy, battery optimisation, or the crash taking the VpnService down
with it). Reproduce with aeroplane mode rather than guessing.

## 2. The disconnect button sits where the gesture-navigation swipe lands

Pulling up from the bottom of the phone sometimes disconnects the mesh. A
destructive control directly under the system gesture area. Move it, inset it
above the navigation bar, or make it need a deliberate second action — the
first is probably enough.

## 3. Settings wording

- "Light node" should read "Edge node", matching the config value and the rest
  of the documentation.
- Drop "Tell peers what this device offers". A phone rarely publishes services,
  so the setting is a disclosure control for something that does not exist —
  and every setting nobody needs is one more thing to explain.

## 4. The widget should list meshes, with a toggle each

Today it is a single connect/disconnect. Wanted: one row per mesh with its own
switch, so a mesh can be dropped or brought back without opening the app.

Needs checking first: whether toggling a mesh from the widget is the same
operation the app already has (config write plus a restart of that instance),
and what that costs on a phone — a widget that silently restarts the tunnel is
worse than no widget.

## Older Android: `.mesh` names work in the browser and not in other apps

Seen 2026-09-29 on a Poco F1 running an old Android. The phone joined `office`
and `http://files.pi5.office.mesh` and `http://jimmy-crib.office.mesh:8099/…`
both loaded in the browser, while F-Droid, on the same phone and at the same
moment, failed on `jimmy-crib.office.mesh` with "unable to resolve host".
x6, a newer Android on the same APK and mesh, resolved it in F-Droid fine.

The browser brings its own DNS resolver, which reads the VPN's DNS server off
the network and asks it directly. Everything else goes through the system
resolver, and older Android has been inconsistent about using a split-tunnel
VPN's DNS server — ours is an IPv6 address inside the mesh prefix. So on those
versions, names work in a browser and nowhere else. Not confirmed with the DNS
counters (which would show whether F-Droid's queries reach us at all); inferred
from the comparison, which isolates the Android version.

Workaround: address by IP. The mesh address works wherever the tunnel is up,
e.g. `http://[fdb0:…]:8099/loam-fdroid/repo`. Worth revisiting only if older
Android matters, since "mobile is a full participant" is a stated goal.
