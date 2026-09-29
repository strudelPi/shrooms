# One kind of mesh

**Status:** decided 2026-09-29; stages 1 and 2 built, 3 and 4 open — see
*Decided* at the end. Vaclav's question, 2026-08-26: *"why do we even
have primary and secondary meshes? Do we need the distinction? I'd just name all
the meshes — we could even alias one as default, but we should treat them all
the same."*

Short answer: no, we do not need it, and it has been costing us. This is what
it would take.

## What the distinction actually is

A config written before ADR-015 describes one mesh in top-level fields —
`network_key`, `relay`, `listen_port`, `admin_keys`. Multi-mesh added
`[mesh.<label>]` tables beside them. Rather than migrate, the top-level form
stayed and became "the primary mesh", labelled `default`.

So a node has one mesh described in one shape and every other mesh described in
another, and every piece of code that touches meshes has to know which it is
holding. There are **87 sites** referencing `DefaultLabel`, `legacy` or
`original` outside tests.

## What it has cost

Not hypothetical — each of these was a real bug:

| symptom | cause |
|---|---|
| `shrooms paths` reports `we announce` for the primary mesh while listing peers from all of them | `out.Announced` is set from `primary`, and there is one field for a per-mesh fact |
| a credential written to the top level but not to the matching mesh entry, so a restart lost it | two homes for one value; fixed by `SetOwnCredential` writing both |
| `MeshState` identifying the original mesh by pointer comparison against `s.Identity` | replaced by an explicit `original` flag |
| `mesh remove` needing a special refusal for the top-level mesh | it is the device's whole config, not an entry |
| `ifaceAndPort` special-casing "the top-level form is always position zero" | interface assignment depends on which shape a mesh is in |
| `admin.json` vs `admin-<label>.json` | two naming schemes for one kind of file |

The `paths` one cost an hour on 2026-08-26 diagnosing why a phone would not
connect on a second mesh: `we announce 192.168.0.151:51821` was read as that
mesh's endpoint when it was the primary's, and the mesh in question was on
`51824`.

The pattern is always the same. A per-mesh fact gets stored or reported in a
place that predates there being more than one mesh, and the answer looks
right — it *is* right, for one mesh — while being silently wrong for the rest.

## The one that settles it

Found 2026-08-26, after the list above was written, while diagnosing why a
phone and a laptop **on the same LAN** could not connect on a new mesh:

    meshCfg := cfg
    meshCfg.NetworkKey = m.NetworkKey
    meshCfg.AdminKeys = m.AdminKeys
    meshCfg.Relay = m.Relay
    ...
    // meshCfg.ListenPort is never set

Both callers — the daemon and the phone — worked out the nth mesh's port, bound
WireGuard to it, and handed the mesh package a config still carrying the
**device's** port. `candidates()` builds every local address from
`m.cfg.ListenPort`, so a mesh listening on 51824 announced `192.168.0.151:51820`.

A peer on the same LAN dials that, reaches the **first mesh's** WireGuard
socket, and its handshake is rejected without comment because the keys belong to
another mesh. Both devices retry forever. It presents as *"two devices one hop
apart cannot find each other without a relay"*, and it was reported that way
twice — once here and once by somebody else who had never seen this config.

Only the first mesh escaped, **because its port is the device's port**. Every
mesh after it was broken from the day multi-mesh shipped, on both front-ends,
and it stayed invisible because the meshes that were tested all had relays —
which route around the wrong address entirely.

That is the argument in one field. Not that the distinction is untidy: that a
config with one mesh and a config with four are read by the same code, and the
code is correct for the first and quietly wrong for the rest. Nobody wrote a bug
here. Somebody wrote `cfg` and it meant two different things.

Fixed by `Config.ForMesh`, which both callers now use, and which cannot omit the
port because there is nowhere left to omit it from. That is a patch on the
symptom. The cause is that `Config` describes a device and a mesh at once.

## The second instance, still open

`Advertise` is the same shape and is not fixed. `candidates()` reads it:

    for _, a := range m.cfg.Advertise {
        add(a)
    }

`state.Mesh` has no `Advertise`, so it is device-wide. An entry is a full
`host:port`, which means a node configured with `advertise =
["203.0.113.5:51820"]` announces that exact string on **every** mesh — right for
the one listening on 51820 and wrong for the rest. It lands on the nodes most
likely to be reached first: relays and public boxes, which are the only ones
anybody sets `advertise` on.

`ListenPort` could be fixed by handing the mesh the port it bound. This one
cannot, because the right value is not derivable:

- **Match by port** — announce an entry only on the mesh whose ListenPort equals
  the entry's port. Correct where the external port equals the internal one, and
  a silent regression where it does not: `advertise = ["1.2.3.4:31820"]` on a
  node listening on 51820 matches no mesh and announces nothing, which is
  today's working single-mesh case broken.
- **Per-mesh `advertise`** — correct, and means anybody with a public node and
  several meshes must write one per mesh. NAT gives each mesh its own external
  port anyway, so there is a real value to write; it is not ceremony.
- **Device-wide applies to the first mesh only** — preserves today's behaviour
  exactly where it works, needs a warning for every other mesh, and is one more
  rule of the form "the first mesh is special".

**Decided 2026-08-26: per-mesh, and it no longer inherits.**
`mesh.<label>.advertise` is a mesh's own; the device-wide value belongs to the
mesh on the device's base port, which leaves a single-mesh config exactly as it
was. A mesh that now correctly announces nothing is indistinguishable from one
somebody forgot to configure, so the daemon names them at startup.

Note the third option is the same shape as the thing this document proposes
removing. That is the tell: every repair to a device-wide field either makes it
per-mesh or adds another "except the first" rule.

## What "treat them all the same" means

Every mesh is a `[mesh.<label>]` table. There is no top-level `network_key`,
no top-level `relay`, no top-level `listen_port`. `default` is a label like any
other, and only the default *name* when none is given.

Status, announces, admin files, relays and interfaces are all per mesh, with no
"which one is this" branch anywhere.

## The one asymmetry that has to survive

**Device identity.** The original mesh keeps the identity the device already
had; every later mesh derives its own:

    st.MeshState(networkID, cfg.NetworkKey != "" && mc.Label == DefaultLabel)

That is not cosmetic. Changing the identity of an existing mesh changes this
node's overlay address and invalidates its credential — every peer would see a
stranger. So one mesh on an upgraded device genuinely is different from the
others, and pretending otherwise would break every node that has ever run.

The fix is to make it **explicit and per-mesh** rather than implied by shape:

    [mesh.default]
    inherits_device_identity = true

One boolean, on the mesh it applies to, that new meshes never set. It replaces
an implicit rule — "the mesh in the old-shaped fields is the one with the old
identity" — with a stated fact. `MeshState.original` already is this flag; it
just is not written down anywhere a person can see.

## Migration

The risk is entirely here, and it is why this is a proposal rather than a
commit.

1. **Read both shapes.** Loading a config with top-level fields synthesises
   `[mesh.default]` with `inherits_device_identity = true`. This is roughly what
   `Meshes()` already does.
2. **Write only the new shape**, once, on the first config write after upgrade,
   with the old file kept as `config.toml.pre-flatten`.
3. **Delete the branches** — the 87 sites — after a release that only reads.

Steps 1 and 2 are mechanical. Step 3 is the payoff and cannot be rushed: a node
that half-migrates has two meshes claiming the same identity, which is worse
than the thing being fixed.

**Decided 2026-08-26: the relay settings keep inheriting.** Unlike `advertise`,
a relay address means the same thing to every mesh — the tag a device registers
under is derived per mesh from the relay's address, so one relay serves all of
them correctly. Pointing a phone at one relay and having every mesh use it is
the case worth keeping easy.

`mesh.<label>.relay_blind`, `.relay_addr` and `.relay_token` override for one
mesh, and `relay_blind = "none"` opts a mesh out without naming a relay of its
own. A literal empty list cannot say that: `[]` parses the same as an absent
line, which means inherit.


## Decided, 2026-09-29

Prompted by a tablet (x6) that joined `office` by invite and could only resolve
`peer.mesh`: the invite carried no label, so it wrote the old top-level shape,
its one mesh became `default`, and `default` is treated as "no label".
`peer.office.mesh` — what every multi-mesh device uses — could not resolve,
because x6 had never been told the word `office`.

Vaclav: *"we should drop the default mesh completely and only use the named
meshes"*, and two choices made explicitly:

1. **The invite suggests the name.** The inviter's label for the mesh rides in
   the invite. The joiner takes it unless told otherwise (`--mesh`), validates
   it as one DNS label, and refuses it on a collision with a mesh it already
   has. It is still a local name afterwards — renaming is local, as before.

   This is not the DNS-suffix field that was removed from the invite. That one
   was device-wide and let an inviter make a phone authoritative for `.com`. A
   label is scoped under the suffix — `peer.<label>.mesh` — so the worst a
   hostile inviter can do is choose a silly name for its own mesh.

2. **Qualified names only.** Every mesh answers `peer.<label>.mesh`, including a
   device's only mesh, and `peer.mesh` stops resolving anywhere. This reverses
   the August position that the short form should stay for typing: the same
   name meaning different machines on different devices is what x6 hit, and a
   name that resolves identically everywhere is worth the bookmarks it breaks.

Stages, in order:

1. New configs are always `[mesh.<label>]`: `init` requires a name, joins take
   the invite's suggestion, and the first mesh on a device carries
   `inherits_identity` so it keeps the base identity as before.
2. The resolver and `hosts --write` answer qualified names only.
3. Existing top-level configs migrate with `Flatten` plus a rename away from
   `default`; `status` says so until it has happened.
4. Everything still printing or using the short form — CLI output, the app's
   service URLs, docs, the site — moves to the qualified form.

### Stages 1 and 2: built

**New configs are named.** `shrooms init` requires `--mesh <name>` on a fresh or
prepared config and refuses `default`; on a config already on a mesh,
`--mesh` still adds a second one. Every first mesh — from `init`, from the
command line's `join`, from a waiting daemon's join, and from the app's join
and "create one" — is written as `[mesh.<name>]` by `Config.WithFirstMesh`,
which:

- sets `inherits_identity`, so the mesh announces with the device's base keys,
  which are the keys its credential names. Leaving it unset derives fresh keys
  beside a credential naming the old ones, and every peer refuses the device —
  `joinnamed_test.go` fails without it;
- pins the interface and port to the device's own, as `Flatten` does, so a
  later mesh that sorts first (`alpha` beside `home`) cannot take them;
- moves what the top-level fields said about the mesh — services, announce
  settings — onto it rather than dropping them.

A first mesh now always keeps the base identity, whatever it is called. It
used to derive a new one whenever a label was given on a first join, since
only the unlabelled shape could hold the base keys.

Its authority is filed as `admin-<name>.json`; commands that read an authority
with no `--mesh` fall back to the directory's only labelled one when
`admin.json` is absent, so a single-mesh device needs no flag.

**The invite suggests the name.** `invite.Response.Label` carries the
inviter's name for the mesh (`Config.MeshLabel`, set by `ForMesh`), never
`default`. The joiner takes `--mesh`, else the suggestion if it is a valid
label not already used here, else `mesh-<id>`; the choice is made after the
exchange and never fails there (`state.ChooseMeshLabel`).

The app's mesh identity is read from `InheritsIdentity` in all three places it
is chosen; it was decided by config shape, which also gave a *flattened* phone
config a derived identity.

**Qualified names only.** The daemon and the app resolve through one rule,
`mesh.ResolveQualified`: the last label is the mesh, and a name without one
answers nothing. `QualifiedDNSName` returns no name for an empty label. Status,
`bound`, service names and `hosts --write` all qualify, on a device with one
mesh as on one with several; `/etc/hosts` gets one entry for this device per
mesh, since it has a different address on each. A block written by an older
build still holding `peer.mesh` names is reported stale by `status`.

`shrooms key show` printed the top-level key, empty on a named mesh; it takes
`--mesh`, and uses the only mesh when there is one.

### Stages 3 and 4: open, and what was found for them

- **Migration (stage 3).** Existing top-level configs still run as a mesh
  labelled `default`, which now answers `peer.default.mesh`. `Flatten` plus
  `mesh rename default <name>` is the manual path; nothing does it
  automatically or says it is needed.
- **Short names in docs.** README (~24) and `site/guides.html` (~19) show
  `peer.mesh` / `peer.internal` forms that no longer resolve.
- **A false claim, twice.** `site/install.html` and the comment on
  `cred.Authority` say the address prefix derives from the admin key set, so
  adding a key would re-address every node. It derives from the network key.
- **`shrooms key rotate`** still rotates only the top-level key; on a named
  config it fails in `cfg.Key()`. Needs `--mesh`.
- **`manage_hosts`** runs per mesh, and each mesh writes the whole managed
  block with only its own entries, so on several meshes they overwrite each
  other. Older than this change; `hosts --write` is correct.
- **Additional meshes** (`join --mesh X` beside an existing one,
  `JoinAnotherWithInvite`) still require a name rather than taking the
  suggestion.
- **`shrooms hosts`** defaults its suffix to `internal`, the daemon's
  `manage_hosts` to `mesh`.
- **A prepared machine joined from the command line with no daemon running**
  goes through the additional-mesh path. Older than this change; the waiting
  daemon is what normally handles a prepared machine.
