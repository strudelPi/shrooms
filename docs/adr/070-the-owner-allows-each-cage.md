# 070. The owner allows tasks from each cage, not from all of them or none

**Status:** accepted 2026-10-23 by the owner ("there would be a request for
accepting tasks, and I can approve or not"); being built. Amends ADR-044's
"the receiver decides", which stays: this adds a third answer to it.

## Context

ADR-044 lets a session take tasks from caged agents or not (`accept_caged`),
with a caged sender named by its own agent: `MACHINE/SESSION`, which the cage
cannot change. The setting was all or nothing.

The case that broke it (2026-10-22): a caged session builds an app, and
`jimmy-crib/publisher` has to take its "sign and publish this" task. Turning
`accept_caged` on for the publisher makes it take tasks from *every* caged
agent on every machine, which is not what anyone wants of the one agent that
holds the signing keys. Leaving it off means the caged builder can never ask.

Files between agents (ADR-048) already solve the same shape of problem: an
allow list per session, `MACHINE/SESSION` or `MACHINE/*`, and a refused sender
shown to the owner as a request they can allow or set aside.

## Decision

**A session takes tasks from every caged agent (`accept_caged`), or from those
its owner allowed by name (`accept_caged_from`). A task from any other caged
agent is held for the owner, not refused.**

- **The allow list.** `accept_caged_from`: `MACHINE/SESSION` or `MACHINE/*`,
  kept with the session, set in the apps, by `PATCH /v1/sessions/NAME`, or
  with `shrooms-agent caged allow|deny`. The names are those the sender's own
  agent writes (ADR-044), so a cage cannot claim to be another.
- **Held, in A2A's own state.** The task is recorded in
  `TASK_STATE_AUTH_REQUIRED` and not sent to the session. The asker gets that
  state straight away and its tools say what it means; it has nothing to do
  but wait. A blocking asker is answered at once rather than kept waiting.
- **A request to the owner.** The session's info lists the held tasks
  (`task_requests`: task, sender, title, when); the apps show them beside file
  requests.
- **Allow** adds the sender to the list (a standing rule, as for files) and
  releases every task held from it, oldest first, as ordinary queued tasks.
- **Decline** (`decline_tasks_from`, or `shrooms-agent caged decline`) rejects
  the tasks held from that sender, with "declined by the owner", and tells
  each asker. The sender is not remembered either way: its next task is a new
  request.
- **Turning `accept_caged` on** releases everything held.
- **Only the owner decides.** A caged request to change settings is refused
  (ADR-044), so a cage cannot allow itself.
- **A held task still expires** with every other open task, after a day.
- **A sender its agent cannot name** (no machine or no session) is refused as
  before.

## Consequences

- The publisher can take tasks from one builder's cage and from no other.
- The identity is a session's *name* on a machine. A caged session made later
  under the same name on the same machine inherits the approval, as a file
  sender does. A cage has no identifier of its own to bind to; if names prove
  too loose, that is the change to make, not a reason to keep all-or-nothing.
- A held task counts against the sender's hourly A2A limit like any other, so
  a cage cannot flood an owner with requests.
- The Android app and the Basecamp view show and act on the requests (front-end
  parity); the phone also notifies, since a task is waiting on the owner,
  which a refused file never was.
