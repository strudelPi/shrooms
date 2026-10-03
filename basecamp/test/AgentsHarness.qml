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

    Main {
        id: view
        anchors.fill: parent
        statusPath: "/nonexistent.json"
        bridge: QtObject {
            function callModule(module, method, args) {
                top.calls.push(method)
                if (method === "status") return JSON.stringify({ name: "desk",
                    meshes: [ { label: "office", overlay: "fdb0:9afc:a5ef:1111:2222:3333:4444:5555" } ], peers: [
                    { name: "laptop", mesh: "office", overlay: "fdb0:9afc:a5ef:388c:8264:7716:36fc:64eb", online: true } ] })
                // The same machine answering on a second mesh address.
                if (method === "agentsFind") { top.lastFind = args[0]
                    return JSON.stringify(top.hosts.concat([Object.assign({}, top.hosts[0], { mesh: "home", address: "fd7b::1" })])) }
                if (method === "agentWatch") return JSON.stringify({ ok: true })
                if (method === "agentEvents") {
                    top.eventCalls++
                    var after = Number(args[0])
                    return JSON.stringify({ next: top.events.length, connected: true, error: "",
                                            events: after >= top.events.length ? [] : top.events.slice(after) })
                }
                if (method === "agentGet") return JSON.stringify({ history: [
                    { time: "2026-10-02T10:00:00+02:00", role: "user", text: "Earlier, in a terminal." },
                    { time: "2026-10-02T10:01:00+02:00", role: "assistant", text: "And *my* answer then." },
                    // The transcript holds the phone's own turns too: this one
                    // is event 1 and must not be shown twice.
                    { time: "2026-10-03T14:21:00+02:00", role: "user", text: "Can you check **the** tests?" } ] })
                if (method === "agentPost") return JSON.stringify({ ok: true })
                return JSON.stringify({ error: "unknown " + method })
            }
        }
    }

    Timer { interval: 300; running: true; onTriggered: view.agentsOpen = true }
    Timer {
        interval: 1500; running: true
        onTriggered: {
            console.error("HOSTS=" + view.agentHosts.length + " SESSIONS=" + view.agentHosts[0].sessions.length)
            console.error("PROBED=" + top.lastFind)
            view.openSession(view.agentHosts[0], "shrooms")
        }
    }
    Timer {
        interval: 3500; running: true
        onTriggered: {
            var kinds = []
            for (var i = 0; i < chatCount(); i++) kinds.push(view.chatModelAt(i).kind)
            console.error("ROWS=" + kinds.join(","))
            console.error("STREAMING=[" + view.agentStreaming + "] WORKING=" + view.agentWorking
                          + " CONTEXT=" + view.contextLabel(view.agentInfo.context_used, view.agentInfo.context_window)
                          + " MODEL=" + view.shortModel(view.agentInfo.model))
            for (i = 0; i < chatCount(); i++) {
                var r = view.chatModelAt(i)
                if (r.kind === "prompt") console.error("PROMPT open=" + r.open + " id=" + r.pid + " text=" + r.text)
            }
            view.answerPrompt("p1", true)
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
