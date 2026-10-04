import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs

// Shrooms Agents (docs/agents.md): the Claude Code sessions on the owner's
// machines, talked to over the shrooms mesh. What grows out of the mycelium —
// a module of its own beside the shrooms one, sharing its core (shrooms_core),
// which does all the networking: Basecamp's sandbox blocks it here.
Item {
    id: root
    width: 1280; height: 800

    // The shrooms palette: the same app family, the same meanings.
    readonly property color cVoid:     "#07090B"
    readonly property color cPanel:    "#0E1216"
    readonly property color cLine:     "#1C2229"
    readonly property color cAsh:      "#6B7680"
    readonly property color cBone:     "#D6DDE3"
    readonly property color cPhosphor: "#35F0A0"
    readonly property color cAmber:    "#F0B429"
    readonly property color cRust:     "#E05252"
    readonly property color cViolet:   "#9A7BFF"
    readonly property color cSky:        "#5AA9FF"
    readonly property color cBlossom:    "#FF6FB5"
    readonly property color cChartreuse: "#C8E64A"
    readonly property var meshTints: [cSky, cBlossom, cChartreuse, cAsh]

    readonly property real autoScale: Math.max(1.0, Math.min(1.45, root.width / 2000))
    property real uiNudge: 0
    readonly property real uiScale: Math.max(0.8, Math.min(2.2, autoScale + uiNudge))
    function fs(n) { return Math.round(n * root.uiScale) }
    function sz(n) { return Math.round(n * root.uiScale) }

    // Basecamp's bridge to the core module. A property, so a test harness can
    // hand the view a stand-in (test/AgentsHarness.qml).
    property var bridge: typeof logos !== "undefined" ? logos : null
    readonly property bool haveCore: !!bridge && !!bridge.callModule
    function callCore(method, args) {
        if (!haveCore) return ""
        try {
            return String(bridge.callModule("shrooms_core", method, args || []))
        } catch (e) {
            return ""
        }
    }

    // The daemon's status, for this device's addresses and its peers: where
    // agents may be.
    property var st: ({})
    property var peers: []
    property string problem: ""
    function reload() {
        var d = unwrap(callCore("status", []))
        if (d && typeof d === "object" && !d.error) {
            root.st = d
            root.peers = d.peers || []
            root.problem = ""
        } else if (d && d.error) {
            root.problem = d.error + (d.detail ? " — " + d.detail : "")
        }
    }
    Timer { interval: 5000; running: root.haveCore; repeat: true; triggeredOnStart: true; onTriggered: root.reload() }

    // What the last action said, shown at the bottom until the next one.
    property string said: ""
    property bool saidBad: false

    TextEdit { id: clipboard; visible: false; width: 0; height: 0 }
    function copyText(s) {
        if (!s) return
        clipboard.text = String(s)
        clipboard.selectAll()
        clipboard.copy()
        root.said = "copied"
        root.saidBad = false
    }
    // Bare URLs as links, by the phone's rule (Markdown.kt bareUrl): no
    // trailing punctuation, nothing inside brackets or quotes.
    readonly property var bareUrl: /https?:\/\/[^\s<>()\[\]`"']+[^\s<>()\[\]`"'.,;:!?]/g
    // Markdown with its bare URLs made autolinks (<url>), leaving code — fenced
    // or inline — and URLs already in a link alone. Qt's markdown does not
    // link a bare URL by itself.
    function linkMarkdown(md) {
        var lines = String(md || "").split("\n"), fenced = false
        for (var i = 0; i < lines.length; i++) {
            if (/^\s*(```|~~~)/.test(lines[i])) { fenced = !fenced; continue }
            if (fenced) continue
            var parts = lines[i].split("`")
            for (var j = 0; j < parts.length; j += 2) {
                parts[j] = parts[j].replace(bareUrl, function(u, at, whole) {
                    var before = at > 0 ? whole.charAt(at - 1) : ""
                    return (before === "(" || before === "<" || before === "[") ? u : "<" + u + ">"
                })
            }
            lines[i] = parts.join("`")
        }
        return lines.join("\n")
    }
    // Plain text — what somebody typed, where a * is just a * — as rich text
    // with only its URLs made links.
    function linkPlain(t) {
        var esc = String(t || "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
        esc = esc.replace(bareUrl, function(u) { return '<a href="' + u.replace(/"/g, "%22") + '" style="color:#5AA9FF">' + u + "</a>" })
        return '<span style="white-space:pre-wrap">' + esc + "</span>"
    }
    // Basecamp's sandbox blocks every http and https URL in a view, so
    // Qt.openUrlExternally does nothing here: the core opens it (xdg-open).
    // Copied, and said so, when even that cannot.
    function openUrl(u) {
        if (!u) return
        var r = unwrap(callCore("agentOpenUrl", [String(u)]))
        if (r && r.ok) return
        if (Qt.openUrlExternally(u)) return
        copyText(u)
        root.said = "could not open " + u + (r && (r.detail || r.error) ? " (" + (r.detail || r.error) + ")" : "") + " — copied it instead"
        root.saidBad = true
    }

    // Mesh colours as the shrooms view assigns them: by the mesh's place in
    // this device's sorted list.
    function meshTint(label) {
        var ls = (root.st && root.st.meshes ? root.st.meshes : []).map(function(m) { return m.label }).sort()
        var i = Math.max(0, ls.indexOf(label))
        return meshTints[i % meshTints.length]
    }

    property bool prefsLoaded: false
    function loadPrefs() {
        if (prefsLoaded || !haveCore) return
        prefsLoaded = true
        // The machines as last seen, at once (mergeHosts).
        try {
            var kept = JSON.parse(String(callCore("getPref", ["agent_hosts"]) || "[]"))
            if (Array.isArray(kept) && root.agentHosts.length === 0)
                root.agentHosts = kept.filter(function(h) { return h && h.name && h.address && Array.isArray(h.sessions) })
        } catch (e) {}
        var n = parseFloat(String(callCore("getPref", ["ui_nudge"]) || ""))
        if (!isNaN(n)) root.uiNudge = Math.max(-0.4, Math.min(1.0, n))
    }
    function savePref(key, value) {
        if (!haveCore) return
        callCore("setPref", [key, String(value)])
    }
    // After the first paint: a call during construction freezes the view.
    Component.onCompleted: Qt.callLater(function() { root.loadPrefs(); root.reload() })

    Rectangle { anchors.fill: parent; color: cVoid }

    Text {
        anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom
        anchors.margins: root.sz(6)
        z: 60
        visible: text !== ""
        text: root.said !== "" ? root.said : root.problem
        color: (root.saidBad || root.said === "") ? cRust : cAsh
        elide: Text.ElideRight
        font.family: "monospace"; font.pixelSize: root.fs(10)
    }

    // ========================================================================
    // Agents (docs/agents.md): the Claude Code sessions on the owner's
    // machines, as the Android app shows them. Everything that touches the
    // network runs in shrooms_core on threads of its own; this only reads what
    // it collected, so no call here can stall the view.
    // ========================================================================

    // Always open: this module is the panel.
    readonly property bool agentsOpen: true
    property var agentHosts: []
    property var agentOpen: null          // {address, name, mesh, session}
    property var agentEventsList: []      // the agent's events, partials left out
    property var agentEarlier: []         // from the transcript, before the events
    property string agentStreaming: ""
    property int agentNext: 0
    property bool agentConnected: false
    property string agentProblem: ""
    property bool agentCreating: false
    // Follow new messages while at the bottom; stop once scrolled up.
    property bool chatStick: true
    // Files sent to the session's machine, named in the next message.
    property var agentAttached: []
    property var agentJobsSeen: ({})
    property string agentSending: ""
    property bool agentRecording: false
    property bool agentTranscribing: false
    // Conversations on the host a new session is being made on.
    property var conversations: []
    property string conversationsProblem: ""

    function unwrap(raw) {
        var r = raw
        for (var k = 0; k < 2 && typeof r === "string"; k++) {
            try { r = JSON.parse(r) } catch (e) { return null }
        }
        return r
    }

    function agentCall(method, args) {
        var r = unwrap(callCore(method, args))
        if (r && r.error) {
            root.said = (r.detail || r.error)
            root.saidBad = true
            return null
        }
        return r
    }

    // "name|mesh|address;..." of where agents may be: this device first — an
    // agent on the machine Basecamp runs on is not a peer of it, and was
    // missed — then the peers that can be reached now.
    function agentPeers() {
        var out = []
        var ms = (root.st && root.st.meshes) ? root.st.meshes : []
        for (var m = 0; m < ms.length; m++) {
            if (ms[m] && ms[m].overlay) out.push((root.st.name || "this device") + "|" + (ms[m].label || "") + "|" + ms[m].overlay)
        }
        for (var i = 0; i < root.peers.length; i++) {
            var p = root.peers[i]
            if (p.online && p.overlay) out.push(p.name + "|" + (p.mesh || "") + "|" + p.overlay)
        }
        return out.join(";")
    }

    function refreshAgents() {
        var r = unwrap(callCore("agentsFind", [agentPeers()]))
        if (!Array.isArray(r)) return
        var hosts = [], seen = {}
        for (var i = 0; i < r.length; i++) {
            var h = r[i]
            // One machine on several meshes answers on each of its addresses;
            // it is one machine with one set of sessions.
            if (seen[h.name]) continue
            seen[h.name] = true
            hosts.push({ name: h.name, mesh: h.mesh, address: h.address,
                         sessions: (h.list && h.list.sessions) ? h.list.sessions : [] })
        }
        var now = Date.now()
        root.nowMs = now
        root.agentHosts = mergeHosts(agentHosts, hosts, now)
        // Kept for the next start, now and then rather than every round.
        if (now - lastHostsSave > 30000) {
            lastHostsSave = now
            savePref("agent_hosts", JSON.stringify(agentHosts))
        }
    }

    // The list kept between rounds of finding — the phone's HostCache: a
    // machine that misses a round stays where it is, with its sessions as last
    // seen, rather than vanishing, coming back, and moving everything under
    // the reader. Greyed once quiet for staleMs; forgotten after forgetMs.
    readonly property double staleMs: 25000
    readonly property double forgetMs: 7 * 24 * 3600 * 1000
    property double nowMs: Date.now()
    property double lastHostsSave: 0
    function mergeHosts(prev, found, now) {
        var out = [], names = {}
        for (var i = 0; i < found.length; i++) {
            var h = Object.assign({}, found[i], { lastSeen: now })
            names[h.name] = true
            out.push(h)
        }
        for (i = 0; i < (prev || []).length; i++) {
            var p = prev[i]
            if (names[p.name] || !(now - (p.lastSeen || 0) < forgetMs)) continue
            out.push(p)
        }
        out.sort(function(a, b) { return a.name < b.name ? -1 : 1 })
        return out
    }
    function hostReachable(h, now) { return (now === undefined ? nowMs : now) - (h.lastSeen || 0) < staleMs }

    // The open session's figures, from the last round of finding.
    readonly property var agentInfo: {
        if (!agentOpen) return null
        for (var i = 0; i < agentHosts.length; i++) {
            if (agentHosts[i].address !== agentOpen.address) continue
            var ss = agentHosts[i].sessions
            for (var j = 0; j < ss.length; j++) if (ss[j].name === agentOpen.session) return ss[j]
        }
        return null
    }

    // Sessions open at their last agentTail events; "load them" asks for all
    // (0), and a search result further back for as many as reach it.
    readonly property int agentTail: 300
    property int agentTailNow: agentTail
    function openSession(h, s, tail) {
        var t = (tail === undefined || tail === null) ? agentTail : tail
        root.agentTailNow = t
        root.searchOpen = false
        // Picking a session in the list leaves the "+ session" form, which
        // otherwise stayed in front of it (2026-10-04).
        root.agentCreating = false
        root.agentOpen = { address: h.address, name: h.name, mesh: h.mesh, session: s }
        root.agentEventsList = []
        root.agentEarlier = []
        root.agentStreaming = ""
        root.agentNext = 0
        root.chatStick = true
        root.agentAttached = []
        chatModel.clear()
        agentCall("agentWatch", [h.address, s, String(t)])
        // After the first paint: a call during construction of what it fills
        // freezes the view.
        Qt.callLater(function() {
            if (!root.agentOpen) return
            var r = agentCall("agentGet", [root.agentOpen.address,
                "/v1/sessions/" + root.agentOpen.session + "/history?limit=30"])
            if (r && r.history) { root.agentEarlier = r.history; rebuildChat() }
            pumpAgent()
        })
    }

    function pumpAgent() {
        if (!agentOpen) return
        var r = unwrap(callCore("agentEvents", [String(agentNext)]))
        if (!r || r.next === undefined) return
        root.agentConnected = !!r.connected
        root.agentProblem = r.error || ""
        if (!r.events || r.events.length === 0) return
        var evs = root.agentEventsList.slice()
        var streaming = root.agentStreaming
        for (var i = 0; i < r.events.length; i++) {
            var e = r.events[i]
            if (e.kind === "partial") { streaming += (e.data && e.data.text) || ""; continue }
            var t = e.data ? e.data.type : ""
            if (e.kind === "claude" && (t === "assistant" || t === "result")) streaming = ""
            evs.push(e)
        }
        root.agentNext = r.next
        root.agentEventsList = evs
        root.agentStreaming = streaming
        rebuildChat()
        // The core answers in pieces of about half a megabyte: keep reading
        // until caught up, without waiting for the next tick.
        if (r.more) Qt.callLater(pumpAgent)
    }

    function epoch(s) { var t = Date.parse(s || ""); return isNaN(t) ? 0 : t }
    function clock(ms) {
        if (!ms) return ""
        var d = new Date(ms), now = new Date()
        var hm = ("0" + d.getHours()).slice(-2) + ":" + ("0" + d.getMinutes()).slice(-2)
        return d.toDateString() === now.toDateString() ? hm : (d.getDate() + "." + (d.getMonth() + 1) + ". " + hm)
    }
    function summarise(input) {
        if (!input) return ""
        // AskUserQuestion: what it asks, not its JSON.
        if (Array.isArray(input.questions)) return input.questions.map(function(q) { return q.question }).join("  ·  ")
        var ks = ["command", "file_path", "pattern", "path", "url", "query", "description", "prompt"]
        for (var i = 0; i < ks.length; i++) if (input[ks[i]]) return String(input[ks[i]])
        return JSON.stringify(input).slice(0, 200)
    }
    function contextLabel(used, win) {
        if (!used || !win) return ""
        return Math.min(100, Math.floor(used * 100 / win)) + "% of " + (win >= 1000000 ? (win / 1000000) + "M" : (win / 1000) + "k")
    }
    function shortModel(m) { return String(m || "").replace(/^claude-/, "").replace("[", " ").replace("]", "").replace(/-\d{8}$/, "") }

    // The conversation as rows, with the same rules as the Android app
    // (AgentChat.kt): which prompts are open, answered, or orphaned by a
    // process that stopped; history only before the first kept event.
    function chatItems() {
        var evs = agentEventsList, out = []
        var answers = {}, lastStop = -1
        for (var i = 0; i < evs.length; i++) {
            var e = evs[i]
            if (e.kind === "answer" && e.data) answers[e.data.prompt] = (e.data.answers
                ? "answered: " + Object.keys(e.data.answers).map(function(k) { return e.data.answers[k] }).join("; ")
                : (e.data.allow ? "allowed" : "denied")) + (e.by ? " from " + e.by : "")
            if (e.kind === "stopped") lastStop = e.seq
        }
        var first = 0
        for (i = 0; i < evs.length; i++) if (epoch(evs[i].time)) { first = epoch(evs[i].time); break }
        for (i = 0; i < agentEarlier.length; i++) {
            var h = agentEarlier[i], ht = epoch(h.time)
            if (first && ht >= first) continue
            out.push({ key: "h" + i, seq: 0, kind: h.role === "user" ? "you" : "said", earlier: true, text: h.text, time: ht, by: "" })
        }
        var per = {}
        function add(e, item) {
            per[e.seq] = (per[e.seq] || 0) + 1
            item.key = "e" + e.seq + "-" + per[e.seq]
            item.seq = e.seq
            item.time = epoch(e.time)
            item.earlier = false
            out.push(item)
        }
        for (i = 0; i < evs.length; i++) {
            e = evs[i]
            var d = e.data || {}
            if (e.kind === "message") add(e, { kind: "you", text: d.text || "", by: e.by || "" })
            else if (e.kind === "stopped") add(e, { kind: "note", text: "asleep; the next message wakes it" })
            else if (e.kind === "setting" && d.auto_approve !== undefined)
                add(e, { kind: "note", text: (d.auto_approve ? "auto-approve on" : "auto-approve off") + (e.by ? " from " + e.by : "") })
            else if (e.kind === "claude") {
                if (d.type === "assistant" && d.message && d.message.content) {
                    var c = d.message.content
                    for (var j = 0; j < c.length; j++) {
                        if (c[j].type === "text" && String(c[j].text).trim() !== "") add(e, { kind: "said", text: String(c[j].text).trim() })
                        else if (c[j].type === "tool_use") add(e, { kind: "tool", text: c[j].name + "  " + summarise(c[j].input) })
                    }
                } else if (d.type === "user" && d.message && Array.isArray(d.message.content)) {
                    c = d.message.content
                    for (j = 0; j < c.length; j++) {
                        if (c[j].type !== "tool_result") continue
                        var t = typeof c[j].content === "string" ? c[j].content
                              : (Array.isArray(c[j].content) ? c[j].content.map(function(x) { return x.text || "" }).join("\n") : "")
                        add(e, { kind: "output", text: t, error: !!c[j].is_error })
                    }
                } else if (d.type === "control_request" && d.request && d.request.subtype === "can_use_tool") {
                    var ans = answers[d.request_id] || (lastStop > e.seq ? "the session stopped before it was answered" : "")
                    if (d.request.tool_name === "AskUserQuestion") {
                        add(e, { kind: "question", id: d.request_id, tool: d.request.tool_name, text: summarise(d.request.input),
                                 qjson: JSON.stringify((d.request.input && d.request.input.questions) || []),
                                 open: ans === "", answer: ans })
                    } else
                    add(e, { kind: "prompt", id: d.request_id, tool: d.request.tool_name,
                             text: summarise(d.request.input), description: d.request.description || "",
                             open: ans === "", answer: ans })
                } else if (d.type === "result") {
                    var note = d.subtype === "success" ? "done" : String(d.subtype).replace(/_/g, " ")
                    if (d.total_cost_usd !== undefined) note += "  ·  $" + Number(d.total_cost_usd).toFixed(3)
                    add(e, { kind: "note", text: note })
                }
            }
        }
        return out
    }

    // Updates the model in place where it can: rows are only ever appended or
    // changed (a prompt being answered), so the view keeps its place. History
    // arriving after the events is the one case that rebuilds it.
    function rebuildChat() {
        var items = chatItems()
        var i = 0
        for (; i < chatModel.count && i < items.length; i++) {
            if (chatModel.get(i).key !== items[i].key) break
            var same = chatModel.get(i).blob === JSON.stringify(items[i])
            if (!same) chatModel.set(i, row(items[i]))
        }
        if (i < chatModel.count) {
            chatModel.clear()
            i = 0
        }
        for (; i < items.length; i++) chatModel.append(row(items[i]))
        if (root.jumpTo > 0) {
            for (i = 0; i < chatModel.count; i++) {
                if (chatModel.get(i).earlier || chatModel.get(i).seq !== root.jumpTo) continue
                // The one scroll done in code that is not following the end:
                // somebody asked for this message.
                var at = i
                root.chatStick = false
                root.agentLit = root.jumpTo
                root.jumpTo = 0
                Qt.callLater(function() { chatList.positionViewAtIndex(at, ListView.Center) })
                litTimer.restart()
                return
            }
        }
        if (root.chatStick) Qt.callLater(function() { chatList.positionViewAtEnd() })
    }

    // Search: the whole conversation, on the agent's machine; the core does it
    // in the background and pumpSearch reads the answer.
    property bool searchOpen: false
    property bool searchBusy: false
    property var searchFound: null
    property real jumpTo: 0
    property real agentLit: 0
    property var reading: null
    function runSearch(q) {
        q = String(q || "").trim()
        if (q === "" || !agentOpen) return
        var r = agentCall("agentSearch", [agentOpen.address, agentOpen.session, q])
        if (r === null) return
        root.searchBusy = true
        root.searchFound = null
    }
    function pumpSearch() {
        if (!searchBusy) return
        var r = unwrap(callCore("agentSearched", []))
        if (!r || !r.done) return
        root.searchBusy = false
        if (r.error) { root.said = "search: " + r.error; root.saidBad = true; return }
        root.searchFound = r.found || []
    }
    // What tailReaching does on the phone (AgentChat.kt).
    function tailReaching(current, lastSeq, seq) {
        return current === 0 ? 0 : Math.max(current, lastSeq - seq + 1 + 20)
    }
    function openFound(f) {
        if (!f.seq) { root.reading = f; readingDialog.open(); return }
        root.searchOpen = false
        var evs = agentEventsList
        var first = evs.length > 0 ? evs[0].seq : Infinity
        if (f.seq < first) {
            var lastSeq = Math.max(agentInfo ? (agentInfo.last_seq || 0) : 0, evs.length > 0 ? evs[evs.length - 1].seq : 0)
            var h = { address: agentOpen.address, name: agentOpen.name, mesh: agentOpen.mesh }
            openSession(h, agentOpen.session, tailReaching(agentTailNow, lastSeq, f.seq))
        }
        root.jumpTo = f.seq
        rebuildChat()
    }
    Timer { id: litTimer; interval: 4000; onTriggered: root.agentLit = 0 }
    function row(it) {
        return { key: it.key, seq: it.seq || 0, kind: it.kind, text: it.text || "", by: it.by || "", time: it.time || 0,
                 earlier: !!it.earlier, error: !!it.error, pid: it.id || "", tool: it.tool || "",
                 description: it.description || "", open: !!it.open, answer: it.answer || "", qjson: it.qjson || "",
                 blob: JSON.stringify(it) }
    }

    readonly property bool agentWorking: {
        if (agentInfo && agentInfo.state === "working") return true
        if (agentStreaming !== "") return true
        var n = agentEventsList.length
        if (n === 0) return false
        var last = agentEventsList[n - 1]
        if (last.kind === "message") return true
        return last.kind === "claude" && last.data && last.data.type !== "result" &&
               !(last.data.type === "control_request")
    }

    // A message with the files sent alongside it named at the end, by their
    // path on the agent's machine — the same words the phone uses.
    function withAttachments(text, paths) {
        if (paths.length === 0) return text
        return (text === "" ? "" : text + "\n\n") + "Attached from Basecamp (on this machine):\n"
               + paths.map(function(p) { return "- " + p }).join("\n")
    }
    function sendToAgent(text) {
        if (!agentOpen || (text.trim() === "" && agentAttached.length === 0) || agentSending !== "") return false
        var r = agentCall("agentPost", [agentOpen.address, "/v1/sessions/" + agentOpen.session + "/messages",
                                        JSON.stringify({ text: withAttachments(text.trim(), agentAttached) })])
        if (r !== null) { root.agentAttached = []; root.chatStick = true }
        return r !== null
    }
    function localPath(url) {
        var u = String(url)
        return u.indexOf("file://") === 0 ? decodeURIComponent(u.slice(7)) : u
    }
    function attachFile(url) {
        if (!agentOpen) return
        agentCall("agentUpload", [agentOpen.address, agentOpen.session, localPath(url)])
        pumpJobs()
    }
    function toggleRecording() {
        if (!agentOpen || agentTranscribing) return
        if (!agentRecording) {
            if (agentCall("agentRecord", ["start", "", "", ""]) !== null) root.agentRecording = true
        } else {
            root.agentRecording = false
            if (agentCall("agentRecord", ["stop", agentOpen.address, agentOpen.session, "auto"]) !== null)
                root.agentTranscribing = true
        }
    }
    function shortDir(d) { return String(d || "").replace(/^\/home\/[^\/]+/, "~") }
    // The coding agents a machine runs (GET /v1/harnesses), Claude Code first;
    // an agent too old to list them runs Claude Code alone.
    readonly property var claudeOnly: [ { name: "claude", title: "Claude Code", caps: { approve: true } } ]
    property var harnesses: claudeOnly
    property string nsHarness: "claude"
    function loadHarnesses(h) {
        root.harnesses = claudeOnly
        root.nsHarness = "claude"
        if (!h) return
        var r = unwrap(callCore("agentGet", [h.address, "/v1/harnesses"]))
        if (r && r.harnesses && r.harnesses.length > 0) root.harnesses = r.harnesses
    }
    function harnessApproves(name) {
        for (var i = 0; i < harnesses.length; i++) if (harnesses[i].name === name) return !!(harnesses[i].caps && harnesses[i].caps.approve)
        return false
    }
    function harnessLabel(h) { return (!h || h === "claude") ? "" : h }
    function createSession(host, name, dir, auto) {
        var r = agentCall("agentPost", [host.address, "/v1/sessions",
            JSON.stringify({ name: name, dir: dir, harness: nsHarness, auto_approve: auto && harnessApproves(nsHarness) })])
        return r !== null
    }
    function loadConversations(h) {
        root.conversations = []
        root.conversationsProblem = ""
        if (!h) return
        var r = unwrap(callCore("agentGet", [h.address, "/v1/conversations?limit=15"]))
        if (r && r.conversations) root.conversations = r.conversations
        else root.conversationsProblem = (r && (r.detail || r.error)) || "the agent does not list conversations — update shrooms-agent"
    }
    // A session name from the directory the conversation ran in, unique on
    // that machine.
    function nameFor(h, conv) {
        var base = String(conv.dir || "conversation").split("/").pop().replace(/[^a-zA-Z0-9._-]/g, "-").replace(/^[^a-zA-Z0-9]+/, "") || "conversation"
        base = base.slice(0, 40)
        var taken = {}
        for (var i = 0; i < h.sessions.length; i++) taken[h.sessions[i].name] = true
        var name = base, n = 2
        while (taken[name]) name = base + "-" + (n++)
        return name
    }
    function takeOver(h, conv) {
        var name = nameFor(h, conv)
        var r = agentCall("agentPost", [h.address, "/v1/sessions",
                          JSON.stringify({ name: name, resume: conv.id, auto_approve: nsAuto.checked })])
        if (r === null) return
        root.agentCreating = false
        refreshAgents()
        openSession(h, name)
    }
    function stopTerminal(h, pid) {
        if (agentCall("agentPost", [h.address, "/v1/terminals/" + pid + "/stop", ""]) !== null) {
            root.said = "stopped the terminal's claude"
            root.saidBad = false
            Qt.callLater(function() { root.loadConversations(h) })
        }
    }
    // Uploads and voice notes finish in the core's own time: picked up here.
    function pumpJobs() {
        var r = unwrap(callCore("agentJobs", []))
        if (!r || !r.jobs) return
        root.agentRecording = !!r.recording
        var sending = [], transcribing = false
        for (var i = 0; i < r.jobs.length; i++) {
            var j = r.jobs[i]
            if (j.state === "pending") {
                if (j.kind === "upload") sending.push(j.name)
                else transcribing = true
                continue
            }
            if (agentJobsSeen[j.id]) continue
            agentJobsSeen[j.id] = true
            if (j.state === "failed") { root.said = j.name + ": " + j.error; root.saidBad = true; continue }
            if (j.kind === "upload" && j.path) root.agentAttached = agentAttached.concat([j.path])
            if (j.kind === "voice" && j.text) composer.text = composer.text.trim() === "" ? j.text : composer.text.trim() + " " + j.text
        }
        root.agentSending = sending.join(", ")
        root.agentTranscribing = transcribing
    }
    function answerPrompt(id, allow) {
        if (!agentOpen) return
        agentCall("agentPost", [agentOpen.address, "/v1/sessions/" + agentOpen.session + "/prompts/" + id,
                                JSON.stringify({ allow: allow })])
    }
    // A question from the model (AskUserQuestion): what has been picked or
    // typed so far, per prompt and question, kept here so a row rebuilt by
    // arriving events does not lose it. The rules are the phone's
    // (AgentChat.answersFor): picks in the order offered, joined by ", ";
    // typed words in their place; nothing sent until every question has one.
    property var qPicked: ({})
    property var qTyped: ({})
    function pickOption(pid, question, label, multi) {
        var all = Object.assign({}, qPicked)
        var mine = Object.assign({}, all[pid] || {})
        var cur = (mine[question] || []).slice()
        var at = cur.indexOf(label)
        if (multi) { if (at >= 0) cur.splice(at, 1); else cur.push(label) }
        else cur = [label]
        mine[question] = cur
        all[pid] = mine
        root.qPicked = all
        typeAnswer(pid, question, "")
    }
    function typeAnswer(pid, question, text) {
        var all = Object.assign({}, qTyped)
        var mine = Object.assign({}, all[pid] || {})
        mine[question] = text
        all[pid] = mine
        root.qTyped = all
    }
    function isPicked(pid, question, label) {
        var p = (qPicked[pid] || {})[question] || []
        return p.indexOf(label) >= 0
    }
    function questionAnswers(pid, questions) {
        var out = {}
        for (var i = 0; i < questions.length; i++) {
            var q = questions[i]
            var t = String((qTyped[pid] || {})[q.question] || "").trim()
            var picked = (qPicked[pid] || {})[q.question] || []
            var p = (q.options || []).map(function(o) { return o.label }).filter(function(l) { return picked.indexOf(l) >= 0 })
            if (t !== "") out[q.question] = t
            else if (p.length > 0) out[q.question] = p.join(", ")
            else return null
        }
        return out
    }
    function answerQuestion(pid, questions) {
        var a = questionAnswers(pid, questions)
        if (!a || !agentOpen) return false
        return agentCall("agentPost", [agentOpen.address, "/v1/sessions/" + agentOpen.session + "/prompts/" + pid,
                                       JSON.stringify({ allow: true, answers: a })]) !== null
    }

    // Starred sessions: first, from every machine, by name (the phone's
    // starredFirst). The star is kept on the agent, so the phone shows it too.
    readonly property var starredSessions: {
        var out = []
        for (var i = 0; i < agentHosts.length; i++) {
            var ss = agentHosts[i].sessions || []
            for (var j = 0; j < ss.length; j++) if (ss[j].starred) out.push({ host: agentHosts[i], sess: ss[j] })
        }
        out.sort(function(a, b) { return a.sess.name === b.sess.name ? (a.host.name < b.host.name ? -1 : 1) : (a.sess.name < b.sess.name ? -1 : 1) })
        return out
    }
    function unstarred(h) { return (h.sessions || []).filter(function(x) { return !x.starred }) }
    function setStarred(h, name, on) {
        // Shown at once; the agent's answer is the next refresh's.
        var hs = agentHosts.map(function(x) {
            if (x.address !== h.address) return x
            var c = Object.assign({}, x)
            c.sessions = (x.sessions || []).map(function(y) { return y.name === name ? Object.assign({}, y, { starred: on }) : y })
            return c
        })
        root.agentHosts = hs
        agentCall("agentPost", [h.address, "/v1/sessions/" + name + "/settings", JSON.stringify({ starred: on })])
    }

    function setAutoApprove(on) {
        if (!agentOpen) return
        if (agentCall("agentPost", [agentOpen.address, "/v1/sessions/" + agentOpen.session + "/settings",
                                    JSON.stringify({ auto_approve: on })]) !== null) refreshAgents()
    }

    ListModel { id: chatModel }

    // What deleting a session in this state does, in words: the same as the
    // phone's (Agents.kt, deleteSessionText).
    function deleteSessionText(state) {
        var t = "This stops the session and removes it from the list. The Claude Code conversation itself is kept on that machine, and can be continued again from \"+ session\"."
        if (state === "working") t += "\n\nIt is working right now: that turn will be cut off."
        if (state === "waiting") t += "\n\nIt is waiting for an answer to a permission prompt, which will be dropped."
        return t
    }
    function askDelete() { deleteDialog.open() }
    function deleteDialogOpen() { return deleteDialog.visible }
    function deleteOpenSession() {
        if (!agentOpen) return false
        var r = agentCall("agentDelete", [agentOpen.address, "/v1/sessions/" + agentOpen.session])
        if (r === null) return false
        root.said = "deleted session " + agentOpen.session
        root.saidBad = false
        root.agentOpen = null
        chatModel.clear()
        refreshAgents()
        return true
    }

    // Asks before deleting, and says what is lost and what is not.
    Dialog {
        id: deleteDialog
        modal: true
        anchors.centerIn: parent
        width: Math.min(root.sz(460), root.width - root.sz(40))
        padding: root.sz(20)
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
        Overlay.modal: Rectangle { color: Qt.rgba(0, 0, 0, 0.6) }
        background: Rectangle { color: cPanel; radius: root.sz(12); border.color: cRust }
        header: Item {}
        footer: Item {}
        contentItem: ColumnLayout {
            spacing: root.sz(14)
            Text {
                Layout.fillWidth: true; wrapMode: Text.Wrap
                text: root.agentOpen ? "Delete session \"" + root.agentOpen.session + "\" on " + root.agentOpen.name + "?" : ""
                color: cBone; font.family: "monospace"; font.pixelSize: root.fs(14)
            }
            Text {
                Layout.fillWidth: true; wrapMode: Text.Wrap
                text: root.deleteSessionText(root.agentInfo ? root.agentInfo.state : "idle")
                color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(11)
            }
            RowLayout {
                Layout.alignment: Qt.AlignRight
                spacing: root.sz(20)
                Lnk { text: "CANCEL"; base: cBone; font.pixelSize: root.fs(12); onClicked: deleteDialog.close() }
                Lnk { text: "DELETE"; base: cRust; font.pixelSize: root.fs(12)
                      onClicked: if (root.deleteOpenSession()) deleteDialog.close() }
            }
        }
    }
    // A search result from before this agent had the conversation: no event to
    // jump to, so it is shown whole.
    Dialog {
        id: readingDialog
        modal: true
        anchors.centerIn: parent
        width: Math.min(root.sz(640), root.width - root.sz(40))
        height: Math.min(root.sz(520), root.height - root.sz(40))
        padding: root.sz(20)
        closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
        Overlay.modal: Rectangle { color: Qt.rgba(0, 0, 0, 0.6) }
        background: Rectangle { color: cPanel; radius: root.sz(12); border.color: cSky }
        header: Item {}
        footer: Item {}
        contentItem: ColumnLayout {
            spacing: root.sz(10)
            RowLayout {
                Text {
                    Layout.fillWidth: true
                    text: root.reading ? [root.reading.role === "user" ? "YOU" : "CLAUDE", root.clock(root.epoch(root.reading.time)), "before this agent"].join("  ·  ") : ""
                    color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10); font.letterSpacing: 1
                }
                Lnk { text: "copy"; base: cAsh; font.pixelSize: root.fs(10); onClicked: root.copyText(root.reading ? root.reading.text : "") }
                Lnk { text: "close"; base: cBone; font.pixelSize: root.fs(10); onClicked: readingDialog.close() }
            }
            ScrollView {
                Layout.fillWidth: true; Layout.fillHeight: true
                clip: true
                TextEdit {
                    width: readingDialog.availableWidth
                    readOnly: true; selectByMouse: true; wrapMode: TextEdit.Wrap
                    textFormat: root.reading && root.reading.role !== "user" ? TextEdit.MarkdownText : TextEdit.RichText
                    text: !root.reading ? "" : root.reading.role !== "user" ? root.linkMarkdown(root.reading.text) : root.linkPlain(root.reading.text)
                    onLinkActivated: function(link) { root.openUrl(link) }
                    color: cBone; font.family: "monospace"; font.pixelSize: root.fs(12)
                }
            }
        }
    }
    function readingOpen() { return readingDialog.visible }

    // For the harness, which cannot reach an id inside this component.
    function chatModelCount() { return chatModel.count }
    function chatModelAt(i) { return chatModel.get(i) }
    function composerText() { return composer.text }
    function nsAutoChecked() { return nsAuto.checked }

    Timer {
        // Finding agents: cheap, since the core probes in the background and
        // this returns at once what it has.
        interval: 3000
        running: root.agentsOpen && root.haveCore
        repeat: true; triggeredOnStart: true
        onTriggered: root.refreshAgents()
    }
    Timer {
        // The open session: collected by the core, read here several times a
        // second so a streamed reply grows smoothly.
        interval: 300
        running: root.agentsOpen && root.agentOpen !== null && root.haveCore
        repeat: true
        onTriggered: { root.pumpAgent(); root.pumpJobs(); root.pumpSearch() }
    }

    // One session in the list: in its machine's group, or among the starred
    // ones, where the machine is named.
    component SessionCard: Rectangle {
        id: srow
        required property var host
        required property var sess
        property bool showHost: false
        readonly property bool up: root.hostReachable(srow.host)
        opacity: up ? 1 : 0.5

        readonly property bool isOpen: root.agentOpen !== null && root.agentOpen.address === srow.host.address && root.agentOpen.session === srow.sess.name
        height: sCol.implicitHeight + root.sz(16)
        radius: root.sz(8)
        color: isOpen ? Qt.rgba(0.21, 0.94, 0.63, 0.08) : cPanel
        border.width: 1
        border.color: srow.up && srow.sess.state === "waiting" ? cAmber : (isOpen ? cPhosphor : cLine)
        Column {
            id: sCol
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
            anchors.margins: root.sz(8)
            spacing: 3
            RowLayout {
                width: parent.width
                Text { text: srow.sess.name; color: cBone; font.family: "monospace"; font.pixelSize: root.fs(12); elide: Text.ElideRight; Layout.fillWidth: !srow.showHost }
                Text { visible: srow.showHost; text: srow.host.name; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10); Layout.fillWidth: true }
                Pulse { visible: srow.up && srow.sess.state !== "idle"; tint: srow.sess.state === "waiting" ? cAmber : cPhosphor }
                Text {
                    Layout.rightMargin: root.sz(22)
                    text: !srow.up ? "unreachable" : srow.sess.state === "waiting" ? "NEEDS YOU" : (srow.sess.state === "working" ? "WORKING" : (srow.sess.running ? "idle" : "asleep"))
                    color: !srow.up ? cAsh : srow.sess.state === "waiting" ? cAmber : (srow.sess.state === "working" ? cPhosphor : cAsh)
                    font.family: "monospace"; font.pixelSize: root.fs(9); font.letterSpacing: 1
                }
            }
            Text {
                visible: (srow.sess.preview || "") !== ""
                width: parent.width
                text: srow.sess.preview || ""
                color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10)
                wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight
            }
            Text {
                width: parent.width
                text: [root.clock(root.epoch(srow.sess.last_time)),
                       root.contextLabel(srow.sess.context_used, srow.sess.context_window),
                       root.harnessLabel(srow.sess.harness), root.shortModel(srow.sess.model),
                       srow.sess.auto_approve && !(srow.sess.caps && !srow.sess.caps.approve) ? "auto-approve" : ""].filter(function(x) { return x !== "" }).join("  ·  ")
                color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(9); elide: Text.ElideRight
            }
        }
        MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.openSession(srow.host, srow.sess.name) }
        // Starred: listed first. Faint until it is. Above the row's own area.
        Text {
            anchors.right: parent.right; anchors.top: parent.top; anchors.margins: root.sz(6)
            z: 2
            text: "🍄"; font.pixelSize: root.fs(13)
            opacity: srow.sess.starred ? 1 : (starMouse.containsMouse ? 0.6 : 0.25)
            MouseArea { id: starMouse; anchors.fill: parent; anchors.margins: -4; hoverEnabled: true; cursorShape: Qt.PointingHandCursor
                        onClicked: root.setStarred(srow.host, srow.sess.name, !srow.sess.starred) }
        }
    }

    component Lnk: Text {
        id: lnk
        signal clicked()
        property color base: cPhosphor
        color: lnkMouse.containsMouse ? cBone : base
        font.family: "monospace"; font.pixelSize: root.fs(11)
        font.underline: lnkMouse.containsMouse
        MouseArea { id: lnkMouse; anchors.fill: parent; hoverEnabled: true; cursorShape: Qt.PointingHandCursor; onClicked: lnk.clicked() }
    }

    component Pulse: Rectangle {
        property color tint: cPhosphor
        width: root.sz(8); height: width; radius: width / 2
        color: tint
        SequentialAnimation on opacity {
            loops: Animation.Infinite
            NumberAnimation { from: 1; to: 0.25; duration: 900 }
            NumberAnimation { from: 0.25; to: 1; duration: 900 }
        }
    }

    Rectangle {
        id: agentsPanel
        anchors.fill: parent
        visible: root.agentsOpen
        z: 50
        color: cVoid


        RowLayout {
            anchors.fill: parent
            anchors.margins: root.sz(20)
            spacing: root.sz(18)

            // --- machines and their sessions ---------------------------------
            ColumnLayout {
                Layout.preferredWidth: root.sz(320)
                Layout.maximumWidth: root.sz(380)
                Layout.fillHeight: true
                spacing: root.sz(10)

                RowLayout {
                    spacing: 8
                    Pulse {}
                    Text { text: "AGENTS"; color: cPhosphor; font.family: "monospace"; font.pixelSize: root.fs(12); font.letterSpacing: 1.5 }
                    Item { Layout.fillWidth: true }
                }
                Text {
                    Layout.fillWidth: true
                    visible: !root.haveCore
                    wrapMode: Text.Wrap
                    text: "Agents need shrooms_core, which runs inside Basecamp."
                    color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(11)
                }
                Text {
                    Layout.fillWidth: true
                    visible: root.haveCore && root.agentHosts.length === 0
                    wrapMode: Text.Wrap
                    text: "Looking for agents among the reachable peers… An agent is shrooms-agent on one of your machines, on its mesh address, port 7387."
                    color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(11)
                }

                ListView {
                    id: hostList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    spacing: root.sz(6)
                    model: root.agentHosts
                    header: Column {
                        width: hostList.width
                        spacing: root.sz(6)
                        bottomPadding: root.starredSessions.length > 0 ? root.sz(10) : 0
                        Text {
                            visible: root.starredSessions.length > 0
                            text: "🍄  STARRED"; color: cPhosphor; font.family: "monospace"; font.pixelSize: root.fs(11); font.letterSpacing: 1.5
                        }
                        Repeater {
                            model: root.starredSessions
                            delegate: SessionCard {
                                required property var modelData
                                width: hostList.width
                                host: modelData.host
                                sess: modelData.sess
                                showHost: true
                            }
                        }
                    }
                    delegate: Column {
                        id: hostCol
                        required property var modelData
                        width: hostList.width
                        spacing: root.sz(6)
                        readonly property bool up: root.hostReachable(hostCol.modelData)
                        RowLayout {
                            width: parent.width
                            spacing: 8
                            opacity: hostCol.up ? 1 : 0.45
                            Rectangle { width: root.sz(9); height: width; radius: width / 2; color: "transparent"; border.width: 2; border.color: root.meshTint(hostCol.modelData.mesh) }
                            Text { text: hostCol.modelData.name; color: cBone; font.family: "monospace"; font.pixelSize: root.fs(14) }
                            Text { text: hostCol.modelData.mesh; color: root.meshTint(hostCol.modelData.mesh); font.family: "monospace"; font.pixelSize: root.fs(10) }
                            Text { visible: !hostCol.up; text: "unreachable · seen " + root.clock(hostCol.modelData.lastSeen); color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10) }
                            Item { Layout.fillWidth: true }
                            Lnk { visible: hostCol.up; text: "+ session"; onClicked: {
                                newSession.host = hostCol.modelData
                                root.agentCreating = true
                                Qt.callLater(function() { root.loadHarnesses(hostCol.modelData); root.loadConversations(hostCol.modelData) })
                            } }
                        }
                        Text {
                            visible: hostCol.modelData.sessions.length === 0
                            text: "no sessions yet"; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10)
                        }
                        Text {
                            visible: hostCol.modelData.sessions.length > 0 && root.unstarred(hostCol.modelData).length === 0
                            text: "all starred"; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10)
                        }
                        Repeater {
                            model: root.unstarred(hostCol.modelData)
                            delegate: SessionCard {
                                required property var modelData
                                width: hostCol.width
                                host: hostCol.modelData
                                sess: modelData
                            }
                        }
                    }
                }
            }

            Rectangle { Layout.fillHeight: true; width: 1; color: cLine }

            // --- the conversation -------------------------------------------
            ColumnLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                spacing: root.sz(8)

                // New session form, in place of the conversation.
                ColumnLayout {
                    id: newSession
                    property var host: null
                    visible: root.agentCreating
                    Layout.fillWidth: true
                    spacing: root.sz(8)
                    Text { text: "NEW SESSION ON " + (newSession.host ? newSession.host.name.toUpperCase() : ""); color: cPhosphor; font.family: "monospace"; font.pixelSize: root.fs(12); font.letterSpacing: 1.5 }
                    Text { text: "A name and a directory on that machine, like a cl session. ~ is that machine's home."; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10) }
                    TextField { id: nsName; Layout.fillWidth: true; placeholderTextColor: cAsh; placeholderText: "name"; color: cBone; font.family: "monospace"; background: Rectangle { color: cPanel; border.color: nsName.activeFocus ? cPhosphor : cLine; radius: 6 } }
                    TextField { id: nsDir; Layout.fillWidth: true; placeholderTextColor: cAsh; text: "~/"; placeholderText: "directory"; color: cBone; font.family: "monospace"; background: Rectangle { color: cPanel; border.color: nsDir.activeFocus ? cPhosphor : cLine; radius: 6 } }
                    // Which coding agent, when the machine has more than Claude Code.
                    Row {
                        visible: root.harnesses.length > 1
                        spacing: root.sz(8)
                        Repeater {
                            model: root.harnesses
                            delegate: Rectangle {
                                id: hchip
                                required property var modelData
                                readonly property bool on: root.nsHarness === hchip.modelData.name
                                width: hText.implicitWidth + root.sz(20); height: hText.implicitHeight + root.sz(10)
                                radius: root.sz(8)
                                color: on ? cPhosphor : "transparent"; border.color: on ? cPhosphor : cLine
                                Text { id: hText; anchors.centerIn: parent; text: hchip.modelData.title
                                       color: hchip.on ? cVoid : cBone; font.family: "monospace"; font.pixelSize: root.fs(11) }
                                MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.nsHarness = hchip.modelData.name }
                            }
                        }
                    }
                    CheckBox { id: nsAuto; visible: root.harnessApproves(root.nsHarness); text: "auto-approve — never ask, like --dangerously-skip-permissions"; contentItem: Text { leftPadding: nsAuto.indicator.width + 6; text: nsAuto.text; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10); verticalAlignment: Text.AlignVCenter } }
                    RowLayout {
                        Lnk {
                            text: "create"
                            onClicked: {
                                if (root.createSession(newSession.host, nsName.text.trim(), nsDir.text.trim(), nsAuto.checked)) {
                                    root.agentCreating = false
                                    root.refreshAgents()
                                    root.openSession(newSession.host, nsName.text.trim())
                                    nsName.text = ""
                                }
                            }
                        }
                        Lnk { text: "cancel"; base: cAsh; onClicked: root.agentCreating = false }
                    }

                    // Or carry on one that started somewhere else — in a
                    // terminal, under cl. Resuming keeps writing to the same
                    // conversation, so this continues it rather than copying it.
                    Text {
                        visible: root.nsHarness === "claude"
                        Layout.topMargin: root.sz(14)
                        text: "OR CONTINUE A CONVERSATION FROM " + (newSession.host ? newSession.host.name.toUpperCase() : "")
                        color: cPhosphor; font.family: "monospace"; font.pixelSize: root.fs(12); font.letterSpacing: 1.5
                    }
                    Text {
                        visible: root.nsHarness === "claude"
                        Layout.fillWidth: true; wrapMode: Text.Wrap
                        text: "Newest first. A terminal still open in the same directory may hold it: stop it before carrying on here, or the two will write over each other's turns."
                        color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10)
                    }
                    Text {
                        visible: root.nsHarness === "claude" && root.conversations.length === 0
                        text: root.conversationsProblem !== "" ? root.conversationsProblem : "looking…"
                        color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10)
                    }
                    ListView {
                        id: convList
                        visible: root.nsHarness === "claude"
                        Layout.fillWidth: true
                        Layout.preferredHeight: Math.min(contentHeight, root.sz(420))
                        clip: true
                        spacing: root.sz(6)
                        model: root.conversations
                        delegate: Rectangle {
                            id: conv
                            required property var modelData
                            readonly property bool taken: (conv.modelData.adopted_by || "") !== ""
                            width: convList.width
                            height: convCol.implicitHeight + root.sz(16)
                            radius: root.sz(8)
                            color: cPanel
                            border.color: convMouse.containsMouse && !taken ? cPhosphor : cLine
                            opacity: taken ? 0.6 : 1
                            MouseArea {
                                id: convMouse
                                anchors.fill: parent; hoverEnabled: true
                                cursorShape: conv.taken ? Qt.ArrowCursor : Qt.PointingHandCursor
                                onClicked: if (!conv.taken) root.takeOver(newSession.host, conv.modelData)
                            }
                            Column {
                                id: convCol
                                x: root.sz(10); y: root.sz(8); width: parent.width - root.sz(20)
                                spacing: 3
                                RowLayout {
                                    width: parent.width
                                    Text { text: root.shortDir(conv.modelData.dir || "?"); color: cBone; font.family: "monospace"; font.pixelSize: root.fs(12); elide: Text.ElideMiddle; Layout.fillWidth: true }
                                    Text { text: root.clock(root.epoch(conv.modelData.modified)); color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(9) }
                                }
                                Text { visible: (conv.modelData.last_user || "") !== ""; width: parent.width; elide: Text.ElideRight
                                       text: "you: " + (conv.modelData.last_user || ""); color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10) }
                                Text { visible: (conv.modelData.last_assistant || "") !== ""; width: parent.width; elide: Text.ElideRight
                                       text: "claude: " + (conv.modelData.last_assistant || ""); color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10) }
                                Text { visible: conv.taken; text: "continued here as \"" + (conv.modelData.adopted_by || "") + "\""; color: cPhosphor; font.family: "monospace"; font.pixelSize: root.fs(10) }
                                Repeater {
                                    model: conv.modelData.terminals || []
                                    delegate: RowLayout {
                                        id: term
                                        required property var modelData
                                        spacing: 8
                                        Pulse { tint: cAmber }
                                        Text { text: "open in a terminal: " + (term.modelData.tmux ? "tmux " + term.modelData.tmux : "pid " + term.modelData.pid)
                                               color: cAmber; font.family: "monospace"; font.pixelSize: root.fs(10) }
                                        Lnk { text: "stop it"; base: cRust; font.pixelSize: root.fs(10)
                                              onClicked: root.stopTerminal(newSession.host, term.modelData.pid) }
                                    }
                                }
                            }
                        }
                    }
                }

                Text {
                    visible: !root.agentCreating && root.agentOpen === null
                    text: "Pick a session on the left."
                    color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(11)
                }

                // Header: the session, its facts, its switches.
                ColumnLayout {
                    visible: !root.agentCreating && root.agentOpen !== null
                    Layout.fillWidth: true
                    spacing: 4
                    RowLayout {
                        spacing: 8
                        Text { text: root.agentOpen ? root.agentOpen.session : ""; color: cBone; font.family: "monospace"; font.pixelSize: root.fs(16) }
                        Pulse { visible: root.agentWorking; }
                        Item { Layout.fillWidth: true }
                        Lnk {
                            // A harness that never asks has nothing to approve.
                            visible: !(root.agentInfo && root.agentInfo.caps && !root.agentInfo.caps.approve)
                            readonly property bool on: root.agentInfo !== null && !!root.agentInfo.auto_approve
                            text: on ? "AUTO-APPROVE ON" : "asks first"
                            base: on ? cPhosphor : cAsh
                            onClicked: root.setAutoApprove(!on)
                        }
                        Lnk { text: root.searchOpen ? "close search" : "search"; base: cSky
                              onClicked: { root.searchOpen = !root.searchOpen; if (root.searchOpen) searchField.forceActiveFocus() } }
                        Lnk { text: "delete"; base: cAsh; onClicked: root.askDelete() }
                        Lnk { visible: root.agentWorking; text: "stop"; base: cRust
                              onClicked: root.agentCall("agentPost", [root.agentOpen.address, "/v1/sessions/" + root.agentOpen.session + "/interrupt", ""]) }
                    }
                    Text {
                        text: root.agentOpen ? [root.agentOpen.name, root.agentOpen.mesh,
                              root.agentInfo ? root.harnessLabel(root.agentInfo.harness) : "",
                              root.agentInfo ? root.shortModel(root.agentInfo.model) : "",
                              root.agentInfo ? root.contextLabel(root.agentInfo.context_used, root.agentInfo.context_window) : "",
                              root.agentConnected ? "" : ("reconnecting" + (root.agentProblem ? " — " + root.agentProblem : ""))
                             ].filter(function(x) { return x !== "" }).join("  ·  ") : ""
                        color: root.agentConnected ? cAsh : cAmber
                        font.family: "monospace"; font.pixelSize: root.fs(10)
                    }
                    Rectangle {
                        visible: root.agentInfo !== null && root.agentInfo.context_window > 0
                        Layout.fillWidth: true; height: 2; color: cLine
                        Rectangle {
                            readonly property real f: root.agentInfo && root.agentInfo.context_window ? Math.min(1, root.agentInfo.context_used / root.agentInfo.context_window) : 0
                            width: parent.width * f; height: 2
                            color: f >= 0.9 ? cRust : (f >= 0.7 ? cAmber : cPhosphor)
                        }
                    }
                }

                ColumnLayout {
                    visible: !root.agentCreating && root.agentOpen !== null && root.searchOpen
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    spacing: root.sz(8)
                    TextField {
                        id: searchField
                        Layout.fillWidth: true
                        placeholderText: "search the whole conversation — Enter"
                        placeholderTextColor: cAsh; color: cBone; font.family: "monospace"
                        background: Rectangle { color: cPanel; border.color: searchField.activeFocus ? cSky : cLine; radius: 6 }
                        onAccepted: root.runSearch(text)
                    }
                    RowLayout {
                        spacing: 8
                        Pulse { visible: root.searchBusy; tint: cSky }
                        Text {
                            text: root.searchBusy ? "searching…"
                                : root.searchFound === null ? "Words anywhere in what was typed or answered — also before this agent had it. Case and accents do not matter."
                                : root.searchFound.length === 0 ? "Nothing found."
                                : root.searchFound.length >= 100 ? "the newest 100" : root.searchFound.length + " found"
                            color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10)
                            Layout.fillWidth: true; wrapMode: Text.Wrap
                        }
                    }
                    ListView {
                        id: foundList
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        clip: true
                        spacing: root.sz(8)
                        model: root.searchFound || []
                        ScrollBar.vertical: ScrollBar {}
                        delegate: Rectangle {
                            id: frow
                            required property var modelData
                            width: foundList.width
                            height: frowCol.implicitHeight + root.sz(16)
                            radius: root.sz(8)
                            color: frowMouse.containsMouse ? cPanel : "transparent"
                            border.color: cLine
                            Column {
                                id: frowCol
                                x: root.sz(8); y: root.sz(8); width: parent.width - root.sz(16)
                                spacing: 4
                                Text {
                                    text: [frow.modelData.role === "user" ? "YOU" : "CLAUDE", root.clock(root.epoch(frow.modelData.time)),
                                           frow.modelData.seq ? "" : "before this agent"].filter(function(x) { return x !== "" }).join("  ·  ")
                                    color: frow.modelData.role === "user" ? cPhosphor : cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(9); font.letterSpacing: 1
                                }
                                Text {
                                    width: parent.width
                                    text: frow.modelData.snippet
                                    color: cBone; font.family: "monospace"; font.pixelSize: root.fs(11)
                                    wrapMode: Text.Wrap; maximumLineCount: 4; elide: Text.ElideRight
                                }
                            }
                            MouseArea { id: frowMouse; anchors.fill: parent; hoverEnabled: true; cursorShape: Qt.PointingHandCursor
                                        onClicked: root.openFound(frow.modelData) }
                        }
                    }
                }

                ListView {
                    id: chatList
                    visible: !root.agentCreating && root.agentOpen !== null && !root.searchOpen
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    spacing: root.sz(10)
                    model: chatModel
                    ScrollBar.vertical: ScrollBar {
                        onPressedChanged: if (!pressed) root.chatStick = chatList.atYEnd
                    }
                    // Messages measure themselves after they are added, so
                    // the height keeps growing after a scroll to the end: it
                    // is followed for as long as the reader is down there.
                    onContentHeightChanged: if (root.chatStick) Qt.callLater(chatList.positionViewAtEnd)
                    onMovementEnded: root.chatStick = chatList.atYEnd
                    onAtYEndChanged: if (atYEnd) root.chatStick = true
                    // Scrolling up by any means — the wheel included, which
                    // reports no movement — stops the following. Content
                    // growing never moves the view up, so this is the reader.
                    property real lastY: 0
                    onContentYChanged: {
                        if (contentY < lastY - 2 && !atYEnd) root.chatStick = false
                        lastY = contentY
                    }

                    // Files dropped on the conversation are sent like 📎 ones.
                    DropArea {
                        anchors.fill: parent
                        onDropped: function(drop) {
                            if (!drop.hasUrls) return
                            for (var i = 0; i < drop.urls.length; i++) root.attachFile(drop.urls[i])
                            drop.accept()
                        }
                    }

                    Rectangle {
                        visible: !chatList.atYEnd && chatModel.count > 0
                        anchors.right: parent.right; anchors.bottom: parent.bottom; anchors.margins: root.sz(12)
                        width: root.sz(34); height: width; radius: width / 2
                        color: cPanel; border.color: cPhosphor
                        z: 5
                        Text { anchors.centerIn: parent; text: "↓"; color: cPhosphor; font.pixelSize: root.fs(16) }
                        MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor
                                    onClicked: { root.chatStick = true; chatList.positionViewAtEnd() } }
                    }
                    header: Item {
                        readonly property int firstSeq: root.agentEventsList.length > 0 ? root.agentEventsList[0].seq : 0
                        width: chatList.width
                        height: visible ? root.sz(30) : 0
                        visible: root.agentTailNow !== 0 && firstSeq > 1
                        Lnk {
                            anchors.centerIn: parent
                            text: "— " + (parent.firstSeq - 1) + " earlier events not loaded · load them —"
                            font.pixelSize: root.fs(10)
                            onClicked: {
                                var h = { address: root.agentOpen.address, name: root.agentOpen.name, mesh: root.agentOpen.mesh }
                                root.openSession(h, root.agentOpen.session, 0)
                            }
                        }
                    }
                    footer: Item {
                        width: chatList.width
                        height: liveCol.implicitHeight + root.sz(8)
                        Column {
                            id: liveCol
                            width: parent.width
                            topPadding: root.sz(8)
                            Rectangle {
                                visible: root.agentStreaming !== ""
                                width: parent.width
                                height: streamText.implicitHeight + root.sz(20)
                                color: cPanel; radius: root.sz(10); border.color: cLine
                                TextEdit {
                                    id: streamText
                                    x: root.sz(10); y: root.sz(10); width: parent.width - root.sz(20)
                                    readOnly: true; selectByMouse: true; wrapMode: TextEdit.Wrap
                                    textFormat: TextEdit.MarkdownText
                                    text: root.linkMarkdown(root.agentStreaming) + " ▍"
                                    onLinkActivated: function(link) { root.openUrl(link) }
                                    color: cBone; font.family: "monospace"; font.pixelSize: root.fs(12)
                                }
                            }
                            RowLayout {
                                visible: root.agentStreaming === "" && root.agentWorking
                                spacing: 8
                                Pulse {}
                                Text { text: "thinking…"; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10) }
                            }
                        }
                    }
                    delegate: Item {
                        id: crow
                        required property int index
                        required property real seq
                        required property string kind
                        required property string text
                        required property string by
                        required property real time
                        required property bool earlier
                        required property bool error
                        required property string pid
                        required property string tool
                        required property string description
                        required property bool open
                        required property string answer
                        required property string qjson
                        width: chatList.width
                        height: crowCol.implicitHeight

                        Column {
                            id: crowCol
                            width: parent.width
                            spacing: 4

                            Text {
                                visible: crow.earlier && crow.index === 0
                                text: "— earlier, from the transcript —"
                                color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10)
                            }
                            Text {
                                visible: !crow.earlier && crow.index > 0 && chatModel.get(crow.index - 1).earlier
                                text: "— with the agent —"
                                color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10)
                            }

                            // You, or the model: a bubble with its time and a copy link.
                            Rectangle {
                                visible: crow.kind === "you" || crow.kind === "said"
                                width: parent.width
                                height: bubbleCol.implicitHeight + root.sz(20)
                                radius: root.sz(10)
                                color: crow.kind === "you" ? Qt.rgba(0.21, 0.94, 0.63, crow.earlier ? 0.04 : 0.07) : cPanel
                                border.color: crow.seq !== 0 && crow.seq === root.agentLit ? cSky
                                            : crow.kind === "you" ? Qt.rgba(0.21, 0.94, 0.63, 0.35) : cLine
                                border.width: crow.seq !== 0 && crow.seq === root.agentLit ? 2 : 1
                                opacity: crow.earlier ? 0.8 : 1
                                Column {
                                    id: bubbleCol
                                    x: root.sz(10); y: root.sz(10); width: parent.width - root.sz(20)
                                    spacing: 4
                                    RowLayout {
                                        width: parent.width
                                        Text {
                                            text: [crow.kind === "you" ? "YOU" : "", crow.by, root.clock(crow.time)].filter(function(x) { return x !== "" }).join("  ·  ")
                                            color: crow.kind === "you" ? cPhosphor : cAsh
                                            font.family: "monospace"; font.pixelSize: root.fs(9); font.letterSpacing: 1
                                            Layout.fillWidth: true
                                        }
                                        Lnk { text: "copy"; base: cAsh; font.pixelSize: root.fs(9); onClicked: root.copyText(crow.text) }
                                    }
                                    TextEdit {
                                        width: parent.width
                                        readOnly: true; selectByMouse: true; wrapMode: TextEdit.Wrap
                                        textFormat: crow.kind === "said" ? TextEdit.MarkdownText : TextEdit.RichText
                                        text: crow.kind === "said" ? root.linkMarkdown(crow.text) : root.linkPlain(crow.text)
                                        color: cBone; selectionColor: Qt.rgba(0.21, 0.94, 0.63, 0.35)
                                        font.family: "monospace"; font.pixelSize: root.fs(12)
                                        onLinkActivated: function(link) { root.openUrl(link) }
                                    }
                                }
                            }

                            // A tool it used: the first line of its command, the rest on a click —
                            // a long script filled the screen (2026-10-03).
                            Text {
                                id: toolText
                                property bool expanded: false
                                readonly property bool more: crow.text.indexOf("\n") >= 0 || crow.text.length > 160
                                visible: crow.kind === "tool"
                                width: parent.width
                                text: "▸ " + (expanded ? crow.text : crow.text.split("\n")[0].slice(0, 160) + (more ? "  … (click)" : ""))
                                color: cViolet; font.family: "monospace"; font.pixelSize: root.fs(11)
                                wrapMode: expanded ? Text.Wrap : Text.NoWrap
                                elide: expanded ? Text.ElideNone : Text.ElideRight
                                MouseArea { anchors.fill: parent; enabled: toolText.more; cursorShape: toolText.more ? Qt.PointingHandCursor : Qt.ArrowCursor
                                            onClicked: toolText.expanded = !toolText.expanded }
                            }

                            TextEdit {
                                id: outText
                                property bool expanded: false
                                visible: crow.kind === "output"
                                width: parent.width
                                leftPadding: root.sz(14)
                                readOnly: true; selectByMouse: true; wrapMode: TextEdit.Wrap
                                text: expanded ? crow.text : (crow.text.split("\n")[0].slice(0, 160) + ((crow.text.indexOf("\n") >= 0 || crow.text.length > 160) ? "  … (click)" : ""))
                                color: crow.error ? cRust : cAsh
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                MouseArea { anchors.fill: parent; acceptedButtons: Qt.LeftButton; propagateComposedEvents: true
                                            onClicked: function(m) { outText.expanded = !outText.expanded; m.accepted = false } }
                            }

                            // A permission prompt: what it would run, in full, above the buttons.
                            Rectangle {
                                visible: crow.kind === "prompt"
                                width: parent.width
                                height: promptCol.implicitHeight + root.sz(24)
                                radius: root.sz(10)
                                color: cPanel
                                border.color: crow.open ? cAmber : cLine
                                Column {
                                    id: promptCol
                                    x: root.sz(12); y: root.sz(12); width: parent.width - root.sz(24)
                                    spacing: 8
                                    RowLayout {
                                        spacing: 8
                                        Pulse { visible: crow.open; tint: cAmber }
                                        Text { text: crow.open ? (crow.tool.toUpperCase() + " WANTS TO RUN") : crow.tool
                                               color: crow.open ? cAmber : cAsh; font.family: "monospace"; font.pixelSize: root.fs(10); font.letterSpacing: 1 }
                                    }
                                    Rectangle {
                                        width: parent.width
                                        height: cmdText.implicitHeight + root.sz(16)
                                        color: cVoid; radius: 6; border.color: cLine
                                        TextEdit {
                                            id: cmdText
                                            x: root.sz(8); y: root.sz(8); width: parent.width - root.sz(16)
                                            readOnly: true; selectByMouse: true; wrapMode: TextEdit.WrapAnywhere
                                            text: crow.text; color: cChartreuse
                                            font.family: "monospace"; font.pixelSize: root.fs(11)
                                        }
                                    }
                                    Text { visible: crow.description !== "" && crow.description !== crow.text; width: parent.width; wrapMode: Text.Wrap
                                           text: crow.description; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10) }
                                    RowLayout {
                                        visible: crow.open
                                        spacing: root.sz(16)
                                        Lnk { text: "ALLOW"; font.pixelSize: root.fs(12); onClicked: root.answerPrompt(crow.pid, true) }
                                        Lnk { text: "DENY"; base: cRust; font.pixelSize: root.fs(12); onClicked: root.answerPrompt(crow.pid, false) }
                                    }
                                    Text { visible: !crow.open; text: crow.answer; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10) }
                                }
                            }

                            // The model asking something: its questions, the options
                            // to pick, or your own words, then sent together.
                            Rectangle {
                                id: qcard
                                visible: crow.kind === "question"
                                readonly property var questions: crow.kind === "question" && crow.qjson ? JSON.parse(crow.qjson) : []
                                width: parent.width
                                height: visible ? qCol.implicitHeight + root.sz(24) : 0
                                radius: root.sz(10)
                                color: cPanel
                                border.color: crow.open ? cSky : cLine
                                Column {
                                    id: qCol
                                    x: root.sz(12); y: root.sz(12); width: parent.width - root.sz(24)
                                    spacing: 10
                                    RowLayout {
                                        spacing: 8
                                        Pulse { visible: crow.open; tint: cSky }
                                        Text { text: crow.open ? "CLAUDE ASKS" : "CLAUDE ASKED"
                                               color: crow.open ? cSky : cAsh; font.family: "monospace"; font.pixelSize: root.fs(10); font.letterSpacing: 1 }
                                    }
                                    Repeater {
                                        model: qcard.questions
                                        delegate: Column {
                                            id: qq
                                            required property var modelData
                                            width: qCol.width
                                            spacing: 6
                                            Text { visible: !!qq.modelData.header; text: String(qq.modelData.header || "").toUpperCase() + (qq.modelData.multiSelect ? "  ·  pick any" : "")
                                                   color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(9); font.letterSpacing: 1 }
                                            Text { width: parent.width; wrapMode: Text.Wrap; text: qq.modelData.question
                                                   color: cBone; font.family: "monospace"; font.pixelSize: root.fs(12) }
                                            Repeater {
                                                model: crow.open ? (qq.modelData.options || []) : []
                                                delegate: Rectangle {
                                                    id: opt
                                                    required property var modelData
                                                    readonly property bool on: root.isPicked(crow.pid, qq.modelData.question, opt.modelData.label)
                                                    width: qq.width
                                                    height: optCol.implicitHeight + root.sz(14)
                                                    radius: root.sz(8)
                                                    color: on ? Qt.rgba(0.35, 0.66, 1.0, 0.12) : (optMouse.containsMouse ? cVoid : "transparent")
                                                    border.color: on ? cSky : cLine
                                                    Column {
                                                        id: optCol
                                                        x: root.sz(10); y: root.sz(7); width: parent.width - root.sz(20)
                                                        Text { text: opt.modelData.label; color: opt.on ? cSky : cBone; font.family: "monospace"; font.pixelSize: root.fs(12) }
                                                        Text { visible: !!opt.modelData.description; width: parent.width; wrapMode: Text.Wrap
                                                               text: opt.modelData.description || ""; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10) }
                                                    }
                                                    MouseArea { id: optMouse; anchors.fill: parent; hoverEnabled: true; cursorShape: Qt.PointingHandCursor
                                                                onClicked: root.pickOption(crow.pid, qq.modelData.question, opt.modelData.label, !!qq.modelData.multiSelect) }
                                                }
                                            }
                                            TextField {
                                                id: own
                                                visible: crow.open
                                                width: qq.width
                                                placeholderText: "or in your own words"
                                                placeholderTextColor: cAsh; color: cBone; font.family: "monospace"; font.pixelSize: root.fs(11)
                                                background: Rectangle { color: cVoid; border.color: own.activeFocus ? cSky : cLine; radius: 6 }
                                                text: (root.qTyped[crow.pid] || {})[qq.modelData.question] || ""
                                                onTextEdited: {
                                                    if (text.trim() !== "") {
                                                        var all = Object.assign({}, root.qPicked), mine = Object.assign({}, all[crow.pid] || {})
                                                        delete mine[qq.modelData.question]; all[crow.pid] = mine; root.qPicked = all
                                                    }
                                                    root.typeAnswer(crow.pid, qq.modelData.question, text)
                                                }
                                            }
                                        }
                                    }
                                    RowLayout {
                                        visible: crow.open
                                        spacing: root.sz(16)
                                        Lnk { readonly property bool ready: root.questionAnswers(crow.pid, qcard.questions) !== null
                                              text: "ANSWER"; base: ready ? cSky : cAsh; font.pixelSize: root.fs(12)
                                              onClicked: if (ready) root.answerQuestion(crow.pid, qcard.questions) }
                                        Lnk { text: "DECLINE"; base: cRust; font.pixelSize: root.fs(12); onClicked: root.answerPrompt(crow.pid, false) }
                                    }
                                    Text { visible: !crow.open; width: parent.width; wrapMode: Text.Wrap; text: crow.answer; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10) }
                                }
                            }

                            Text {
                                visible: crow.kind === "note"
                                text: "— " + crow.text + (crow.time ? "  ·  " + root.clock(crow.time) : "")
                                color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10)
                            }
                        }
                    }
                }

                FileDialog {
                    id: attachDialog
                    title: "Send a file to the agent"
                    onAccepted: root.attachFile(selectedFile)
                }

                // What goes with the next message, and what is still on its way.
                Flow {
                    visible: !root.agentCreating && root.agentOpen !== null && (root.agentAttached.length > 0 || root.agentSending !== "" || root.agentTranscribing)
                    Layout.fillWidth: true
                    spacing: root.sz(8)
                    Repeater {
                        model: root.agentAttached
                        delegate: Rectangle {
                            id: chip
                            required property string modelData
                            height: chipText.implicitHeight + root.sz(10)
                            width: chipText.implicitWidth + root.sz(20)
                            radius: height / 2; color: "transparent"; border.color: cSky
                            Text {
                                id: chipText
                                anchors.centerIn: parent
                                text: "📎 " + chip.modelData.split("/").pop().replace(/^\d{8}-\d{6}-(\d+-)?/, "") + "  ×"
                                color: cSky; font.family: "monospace"; font.pixelSize: root.fs(10)
                            }
                            MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor
                                        onClicked: root.agentAttached = root.agentAttached.filter(function(x) { return x !== chip.modelData }) }
                        }
                    }
                    RowLayout {
                        visible: root.agentSending !== ""
                        Pulse { tint: cSky }
                        Text { text: "sending " + root.agentSending + "…"; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10) }
                    }
                    RowLayout {
                        visible: root.agentTranscribing
                        Pulse { tint: cSky }
                        Text { text: "transcribing on " + (root.agentOpen ? root.agentOpen.name : "") + "…"; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10) }
                    }
                }

                // Composer: Enter sends, Shift+Enter is a new line.
                RowLayout {
                    visible: !root.agentCreating && root.agentOpen !== null
                    Layout.fillWidth: true
                    spacing: root.sz(10)
                    Lnk { text: "📎"; font.pixelSize: root.fs(16); onClicked: attachDialog.open() }
                    Item {
                        width: root.sz(26); height: root.sz(26)
                        Pulse { anchors.centerIn: parent; visible: root.agentRecording; tint: cRust; width: root.sz(18) }
                        Text {
                            anchors.centerIn: parent
                            text: root.agentRecording ? "■" : "🎤"
                            color: root.agentRecording ? cBone : (root.agentTranscribing ? cAsh : cPhosphor)
                            font.pixelSize: root.fs(root.agentRecording ? 11 : 16)
                        }
                        MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.toggleRecording() }
                    }
                    ScrollView {
                        Layout.fillWidth: true
                        Layout.preferredHeight: Math.min(root.sz(140), Math.max(root.sz(40), composer.implicitHeight))
                        TextArea {
                            id: composer
                            placeholderText: "message — Enter sends, Shift+Enter for a new line"
                            placeholderTextColor: cAsh
                            wrapMode: TextArea.Wrap
                            color: cBone
                            font.family: "monospace"; font.pixelSize: root.fs(12)
                            background: Rectangle { color: cPanel; radius: root.sz(10); border.color: composer.activeFocus ? cPhosphor : cLine }
                            // An image on the clipboard goes to the agent like a
                            // file; anything else pastes as usual.
                            Keys.onPressed: function(ev) {
                                if (!ev.matches(StandardKey.Paste) || !root.agentOpen) return
                                var r = root.unwrap(root.callCore("agentPaste", [root.agentOpen.address, root.agentOpen.session]))
                                if (r && r.job) { ev.accepted = true; root.pumpJobs() }
                                else if (r && r.error) { root.said = r.detail || r.error; root.saidBad = true }
                            }
                            Keys.onReturnPressed: function(ev) {
                                if (ev.modifiers & Qt.ShiftModifier) { ev.accepted = false; return }
                                if (root.sendToAgent(composer.text)) composer.text = ""
                            }
                        }
                    }
                    Lnk {
                        text: "SEND"; font.pixelSize: root.fs(12)
                        base: (composer.text.trim() !== "" || root.agentAttached.length > 0) ? cPhosphor : cAsh
                        onClicked: if (root.sendToAgent(composer.text)) composer.text = ""
                    }
                }
            }
        }
    }
}
