import QtQuick

// Opens the Agents panel against a stand-in core and checks what it shows,
// then saves a picture of it: the panel runs only inside Basecamp, whose
// sandbox and IPC cannot be driven from a test, so the view is handed a
// bridge that answers like shrooms_core does — events shaped exactly as
// shrooms-agent sends them (Claude Code's stream-json, verbatim).
Item {
    id: top
    width: 1280; height: 800

    property string out: Qt.application.arguments[Qt.application.arguments.length - 1]
    property int eventCalls: 0

    readonly property var hosts: [{
        name: "laptop", mesh: "office", address: "fdb0:9afc:a5ef:388c:8264:7716:36fc:64eb",
        list: { sessions: [
            { name: "shrooms", dir: "/home/someone/logos-vpn", state: "waiting", pending: 1, running: true,
              last_seq: 9, last_time: "2026-10-03T14:27:31+02:00", auto_approve: false,
              context_used: 678705, context_window: 1000000, model: "claude-opus-5[1m]",
              preview: "The view loads and the existing checks pass." },
            { name: "notes", dir: "/home/someone/notes", state: "idle", pending: 0, running: false,
              last_seq: 3, last_time: "2026-10-02T09:00:00+02:00", auto_approve: true,
              context_used: 22703, context_window: 200000, model: "claude-haiku-4-5-20251001", preview: "" } ] } }]

    function ev(seq, kind, data, by) {
        return { seq: seq, time: "2026-10-03T14:2" + (seq % 10) + ":00+02:00", kind: kind, by: by || "", data: data }
    }
    readonly property var events: [
        ev(1, "message", { text: "Can you check **the** tests? http://vps.office.mesh:8099/x" }, "nothing.office"),
        ev(2, "claude", { type: "system", subtype: "init", session_id: "s1", model: "claude-opus-5[1m]" }),
        ev(3, "claude", { type: "assistant", message: { content: [
            { type: "thinking", thinking: "" },
            { type: "text", text: "## Running them\n\nFirst `make test`, then:\n\n- the **agent** package\n- the view\n\n```\ngo test ./...\n```" },
            { type: "tool_use", id: "t1", name: "Bash", input: { command: "make test", description: "Run the tests" } } ] } }),
        ev(4, "claude", { type: "user", message: { content: [ { type: "tool_result", tool_use_id: "t1", content: "ok  internal/agent\nok  cmd/shrooms", is_error: false } ] } }),
        ev(5, "claude", { type: "result", subtype: "success", total_cost_usd: 0.0123 }),
        ev(6, "setting", { auto_approve: false }, "laptop.office"),
        ev(7, "message", { text: "Now push it." }, "nothing.office"),
        ev(8, "claude", { type: "assistant", message: { content: [ { type: "tool_use", id: "t2", name: "Bash", input: { command: "git push origin master" } } ] } }),
        ev(9, "claude", { type: "control_request", request_id: "p1", request: { subtype: "can_use_tool", tool_name: "Bash",
            input: { command: "git push origin master" }, description: "Push to GitHub" } }),
        { seq: 9, kind: "partial", data: { text: "Pushing " } },
        { seq: 9, kind: "partial", data: { text: "**now**…" } },
    ]

    property var calls: []
    property string lastFind: ""
    property string lastPost: ""
    property string lastDelete: ""
    property string lastWatch: ""
    property string lastSearch: ""
    property bool findNone: false
    property int searchAsked: 0
    property var jobsNow: []

    Main {
        id: view
        anchors.fill: parent
        bridge: QtObject {
            function callModule(module, method, args) {
                top.calls.push(method)
                if (method === "status") return JSON.stringify({ name: "desk",
                    meshes: [ { label: "office", overlay: "fdb0:9afc:a5ef:1111:2222:3333:4444:5555" } ], peers: [
                    { name: "laptop", mesh: "office", overlay: "fdb0:9afc:a5ef:388c:8264:7716:36fc:64eb", online: true } ] })
                // The same machine answering on a second mesh address.
                if (method === "agentsFind" && top.findNone) return JSON.stringify([])
                if (method === "agentsFind") { top.lastFind = args[0]
                    return JSON.stringify(top.hosts.concat([Object.assign({}, top.hosts[0], { mesh: "home", address: "fd7b::1" })])) }
                if (method === "agentSearch") { top.lastSearch = args.join(" "); return JSON.stringify({ search: 1 }) }
                // Still running the first time it is asked, as a real search is.
                if (method === "agentSearched" && top.searchAsked++ === 0) return JSON.stringify({ id: 1, done: false, error: "", found: null })
                if (method === "agentSearched") return JSON.stringify({ id: 1, done: true, error: "", found: [
                    { seq: 3, time: "2026-10-03T14:22:00+02:00", role: "assistant", snippet: "All **tests** pass." },
                    { seq: 0, time: "2026-10-02T10:00:00+02:00", role: "user", snippet: "the tests, in a terminal", text: "Earlier: the tests, in a terminal." } ] })
                if (method === "agentWatch") { top.lastWatch = args.join(" "); return JSON.stringify({ ok: true }) }
                if (method === "agentEvents") {
                    top.eventCalls++
                    var after = Number(args[0])
                    // In pieces, as the core answers: the view must keep reading.
                    var upto = Math.min(top.events.length, after + 4)
                    return JSON.stringify({ next: upto, more: upto < top.events.length, connected: true, error: "",
                                            events: after >= top.events.length ? [] : top.events.slice(after, upto) })
                }
                if (method === "agentGet" && String(args[1]) === "/v1/harnesses") return JSON.stringify({ harnesses: [
                    { name: "claude", title: "Claude Code", caps: { approve: true, takeover: true } },
                    { name: "pi", title: "pi", caps: { approve: false, takeover: false } } ] })
                if (method === "agentGet" && String(args[1]).indexOf("/v1/conversations") === 0) return JSON.stringify({ conversations: [
                    { id: "c-new", dir: "/home/someone/shrooms", modified: "2026-10-03T15:00:00+02:00", size: 1000,
                      last_user: "fix the tether", last_assistant: "Fixed.",
                      terminals: [ { pid: 23173, dir: "/home/someone/shrooms", tmux: "cl-logos-vpn", args: "claude --continue" } ] },
                    { id: "c-taken", dir: "/home/someone/notes", modified: "2026-10-02T09:00:00+02:00", size: 10, adopted_by: "notes" } ] })
                if (method === "agentGet") return JSON.stringify({ history: [
                    { time: "2026-10-02T10:00:00+02:00", role: "user", text: "Earlier, in a terminal." },
                    { time: "2026-10-02T10:01:00+02:00", role: "assistant", text: "And *my* answer then." },
                    // The transcript holds the phone's own turns too: this one
                    // is event 1 and must not be shown twice.
                    { time: "2026-10-03T14:21:00+02:00", role: "user", text: "Can you check **the** tests?" } ] })
                if (method === "agentPost") { top.lastPost = args[2]; return JSON.stringify({ ok: true }) }
                if (method === "agentUpload") return JSON.stringify({ job: 1 })
                if (method === "agentDelete") { top.lastDelete = args[1]; return JSON.stringify({ ok: true }) }
                if (method === "agentRecord") return JSON.stringify(args[0] === "stop" ? { job: 2 } : { ok: true })
                if (method === "agentJobs") return JSON.stringify({ recording: false, jobs: top.jobsNow })
                return JSON.stringify({ error: "unknown " + method })
            }
        }
    }

    Timer {
        interval: 1500; running: true
        onTriggered: {
            console.error("HOSTS=" + view.agentHosts.length + " SESSIONS=" + view.agentHosts[0].sessions.length)
            console.error("PROBED=" + top.lastFind)
            view.openSession(view.agentHosts[0], "shrooms")
        }
    }
    // Just after opening, before the view's next poll: everything is read.
    Timer {
        interval: 1600; running: true
        onTriggered: console.error("LOADED=" + view.agentNext + "/" + top.events.length)
    }
    Timer {
        interval: 3500; running: true
        onTriggered: {
            var kinds = []
            for (var i = 0; i < chatCount(); i++) kinds.push(view.chatModelAt(i).kind)
            console.error("ROWS=" + kinds.join(","))
            console.error("WATCH=" + top.lastWatch)
            console.error("STREAMING=[" + view.agentStreaming + "] WORKING=" + view.agentWorking
                          + " CONTEXT=" + view.contextLabel(view.agentInfo.context_used, view.agentInfo.context_window)
                          + " MODEL=" + view.shortModel(view.agentInfo.model))
            for (i = 0; i < chatCount(); i++) {
                var r = view.chatModelAt(i)
                if (r.kind === "prompt") console.error("PROMPT open=" + r.open + " id=" + r.pid + " text=" + r.text)
            }
            view.answerPrompt("p1", true)

            // A file and a voice note, finishing in the core's own time.
            top.jobsNow = [
                { id: 1, kind: "upload", state: "done", name: "shot.png", path: "/home/x/.local/share/shrooms-agent/uploads/shrooms/20261003-150000-shot.png", text: "", error: "" },
                { id: 2, kind: "voice", state: "done", name: "voice note", path: "/x/voice.wav", text: "ahoj, tady Vašek", error: "" } ]
            view.pumpJobs()
            view.pumpJobs()
            console.error("ATTACHED=" + view.agentAttached.length + " STICK=" + view.chatStick)
            console.error("COMPOSER=[" + view.composerText() + "]")
            view.sendToAgent("look")

            console.error("SENT=" + JSON.stringify(JSON.parse(top.lastPost).text) + " ATTACHED_AFTER=" + view.agentAttached.length)

            // Search: asked of the core, read back, and a result opened — one
            // from before the agent is shown whole, one of its own is jumped to.
            view.searchOpen = true
            view.runSearch("  tests ")
            view.pumpSearch()
            console.error("STILLBUSY=" + view.searchBusy + " " + (view.searchFound === null))
            view.pumpSearch()
            console.error("SEARCH=" + top.lastSearch + " FOUND=" + view.searchFound.length + " BUSY=" + view.searchBusy)
            view.openFound(view.searchFound[1])
            console.error("READING=" + view.readingOpen() + " TEXT=" + view.reading.text)
            view.openFound(view.searchFound[0])
            var litRow = -1
            for (i = 0; i < chatCount(); i++) if (view.chatModelAt(i).seq === 3) { litRow = i; break }
            console.error("JUMP lit=" + view.agentLit + " row=" + litRow + " kind=" + (litRow >= 0 ? view.chatModelAt(litRow).kind : "")
                          + " searchOpen=" + view.searchOpen + " stick=" + view.chatStick
                          + " reach=" + view.tailReaching(300, 1000, 500) + "," + view.tailReaching(0, 1000, 5))

            // A question from the model: a card of its own, the options picked
            // (two of three, given in the order offered), and the answer sent.
            var qs = [ { question: "Which user?", header: "VPS user", multiSelect: false,
                         options: [ { label: "agent", description: "no sudo" }, { label: "root", description: "" } ] },
                       { question: "What else?", header: "Extras", multiSelect: true,
                         options: [ { label: "voice", description: "" }, { label: "backups", description: "" }, { label: "logs", description: "" } ] } ]
            top.events.push(ev(top.events.length + 1, "claude", { type: "control_request", request_id: "q1",
                request: { subtype: "can_use_tool", tool_name: "AskUserQuestion", input: { questions: qs } } }))
            view.pumpAgent()
            var qrow = null
            for (i = 0; i < chatCount(); i++) if (view.chatModelAt(i).kind === "question") qrow = view.chatModelAt(i)
            var before = view.questionAnswers("q1", qs)
            view.pickOption("q1", "Which user?", "root", false)
            view.pickOption("q1", "Which user?", "agent", false)
            view.pickOption("q1", "What else?", "logs", true)
            view.pickOption("q1", "What else?", "voice", true)
            view.answerQuestion("q1", qs)
            var posted = JSON.parse(top.lastPost)
            top.events.push(ev(top.events.length + 1, "answer", { prompt: "q1", allow: true, answers: posted.answers }, "desk"))
            view.pumpAgent()
            for (i = 0; i < chatCount(); i++) if (view.chatModelAt(i).kind === "question") qrow = view.chatModelAt(i)
            console.error("QUESTION open=" + (qrow !== null) + " before=" + before + " posted=" + JSON.stringify(posted)
                          + " after=" + qrow.open + " [" + qrow.answer + "]")

            // Bare URLs are links, in the model's markdown and in what was typed;
            // code and links already written are left alone.
            console.error("LINKMD=" + view.linkMarkdown("see https://pi.dev, or [docs](https://x.io/a) and `curl http://no.pe`\n```\nhttp://in.code\n```\n<https://already.io>"))
            console.error("LINKPLAIN=" + view.linkPlain("a <b> & http://vps.office.mesh:8099/x."))

            // A machine that misses a round of finding stays, as last seen;
            // greyed once quiet a while; forgotten only after days.
            top.findNone = true
            view.refreshAgents()
            var stayed = view.agentHosts.map(function(x) { return x.name + ":" + x.sessions.length }).join(",")
            var kept = view.agentHosts[0]
            console.error("FLAKY stayed=" + stayed + " now=" + view.hostReachable(kept, Date.now())
                          + " later=" + view.hostReachable(kept, Date.now() + 30000)
                          + " forgotten=" + view.mergeHosts([{ name: "old", lastSeen: 1, sessions: [] }], [], Date.now()).length)
            top.findNone = false
            view.refreshAgents()

            // Starring: first in the list, out of its machine's, and kept on the agent.
            var h0 = view.agentHosts[0]
            view.setStarred(h0, "shrooms", true)
            var starBody = top.lastPost
            console.error("STARRED=" + view.starredSessions.map(function(x) { return x.host.name + "/" + x.sess.name }).join(",")
                          + " rest=" + view.unstarred(view.agentHosts[0]).map(function(x) { return x.name }).join(",")
                          + " sent=" + starBody)
            view.setStarred(view.agentHosts[0], "shrooms", false)
            console.error("UNSTARRED=" + view.starredSessions.length + " sent=" + top.lastPost)

            // A session of another harness: offered when the machine has it,
            // created with it, and without auto-approve, which pi has no use for.
            view.loadHarnesses(view.agentHosts[0])
            var offered = view.harnesses.map(function(x) { return x.name }).join(",")
            view.nsHarness = "pi"
            view.createSession(view.agentHosts[0], "pi-proj", "~/proj", true)
            var made = JSON.parse(top.lastPost)
            console.error("HARNESS offered=" + offered + " sent=" + made.harness + " auto=" + made.auto_approve
                          + " label=[" + view.harnessLabel("pi") + "][" + view.harnessLabel("claude") + "]")
            view.loadHarnesses(null)
            view.agentCreating = true
            view.openSession(view.agentHosts[0], "shrooms")
            console.error("FORMCLOSED=" + !view.agentCreating)

            // Taking over a conversation from a terminal.
            view.loadConversations(view.agentHosts[0])
            console.error("CONVERSATIONS=" + view.conversations.length
                          + " TERMINAL=" + view.conversations[0].terminals[0].tmux
                          + " NAME=" + view.nameFor(view.agentHosts[0], view.conversations[0]))
            view.takeOver(view.agentHosts[0], view.conversations[0])
            var took = JSON.parse(top.lastPost)
            console.error("TAKEOVER name=" + took.name + " resume=" + took.resume + " open=" + view.agentOpen.session)

            // Deleting the open session: asks what the phone asks, then goes.
            console.error("DELETETEXT=" + (view.deleteSessionText("working").indexOf("cut off") > 0)
                          + "," + (view.deleteSessionText("idle").indexOf("conversation itself is kept") > 0))
            view.askDelete()
            console.error("DIALOG=" + view.deleteDialogOpen())
            view.deleteOpenSession()
            console.error("DELETED=" + top.lastDelete + " OPEN=" + (view.agentOpen === null ? "none" : view.agentOpen.session))
            console.error("CALLS=" + top.calls.filter(function(c) { return c.indexOf("agent") === 0 })
                          .filter(function(c, i, a) { return a.indexOf(c) === i }).join(","))
            top.grabToImage(function(img) {
                img.saveToFile(top.out)
                console.error("SAVED " + top.out)
                Qt.quit()
            })
        }
    }
    function chatCount() { return view.chatModelCount() }
}
