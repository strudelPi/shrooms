# Shrooms Agents together: messages, schedules and where work came from

**Status:** design for discussion, 2026-10-05. Nothing built. Builds on
`docs/agents-calendar.md` (a scheduler; Scala through a hub) and the
cross-agent idea (an MCP tool for one agent to message another).

## One idea, not three

Cross-agent messages, schedules, and "events for several agents" are the same
thing seen from different sides: **a task** — a prompt for one or more agents,
to run now or at a time (once or repeating), sent by someone who can be traced.

```
task {
  id
  from:    you (a device) | an agent ("laptop/shrooms") | a calendar event
  to:      ["laptop/shrooms", "jimmy-crib/vpavlin", …]
  at:      now | a time, with repeat {daily|weekly|monthly|yearly, interval, until}
  prompt
  parent:  the task this one came from (none when you started it)
  reply:   whether, and to whom, the result goes back
}
```

- A cross-agent message is a task with `at: now`.
- A reminder is a task to yourself's agent at a time.
- "Every Monday at 8, the shrooms and jimmy-crib agents check the roadmap" is
  one task with two recipients.
- One agent asking another to do something later is a task with `from` an agent.

Build the task once — in the agent, with a store, a scheduler and an API — and
each of the wishes is a way of creating one.

## Where each piece lives

**In each `shrooms-agent`:** a small task store and scheduler. At a task's time
it sends the session a turn that says where it came from ("Scheduled by you
for Monday 08:00:" / "From laptop/shrooms:"). Agents reach each other's
schedulers over the mesh, as the apps already reach them: `POST /v1/tasks` on
the recipient's agent. No Scala needed for any of this.

**For the model:** one MCP tool, `task`, with `send` (now), `schedule`
(later), `list` and `cancel`; recipients by name, from `list_agents`. The
reply comes back to the sender as its own turn, linked to the task.

**Scala (optional, through the hub of `docs/agents-calendar.md`):** one shared
calendar, **Agents**, mirrored both ways:

- each scheduled task is an event; its recipients are a custom field.
  Scala calendars carry a schema of custom fields that its UI shows as inputs
  (`fields`, `updateCalendarMeta(schema)`), so **you can schedule something for
  several agents from Scala itself**: create an event in Agents, tick the
  agents in its field.
- each recipient answers with Scala's RSVP (`event.rsvp`) when it takes the
  task, so "who has it" is visible in the calendar; its result can go into the
  event's description.
- one calendar rather than one per agent: multi-agent events have a single
  home, and you see every agent's plans in one place. A colour per machine can
  come from a field.

## Where work came from

Every turn an agent receives already records who sent it (the "from nothing",
"from laptop" you noticed). Tasks add *why*: `from`, `parent`, and the task's
time. That is enough for:

- **in the conversation:** a turn shows "scheduled by you · Mon 08:00" or
  "from laptop/shrooms ↗", and the arrow opens the turn that asked;
- **in the list (the left pane):** a session doing work for another shows it —
  "← laptop/shrooms" under its name, a link to the origin.

A full tree in the left pane — sessions nested under the session or person
that set them going — is probably not worth it yet: sessions are long-lived
and work for many origins over a day, so the tree would be of *tasks*, not
sessions, and the list would reshape itself as tasks come and go. The origin
line and the link answer "where did this come from?" with none of that. If
chains of agents become common, an "activity" view — today's tasks as a tree,
each with its sessions — is the place for the tree, beside the list rather
than replacing it.

## What must hold, whatever is built

- **A human at the root.** Every task traces back, through `parent`, to
  something you did — a message, a schedule, a Scala event. An agent may
  create tasks only inside a chain that started with you.
- **Depth and rate limits.** A chain stops at a depth (say 3) and an agent may
  create only so many tasks an hour, so two agents cannot keep each other
  busy forever.
- **Trust by signature, not by reachability.** An agent runs a task only from
  you or from agents on its allow-list; a Scala event only if it is signed by
  an identity it trusts (Scala signs every event). Anything else is shown and
  not run.
- **Auto-approve is the sharp edge.** A session that runs tools without asking
  accepts tasks only from you unless switched on per session ("accepts tasks
  from agents").
- **Visible.** Every task, its origin and its result show in both apps, so you
  can see and stop what agents are doing to each other.

## Order of work

1. **Tasks in the agent**, with `at: now` and later, between agents over the
   mesh; the MCP tool; origin in turns and in the list; the limits above.
   (Cross-agent messages and the scheduler at once — ~4–5 days.)
2. **The Agents calendar in Scala**, through the hub; the recipients field and
   RSVPs. (~4–5 days, mostly the hub and the mirror.)
3. An activity view with the tree, only if chains of agents turn out to be
   common.

## Open questions

- Names for agents across the mesh: `machine/session` as now, or something
  that survives a session being renamed?
- Should a recurring task run in the same session each time (its context
  grows) or start a fresh one (it forgets)? Probably a choice per task.
- Time zones: Scala has none; tasks would be in the recipient machine's zone,
  said so on the event. Fine for one person across one zone.
