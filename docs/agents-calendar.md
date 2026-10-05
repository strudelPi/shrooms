# Shrooms Agents and Scala: a calendar per agent, events that wake it

**Status:** analysis, 2026-10-05. Nothing built. For a decision.

The wish: each agent has its own calendar. "Create a reminder for 8:00 on
Monday to check the Logos roadmap" → the agent puts it in its calendar, it
shows in your Scala, and on Monday at 8:00 the agent does it. Events and cron
jobs for agents, with Scala as the place you see and edit them.

## Two halves, of very different weight

1. **The agent acting at a time** — a scheduler in `shrooms-agent`. At the time
   it sends the session a turn ("It is Monday 08:00 — your reminder: check the
   Logos roadmap"), which wakes it like any message; the reply notifies the
   phone as replies do. This is the value, and it needs no Scala at all.
2. **Scala as the window onto it** — the schedule mirrored into a Scala
   calendar the agent owns, which you join once and then see, and edit, in
   Scala on the desktop and phone. This is the cost.

## What Scala is (from `scala-core`, 2026-10-05)

- A calendar is an append-only log of signed events (`cal.meta`, `event.put`,
  `event.del`, `member.set`, `event.rsvp`) folded into state; HLC order,
  last write wins per event. Events: `title, startTime, endTime` (UTC ms),
  `description, location, allDay, reminderMin, recur {freq, interval, until}`
  and free extra keys. No time zones; recurrence is expanded in device-local
  time.
- Sync: SDS Reliable Channels over Logos Delivery (`logos.test`, cluster 2),
  through the `loam_core` module, on `/scala/1/<calendarId>/json`. JSON,
  AES-256-GCM with a derivation quirk, ECDSA signatures (secp256k1, a
  SHA-256 address), catch-up by RBSR. Golden vectors in `mobile/test/`.
- Membership is the symmetric key (the `scala://join?...` link); roles —
  owner, editor, viewer — are signed `member.set` events, and the fold drops
  writes a role does not allow.
- APIs: the C++ core exposes `createCalendar`, `createEventAt`,
  `updateEvent`, `deleteEvent`, `listEvents`, `generateShareLink`,
  `manageMember`, … The "intents" (`scala.event.create`, …) live in the view
  and need Basecamp, its window and a person's click: not for an agent.
- Headless: `logoscore` with `scala`, `loam_core`, `storage_module` and the
  delivery module (`hub/scala-hub.sh`, `docs/hub.md`), driven by
  `logos-hub call`. No HTTP API, no Go, Rust or Node library.
- Reminders fire on Android (OS notifications up to 45 days ahead); Basecamp
  has none beyond an in-app banner.

## Ways to connect the agent to Scala

**A. Scala headless on every agent machine.** Each machine runs `logoscore`
with Scala's modules; the agent drives it through the CLI (its comma
splitting and number typing are known traps — `createEventAt` and
`manageMember` exist because of them). Works today, but four Logos modules
and a second Logos Delivery node on the VPS, atlas, jimmy-crib and the laptop,
each kept at the version the clients run.

**B. One calendar hub for all agents (recommended for Scala).** One machine
(jimmy-crib, say) runs Scala headless — the hub `docs/hub.md` already
describes — plus a small bridge on the mesh: `POST /calendars/{agent}/events`
and the like, which call the hub's Scala in that agent's identity (a
`loam_core` soft identity per agent, made with `addSoftIdentity`). Agents
stay simple: an HTTP call over the mesh, as they already make. One thing to
deploy and keep in step with Scala; the hub's operator (you) can read the
agents' calendars, which is the point anyway.

**C. Scala's protocol in Go, inside `shrooms-agent`.** The cleanest end state
— no Logos modules beside the agent, which already reaches Logos Delivery
through shrooms' Go binding — and the most work: the event and HLC format,
canonical JSON and secp256k1 signing, the key-derivation quirk and envelope,
SDS Reliable Channels and RBSR catch-up, and the fold's permission rules so
writes are not silently dropped; then kept in step as Scala's protocol moves.
Golden vectors make it checkable.

## How it would behave

- Each agent session gets a calendar, "agent · <session> @ <machine>", owned
  by the agent's identity. The apps show its invite link ("see in Scala");
  join once, on the desktop or the phone.
- The model gets a tool (MCP, as for cross-agent messages): `schedule(when,
  what, repeat?)`, `list`, `cancel`. "Every weekday at 8" maps to Scala's
  `recur {weekly…}` — Scala has daily/weekly/monthly/yearly with an interval
  and an end, no exceptions and no time zone, so "08:00" is fixed in the
  agent's time zone and said so.
- Edits flow back: move or delete the event in Scala and the agent's schedule
  follows; add an event to the agent's calendar and it is a task for the
  agent at that time.
- Reminders reach you as the agent's reply — a notification on the phone,
  as now — and as Scala's own reminder on Android if `reminderMin` is set.

## The risk to design for first

An event in the agent's calendar becomes a prompt the agent runs — on a
session that may auto-approve tools. Knowing the calendar's key is enough to
write to an *open* calendar. So: agent calendars are created closed, you are
granted editor by address, and the agent acts only on events signed by itself
or by addresses it was told to trust; anything else is shown, not run. Scala's
signatures make "who wrote this event" checkable, which is what makes this
workable.

## Effort, roughly

| piece | effort |
|---|---|
| Scheduler in the agent, the tool, the apps showing a session's schedule | ~2 days |
| B: hub + bridge + identities + the mirror both ways | ~4–5 days, plus time with `logoscore` |
| C instead of B: Scala's protocol in Go | ~2 weeks, interop the risk |

## Recommendation

The scheduler first — it is most of the value, works without Scala, and its
schedule is what any later Scala mirror shows. Then B, a calendar hub on
jimmy-crib, if seeing and editing the schedule in Scala is worth it in
practice; C only if Scala becomes something every agent machine should speak
natively.
