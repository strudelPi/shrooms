import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Dialogs

// A monitoring view for logos-vpn.
//
// Read-only by design: the daemon owns the mesh, this only watches it. Nothing
// here can change the network, so nothing here can break it.
//
// It reads the status file the daemon writes (`status_file` in config.toml)
// rather than talking to the control socket, because QML can read a file and
// cannot open a unix socket. The file is written atomically, so a partial
// document is never observed.
//
// Deliberately plain QtQuick rather than Logos.Controls: the bundled design
// system varies between Basecamp versions — qaku's own view documents controls
// that are "not a type" on older hosts — and a monitoring view that fails to
// load tells you nothing at all. Colours mirror the Android app so the two read
// as one product.
Item {
    id: root
    anchors.fill: parent

    // --- palette (matches the Android app) -----------------------------------
    readonly property color cVoid:     "#07090B"
    readonly property color cPanel:    "#0E1216"
    readonly property color cLine:     "#1C2229"
    readonly property color cAsh:      "#6B7680"
    readonly property color cBone:     "#D6DDE3"
    readonly property color cPhosphor: "#35F0A0"   // connected, carrying traffic
    readonly property color cAmber:    "#F0B429"   // reaching, not there yet
    readonly property color cRust:     "#E05252"   // failed
    readonly property color cViolet:   "#9A7BFF"   // relayed

    // Which mesh, as a colour — deliberately none of the four above.
    //
    // Those all already mean something about whether a peer is reachable, and a
    // mesh ring in one of them around a node in another is unreadable: the
    // Android app tinted its first mesh phosphor and the rings vanished against
    // the very nodes they were meant to group. Same three tints here, in the
    // same order, so a device on two meshes looks the same on both screens.
    readonly property color cSky:        "#5AA9FF"
    readonly property color cBlossom:    "#FF6FB5"
    readonly property color cChartreuse: "#C8E64A"
    readonly property var meshTints: [cSky, cBlossom, cChartreuse, cAsh]

    property var st: ({})
    property var peers: []
    property string problem: ""

    // Two ways in, tried in order.
    //
    // The file is preferred: no port is opened and access is decided by file
    // permissions. But QML refuses file:// reads through XMLHttpRequest unless
    // the host sets QML_XHR_ALLOW_FILE_READ, which is Basecamp's environment
    // and not ours to choose — so when that is off, the daemon's loopback
    // endpoint is the only thing left. Found by running this offscreen rather
    // than by reading the documentation.
    // Where the status comes from, in order of what actually works.
    //
    // Inside Basecamp the only route is the core module. A ui_qml app is
    // sandboxed: a deny-all network manager blocks every HTTP request, and
    // XMLHttpRequest refuses local files outright unless the host sets
    // QML_XHR_ALLOW_FILE_READ, which Basecamp does not. So neither a file nor a
    // port is reachable from here however either is permissioned — the log says
    // so plainly, in two different ways, and it took three wrong guesses before
    // anyone read it.
    //
    // shrooms_core runs in its own process, unsandboxed, and reads the daemon's
    // control socket. That is what Basecamp's own spec prescribes: UI apps
    // reach the outside indirectly, through Logos Modules.
    //
    // The file and endpoint remain for running this outside Basecamp — the
    // offscreen check, or a plain qml runtime — where the sandbox does not
    // apply and the core module does not exist.
    property string statusPath: "/run/shrooms/status.json"
    property string statusUrl: "http://127.0.0.1:8787/status"
    property var sources: [
        { url: "status.json", file: true },              // beside Main.qml
        { url: "file://" + statusPath, file: true },     // an absolute path
        { url: statusUrl, file: false },                 // the daemon's endpoint
    ]
    property int source: 0
    property bool everLoaded: false
    property int attempts: 0

    readonly property bool fileBlocked: !sources[source].file

    /** True when the core module is present, which is the Basecamp case. */
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

    // What a write said, shown until the next one. Kept as one line rather
    // than a dialog: every one of these operations is small, and a modal for
    // "name set" would be worse than the operation.
    property string said: ""
    property bool saidBad: false

    // Which sections are unrolled. All shut by default: this view's job is to
    // show a mesh, and four open forms above the roster is a control panel
    // that happens to draw a graph.
    property bool settingsOpen: false
    property bool servicesOpen: false
    property bool membersOpen: false
    property bool logsOpen: false
    property bool dnsOpen: false

    // --- the log tail -------------------------------------------------------
    //
    // The same pane the Android app has had from the beginning, for the reason
    // it has had it: when a mesh does not come up, the log is the only thing
    // that says why, and on the desktop the alternative is journalctl in a
    // terminal — which is the terminal these controls exist to avoid.
    //
    // Polled incrementally. `since` is the stamp of the newest line already
    // held, so each poll carries what happened in the last two seconds rather
    // than re-sending two hundred lines and re-rendering the pane.
    property var logLines: []
    property string logSince: "0"
    readonly property int logKeep: 200

    // Why there is no log, when there is no log.
    //
    // This pane silently showed nothing against a daemon too old to have the
    // endpoint, which is the exact failure this whole feature exists to stop:
    // an empty box is indistinguishable from a quiet daemon, and the one thing
    // a diagnostic must never do is fail quietly. So every reason the lines did
    // not arrive is kept and rendered in their place.
    property string logProblem: ""

    function pumpLogs() {
        if (!logsOpen || !haveCore) return
        var t = String(callCore("logs", [root.logSince]) || "").trim()
        for (var i = 0; i < 2 && t.charAt(0) === '"'; i++) {
            try { t = String(JSON.parse(t)).trim() } catch (e) { break }
        }
        var d = null
        try { d = JSON.parse(t) } catch (e) { d = null }
        if (!d) {
            root.logProblem = "the module returned nothing readable"
            return
        }
        if (d.error) {
            // A 404 here has one cause and one fix, and the daemon's own
            // wording for it ("404 page not found") says neither. Named,
            // because "cannot read the daemon" sends somebody to check whether
            // it is running — and it plainly is, since everything above this
            // pane is being drawn from it.
            var detail = String(d.detail || "")
            root.logProblem = detail.indexOf("404") >= 0
                ? "this daemon has no log endpoint — it predates the pane. "
                  + "Update the daemon and restart it; everything else here "
                  + "keeps working meanwhile."
                : d.error + (detail ? " — " + detail : "")
            return
        }
        root.logProblem = ""
        if (!d.lines || !d.lines.length) return

        var out = root.logLines.concat(d.lines)
        // Bounded here as well as in the daemon: the daemon caps what it
        // holds, this caps what has been streamed across since the pane was
        // opened, which is a different and unbounded quantity.
        if (out.length > root.logKeep) out = out.slice(out.length - root.logKeep)
        root.logLines = out
        root.logSince = String(d.lines[d.lines.length - 1].t)
    }

    /**
     * The colour of one option in a two-option setting.
     *
     * The whole settings section used to paint every option in a fixed colour,
     * because each one is a button and buttons were styled as buttons. Which
     * meant nothing on the form ever said what the current value was: "on" was
     * bright next to a mesh that was switched off, and read as its state.
     *
     * So: the option in force is lit, the one you can move to is dim. Same rule
     * everywhere, so the section can be read at a glance instead of clicked to
     * find out.
     */
    function pick(current) { return current ? cPhosphor : cAsh }

    // --- size ---------------------------------------------------------------
    //
    // Everything here was sized in fixed pixels, which is fine on the screen it
    // was written on and tiny on anything denser. Basecamp does not appear to
    // be applying a device pixel ratio, so the view has to do it: one scale
    // derived from how wide the window actually is, applied to every font and
    // every fixed dimension.
    //
    // Anchored so that the screen it was designed on is unchanged — a window
    // around 1400 logical pixels scales by one — and a window twice that wide,
    // which is what a 4K panel without DPI scaling looks like, scales by two.
    // Gentler than the first attempt, which mapped a wide window straight to
    // 2x and made a good screen unreadable in the other direction. A dense
    // panel needs the text bigger, not doubled.
    readonly property real autoScale: Math.max(1.0, Math.min(1.45, root.width / 2000))

    // And an adjustment on top, because no formula knows how far away the
    // screen is. Nudged from the panel, applied everywhere at once.
    //
    // Not persisted: there is nowhere to put it that survives a restart
    // without inventing settings storage for one number. It resets to the
    // automatic value, which should be close enough that nobody has to.
    property real uiNudge: 0

    // --- preferences --------------------------------------------------------
    //
    // Kept by the core module, which is not sandboxed and can write a file.
    // Only view state lives here — whether the graph draws inferred links, how
    // big the text is — never anything about the mesh, which belongs to the
    // daemon's config and to every node that reads it.
    //
    // Loaded once, on the first status that proves the module is there. Saving
    // is fire and forget: a preference that fails to save is worth no error
    // path, it just starts where it started next time.
    property bool prefsLoaded: false

    function loadPrefs() {
        if (prefsLoaded || !haveCore) return
        prefsLoaded = true
        var w = String(callCore("getPref", ["whole_mesh"]) || "").trim()
        if (w === "1" || w === "0") root.wholeMesh = (w === "1")
        var vl = String(callCore("getPref", ["voice_lang"]) || "").trim()
        if (vl === "cs" || vl === "en" || vl === "auto") root.voiceLang = vl
        var n = parseFloat(String(callCore("getPref", ["ui_nudge"]) || ""))
        if (!isNaN(n)) root.uiNudge = Math.max(-0.4, Math.min(1.0, n))
    }
    function savePref(key, value) {
        if (!haveCore) return
        callCore("setPref", [key, String(value)])
    }
    readonly property real uiScale: Math.max(0.8, Math.min(2.2, autoScale + uiNudge))

    /** A font size, scaled. */
    function fs(n) { return Math.round(n * root.uiScale) }
    /** A fixed dimension — a column width, a row height, a margin — scaled. */
    function sz(n) { return Math.round(n * root.uiScale) }

    // --- copying ------------------------------------------------------------
    //
    // Addresses exist to be pasted somewhere else. They were plain labels, so
    // getting one out of here meant reading it off the screen and typing it,
    // which for `fd3b:ffe9:f81:81a7:18bc:69b1:9bb:7e69` is not a realistic
    // thing to ask of anybody.
    //
    // One click copies the whole thing rather than a drag selecting part of
    // it: every one of these is a single token, and a half-selected IPv6
    // address is worse than none.
    TextEdit {
        // The only way QML has to reach the clipboard without importing a
        // module this view deliberately does not depend on. Never shown, never
        // focused, and holds whatever was copied last.
        id: clipboard
        visible: false
        width: 0; height: 0
    }

    function copyText(s) {
        if (!s) return
        clipboard.text = String(s)
        clipboard.selectAll()
        clipboard.copy()
        root.said = "copied  " + s
        root.saidBad = false
    }

    /**
     * Open a service in whatever the desktop uses for it.
     *
     * Qt.openUrlExternally hands the URL to the desktop rather than fetching
     * anything, so it is not a network request and the sandbox that blocks
     * every one of those does not apply. If the host refuses anyway the
     * address is still one click from the clipboard, which is why both are
     * offered on every service row.
     */
    function openUrl(u) {
        if (!u) return
        if (Qt.openUrlExternally(u)) {
            root.said = "opened  " + u
            root.saidBad = false
        } else {
            root.said = "could not open " + u + " — copied it instead"
            root.saidBad = false
            copyText(u)
        }
    }

    /** The primary mesh's entry, which is the one the top-level fields describe. */
    readonly property var primaryMesh: {
        var ms = (root.st && root.st.meshes) ? root.st.meshes : []
        for (var i = 0; i < ms.length; i++) {
            if (ms[i] && ms[i].prefix && ms[i].prefix === root.st.prefix) return ms[i]
        }
        return ms.length ? ms[0] : ({})
    }

    /**
     * True for the mesh this device was built around.
     *
     * It cannot be switched off or left — the single-mesh config form has
     * nowhere to write "off", and a device with one mesh switched off is a
     * device switched off. The daemon refuses both, so the row says so rather
     * than offering two buttons that only ever produce an error.
     */
    function isPrimary(m) {
        return !!(m && m.prefix && root.st.prefix && m.prefix === root.st.prefix)
    }

    function levelColour(l) {
        if (l === "ERROR") return cRust
        if (l === "WARN") return cAmber
        if (l === "DEBUG") return cAsh
        return cBone
    }

    /** A log line's stamp as "12s ago", which is what the pane is read for. */
    function ago(ms) {
        var s = Math.max(0, (Date.now() - ms) / 1000)
        if (s < 60) return Math.round(s) + "s"
        if (s < 3600) return Math.round(s / 60) + "m"
        return Math.round(s / 3600) + "h"
    }

    // A write, and then a refresh, because every one of these changes
    // something the status page shows. Reporting the daemon's own sentence is
    // deliberate: it says what changed *and when it takes effect*, which is
    // the part a UI cannot know and keeps getting wrong.
    function callWrite(method, args) {
        if (!haveCore) {
            root.said = "no shrooms_core module; this view can only read"
            root.saidBad = true
            return
        }
        var t = String(callCore(method, args) || "").trim()
        for (var i = 0; i < 2 && t.charAt(0) === '"'; i++) {
            try { t = String(JSON.parse(t)).trim() } catch (e) { break }
        }
        // Not every endpoint answers in JSON, and assuming they all did is why
        // `reload` reported "the daemon said nothing readable" after every
        // successful reload since this form was built. /reload answers a plain
        // sentence — `reloaded: 1 mesh(es) republished services` — and the
        // module only returns a body at all when the daemon answered 2xx. So a
        // body that is not JSON is a success whose message is the body.
        //
        // The failure shape is unambiguous: the module builds its own
        // {"error":…} document, so anything starting with a brace is
        // structured and anything else is the daemon talking.
        var d = null
        if (t.charAt(0) === "{") {
            try { d = JSON.parse(t) } catch (e) { d = null }
        }
        if (!d && t !== "") {
            root.said = t
            root.saidBad = false
            root.reload()
            return
        }
        if (!d) {
            // Nothing at all came back. The module returns "" when the method
            // does not exist on it — an out-of-date shrooms_core — and the
            // view has no way to ask which methods it has, so that is named.
            root.said = "no answer from shrooms_core. If this control is new, "
                      + "update the shrooms_core module in Basecamp as well as "
                      + "this view — they ship separately and the view cannot "
                      + "tell which version is installed."
            root.saidBad = true
        } else if (d.error) {
            // A 404 means the daemon does not have the endpoint, which is a
            // version problem and not a fault. "the daemon answered 404" reads
            // as a bug in the app; it is a daemon that predates the button.
            var detail = String(d.detail || "")
            root.said = detail.indexOf("404") >= 0
                ? "this daemon is older than that control — update the daemon "
                  + "and restart it (sudo systemctl restart shrooms). "
                  + "Everything else here keeps working meanwhile."
                : d.error + (detail ? " — " + detail : "")
            root.saidBad = true
        } else {
            root.said = d.result ? d.result : "done"
            root.saidBad = false
        }
        root.reload()
    }

    function reload() {
        attempts++

        if (haveCore) {
            var raw = callCore("status", [])
            // The bridge may hand back a JSON string that is itself quoted;
            // qaku unwraps the same way. Unwrap, then parse.
            var t = String(raw || "").trim()
            for (var i = 0; i < 2 && t.charAt(0) === '"'; i++) {
                try { t = String(JSON.parse(t)).trim() } catch (e) { break }
            }
            if (t.charAt(0) === "{") {
                try {
                    var d = JSON.parse(t)
                    if (d.error) {
                        root.problem = d.error + (d.detail ? "\n" + d.detail : "")
                        root.peers = []
                    } else {
                        root.st = d
                        root.peers = d.peers || []
                        root.problem = ""
                        root.everLoaded = true
                    }
                    return
                } catch (e) { /* fall through to the file transports */ }
            }
            root.problem = "shrooms_core returned nothing readable"
            return
        }

        // Advance on elapsed attempts rather than on an error: a refused
        // file:// read does not deliver a failed request, so waiting for one
        // waits forever.
        if (!everLoaded && attempts > 2 * (source + 1) && source < sources.length - 1) {
            source++
        }
        var s = sources[source]
        fetch(s.url, s.file)
    }

    function fetch(url, isFile) {
        var xhr = new XMLHttpRequest()
        xhr.onreadystatechange = function() {
            if (xhr.readyState !== XMLHttpRequest.DONE) return

            if (!xhr.responseText) {
                if (isFile) return          // let the escalation above handle it
                root.problem = "no status from " + root.statusPath
                            + "\nnor from " + root.statusUrl
                root.peers = []
                return
            }
            try {
                var d = JSON.parse(xhr.responseText)
                root.st = d
                root.peers = d.peers || []
                root.problem = ""
                root.everLoaded = true
            } catch (e) {
                // The daemon writes atomically, so a partial document should be
                // impossible; if one appears, say so rather than showing a
                // stale mesh as if it were current.
                root.problem = "status is not readable JSON"
            }
        }
        try {
            xhr.open("GET", url)
            xhr.send()
        } catch (e) {
            // A refused file read can also throw outright, depending on host.
            if (isFile) root.fileBlocked = true
        }
    }

    Component.onCompleted: {
        reload()
        rebuildRows()
        loadPrefs()
    }
    Timer {
        interval: root.everLoaded ? 2000 : 700
        running: true; repeat: true
        onTriggered: {
            root.reload()
            root.pumpLogs()
        }
    }

    function reachOf(p) {
        if (p.live) return "connected"
        if (p.online) return "reaching"
        return "offline"
    }
    function colourOf(p) {
        var r = reachOf(p)
        if (r === "connected") return p.relayed ? cViolet : cPhosphor
        if (r === "reaching") return cAmber
        return cAsh
    }
    function rate(bps) {
        if (!bps || bps < 1) return "—"
        if (bps < 1024) return Math.round(bps) + " B/s"
        if (bps < 1024 * 1024) return (bps / 1024).toFixed(1) + " KB/s"
        return (bps / 1048576).toFixed(1) + " MB/s"
    }
    function since(s) {
        if (!s) return ""
        if (s < 60) return Math.round(s) + "s"
        if (s < 3600) return Math.round(s / 60) + "m"
        return Math.round(s / 3600) + "h"
    }

    // --- meshes -------------------------------------------------------------
    //
    // Every mesh this device knows about, in one order that the graph, the
    // roster headings and the legend all read. They have to agree: a tint that
    // means one mesh in the ring and another in the list is worse than no tint,
    // and they would disagree the moment one of them derived its own order.
    //
    // Built from `st.meshes` when the daemon reports it and topped up from the
    // peers' own labels, because a mesh whose peers are visible is a mesh worth
    // colouring even if an older daemon never listed it. A single-mesh node
    // labels nothing at all, so this comes out empty and every multi-mesh
    // decoration below switches itself off.
    readonly property var meshOrder: {
        var out = []
        var ms = (root.st && root.st.meshes) ? root.st.meshes : []
        for (var i = 0; i < ms.length; i++) {
            // A mesh with no instance behind it — switched off, or switched
            // on and not yet started — has no peers, no address and no
            // tunnels, so a colour and a legend entry for it would decorate
            // nothing. It appears in the settings list below, which is where
            // its state is and where it can be changed.
            if (ms[i] && (ms[i].disabled === true || ms[i].not_running === true)) continue
            var l = ms[i] ? ms[i].label : undefined
            if (l && out.indexOf(l) < 0) out.push(l)
        }
        for (var j = 0; j < root.peers.length; j++) {
            var m = root.peers[j].mesh || ""
            if (m !== "" && out.indexOf(m) < 0) out.push(m)
        }
        return out
    }
    readonly property bool multiMesh: meshOrder.length > 1

    /**
     * The meshes actually carrying traffic, which is what the graph, the
     * legend and the summary rows describe.
     *
     * A mesh that has been left is still here: leaving does not tear down a
     * tunnel, so it keeps working until a restart and dropping it from the
     * picture would hide traffic that is really flowing. A mesh switched on
     * and not yet started is not, for the mirror-image reason.
     */
    readonly property var runningMeshes: {
        var out = [], ms = (root.st && root.st.meshes) ? root.st.meshes : []
        for (var i = 0; i < ms.length; i++) {
            if (ms[i] && ms[i].disabled !== true && ms[i].not_running !== true) out.push(ms[i])
        }
        return out
    }

    /**
     * Every mesh this device belongs to, running or not.
     *
     * Everything the daemon reported, with no rule about when to show it. Two
     * bugs came out of having such a rule, in opposite directions and on
     * consecutive days: a mesh switched off vanished from the only list that
     * could switch it on, and then a mesh switched *on* vanished the same way
     * — it had left the disabled list and not yet joined the running one, so
     * the whole section emptied itself mid-click.
     *
     * A settings list that hides rows by counting them will always have a
     * third case waiting. So it hides nothing: one mesh gets one row, which
     * costs a line and cannot disappear.
     */
    readonly property var switchableMeshes: {
        return (root.st && root.st.meshes) ? root.st.meshes : []
    }

    /** What a mesh row's state reads as, in the daemon's own terms. */
    function meshState(m) {
        if (!m) return ""
        // Short enough to fit beside the switches. The long forms elided from
        // the left, which ate the only part that differed — three rows all
        // reading "…s on the next restart".
        //
        // Nothing at all for a mesh that is off and not running: the switch
        // beside it already says off, and there is no pending anything.
        if (m.left === true) return "left · stops at restart"
        if (m.disabled === true && m.not_running !== true) return "stops at restart"
        if (m.disabled === true) return ""
        if (m.not_running === true) return "starts at restart"
        return ""
    }

    function meshTint(label) {
        var i = meshOrder.indexOf(label)
        if (i < 0) i = 0
        return meshTints[i % meshTints.length]
    }

    // Sorted by (mesh, name) so each mesh occupies an arc of the graph's ring
    // rather than being scattered around it, and so the roster can group by
    // simply walking the list. Sorting a copy: `peers` is what the harness and
    // the header count, and reordering it under them would be a surprise.
    readonly property var sortedPeers: {
        var ps = root.peers ? root.peers.slice() : []
        ps.sort(function(a, b) {
            var am = a.mesh || "", bm = b.mesh || ""
            if (am !== bm) return am < bm ? -1 : 1
            var an = a.name || "", bn = b.name || ""
            return an < bn ? -1 : (an > bn ? 1 : 0)
        })
        return ps
    }

    // The roster as rows, with a heading before each mesh. One flat model
    // rather than a ListView section, because the model here is a plain JS
    // array and sections want roles; a header entry is the same thing and works
    // against whatever the daemon sent.
    /**
     * The roster's shape: who is listed, in what order, with which headings.
     *
     * Not their state — not whether a peer is up, its throughput, its
     * handshake age. Those change every two seconds and must not, on their
     * own, cause the model below to be rebuilt.
     */
    readonly property string rosterShape: {
        var ps = root.sortedPeers, out = []
        for (var i = 0; i < ps.length; i++) {
            out.push((ps[i].mesh || "") + "/" + (ps[i].name || ""))
        }
        return (root.multiMesh ? "m:" : "s:") + out.join(",")
    }

    /**
     * The roster as rows, with a heading before each mesh.
     *
     * Rebuilt only when rosterShape changes, which is the whole point.
     * Assigning a new array to a ListView's model resets the view, and the
     * status poll runs every two seconds — so the list scrolled itself back to
     * the top three times before anybody could read the row they had scrolled
     * down to find.
     *
     * The rows therefore carry identity, not data: a mesh and a name. Each
     * delegate looks the peer up live, so throughput and reachability stay
     * current while the model underneath them sits still.
     *
     * One flat model rather than a ListView section, because the model here is
     * a plain JS array and sections want roles; a header entry is the same
     * thing and works against whatever the daemon sent.
     */
    property var rows: []

    function rebuildRows() {
        var out = [], ps = root.sortedPeers, last = null
        for (var i = 0; i < ps.length; i++) {
            var m = ps[i].mesh || ""
            if (root.multiMesh && m !== last) {
                out.push({ header: true, mesh: m })
                last = m
            }
            out.push({ header: false, mesh: m, name: ps[i].name || "" })
        }
        root.rows = out
    }
    onRosterShapeChanged: rebuildRows()

    /**
     * A peer by the identity a row carries, or an empty object.
     *
     * Never undefined: a delegate binding that dereferences it on a peer that
     * has just left throws, and one thrown TypeError per delegate is a roster
     * that renders half a list.
     */
    function peerFor(mesh, name) {
        for (var i = 0; i < root.peers.length; i++) {
            var p = root.peers[i]
            if ((p.mesh || "") === mesh && (p.name || "") === name) return p
        }
        return ({})
    }

    /** How many peers a mesh has, counted here when the daemon did not count. */
    function meshPeers(m) {
        if (typeof m.peers === "number") return m.peers
        var c = 0
        for (var i = 0; i < root.peers.length; i++)
            if ((root.peers[i].mesh || "") === m.label) c++
        return c
    }

    /**
     * Days until this device's credential on a mesh runs out, or 999 when the
     * daemon did not say. Old daemons omit `expires` entirely, and a missing
     * field must read as "nothing to worry about" rather than as an expiry in
     * 1970.
     */
    function endsIn(m) {
        if (!m || !m.expires) return 999
        return Math.floor((m.expires - Date.now() / 1000) / 86400)
    }

    /**
     * What a peer says it offers, as the addresses you would actually type.
     *
     * A claim, not a health report: the peer repeats this list every few
     * minutes and only it knows whether the port answers. Printed anyway
     * because a service name is worthless until you can see it spelled out.
     */
    function serviceUrls(p) {
        var s = p ? p.services : undefined
        if (!s || !s.length) return ""
        var host = p.dns_name || p.name || ""
        var out = []
        for (var i = 0; i < s.length; i++) out.push("http://" + s[i] + "." + host)
        return out.join("   ")
    }

    /**
     * Everything reachable on any mesh, as one list.
     *
     * Grouped by mesh rather than by the device offering it, which is a
     * deliberate difference from the roster above: a service is a thing you
     * want to open, and the question in front of it is "which of my networks
     * is this on" far more often than "which box is it running on". The device
     * is still shown, because it is in the address either way.
     *
     * Two kinds in one list. An announced service (ADR-023) has a name of its
     * own and is reached at <service>.<device>; a bound port (ADR-026) has no
     * name and is reached at <device>:<port> — no forwarder involved, the
     * process is simply listening on the mesh address. Marked as such, because
     * one of them is a URL you can click and the other is a host and a port.
     */
    readonly property var allServices: {
        var out = []
        for (var i = 0; i < root.sortedPeers.length; i++) {
            var p = root.sortedPeers[i]
            var host = p.dns_name || p.name || ""
            var s = p.services || []
            for (var j = 0; j < s.length; j++) {
                out.push({ mesh: p.mesh || "", device: p.name || "?", live: p.live === true,
                           label: s[j], addr: "http://" + s[j] + "." + host, bound: false })
            }
            var b = p.bound || []
            for (var k = 0; k < b.length; k++) {
                // "ssh:22" — the name is advisory and the port is the fact, so
                // the port is what goes in the address.
                var parts = String(b[k]).split(":")
                var port = parts.length > 1 ? parts[parts.length - 1] : ""
                out.push({ mesh: p.mesh || "", device: p.name || "?", live: p.live === true,
                           label: parts[0], addr: host + (port ? ":" + port : ""), bound: true })
            }
        }
        return out
    }

    /** Rows for the services list: a heading per mesh, then its services. */
    readonly property var serviceRows: {
        var out = [], last = null
        var ss = root.allServices.slice()
        ss.sort(function(a, b) {
            if (a.mesh !== b.mesh) return a.mesh < b.mesh ? -1 : 1
            if (a.device !== b.device) return a.device < b.device ? -1 : 1
            return a.label < b.label ? -1 : (a.label > b.label ? 1 : 0)
        })
        for (var i = 0; i < ss.length; i++) {
            if (root.multiMesh && ss[i].mesh !== last) {
                out.push({ header: true, mesh: ss[i].mesh })
                last = ss[i].mesh
            }
            out.push({ header: false, svc: ss[i] })
        }
        return out
    }

    /**
     * What this device has bound to a mesh address, per mesh.
     *
     * The same list `shrooms bound` prints, and shown for the same reason: it
     * is what the announce switch beside it would disclose, and it is
     * discovered rather than declared — so it changes without anybody editing
     * anything.
     */
    readonly property var boundRows: {
        var out = [], ms = (root.st && root.st.meshes) ? root.st.meshes : []
        for (var i = 0; i < ms.length; i++) {
            var b = ms[i].bound_here || []
            for (var j = 0; j < b.length; j++) {
                // "ssh:22" — the name is advisory, the port is the fact, and
                // the host has to carry the mesh label or it names an address
                // on another network entirely.
                var parts = String(b[j]).split(":")
                var port = parts.length > 1 ? parts[parts.length - 1] : ""
                var host = root.st.name || "this-device"
                if (ms.length > 1 && ms[i].label) host += "." + ms[i].label
                out.push({ spec: b[j], mesh: ms[i].label,
                           addr: host + ".mesh" + (port ? ":" + port : ""),
                           announced: ms[i].announce_bound === true })
            }
        }
        return out
    }

    /** The bound ports belonging to one mesh, for that mesh's own block. */
    function boundFor(label) {
        var out = []
        for (var i = 0; i < root.boundRows.length; i++) {
            if (root.boundRows[i].mesh === label) out.push(root.boundRows[i])
        }
        return out
    }

    /** Days until a unix-seconds stamp, or 999 when there is none. */
    function daysTo(unix) {
        if (!unix) return 999
        return Math.floor((unix - Date.now() / 1000) / 86400)
    }

    /**
     * How a credential's remaining life reads.
     *
     * "unknown" is a real answer and not a bad one: expiry is learned from
     * what a peer announces, so a peer that has been quiet since this daemon
     * started simply has not said. Rendering that as "expired" would send
     * somebody chasing a renewal nobody needs.
     */
    function membershipText(unix) {
        if (!unix) return "unknown"
        var d = root.daysTo(unix)
        if (d < 0) return "ended"
        if (d === 0) return "ends today"
        return d + "d left"
    }
    function membershipColour(unix) {
        if (!unix) return cAsh
        var d = root.daysTo(unix)
        if (d < 0) return cRust
        if (d <= 10) return cAmber
        return cPhosphor
    }

    /**
     * Draw the links between other peers as well as this device's own.
     *
     * Off by default because those links are inferred, not measured: no node
     * knows how any two others reach each other, and a graph that draws a guess
     * the same way it draws a tunnel is lying.
     */
    property bool wholeMesh: false
    onWholeMeshChanged: if (prefsLoaded) savePref("whole_mesh", wholeMesh ? "1" : "0")
    onUiNudgeChanged: if (prefsLoaded) savePref("ui_nudge", uiNudge.toFixed(2))

    // A 32-bit string hash, the same one Java's String.hashCode computes, so a
    // node wanders the same way here as it does on the phone.
    function hashOf(s) {
        var h = 0
        for (var i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
        return h
    }

    Rectangle {
        anchors.fill: parent
        color: cVoid

        // Two columns, because there was a great deal of empty space to the
        // right of a graph and a list, and a settings form stacked underneath
        // them that could not be reached without scrolling a page that did not
        // scroll.
        //
        // The split also localises the scrolling problem. The left column ends
        // in a roster that scrolls itself; the right column is one flickable
        // panel. Neither can grow past the window, so nothing ends up below the
        // fold with no way down — which is what a single tall page did.
        ColumnLayout {
            anchors.fill: parent
            anchors.margins: root.sz(20)
            spacing: root.sz(12)

        RowLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: root.sz(18)

            // --- the mesh itself ------------------------------------------
            ColumnLayout {
                Layout.fillWidth: true
                Layout.preferredWidth: 0
                Layout.fillHeight: true
                spacing: root.sz(14)

                // --- header ---------------------------------------------------
                ColumnLayout {
                    spacing: 3
                    Layout.fillWidth: true

                    RowLayout {
                        spacing: 8
                        Rectangle {
                            width: root.sz(9); height: root.sz(9); radius: root.sz(5)
                            color: root.problem !== "" ? cRust
                                 : (root.peers.length > 0 ? cPhosphor : cAsh)
                        }
                        Text {
                            text: root.st.name ? root.st.name : "logos-vpn"
                            color: cBone
                            font.family: "monospace"; font.pixelSize: root.fs(17)
                        }
                        Text {
                            // The daemon's build, not this module's. They are
                            // different things and the daemon's is the one that
                            // decides whether a control here exists at all.
                            visible: text !== ""
                            text: root.st.version ? root.st.version : ""
                            color: cAsh
                            font.family: "monospace"; font.pixelSize: root.fs(10)
                        }
                        Item { Layout.fillWidth: true }
                        Text {
                            // The agents on the owner's machines (docs/agents.md).
                            text: "agents ▸"
                            color: agentsBtn.containsMouse ? cBone : cPhosphor
                            font.family: "monospace"; font.pixelSize: root.fs(11)
                            MouseArea { id: agentsBtn; anchors.fill: parent; hoverEnabled: true
                                        cursorShape: Qt.PointingHandCursor; onClicked: root.agentsOpen = true }
                        }
                        Text {
                            // Counted rather than assumed: a peer announcing is not
                            // a peer you can reach, and the difference is the whole
                            // point of the colours below.
                            text: {
                                var up = 0
                                for (var i = 0; i < root.peers.length; i++)
                                    if (root.peers[i].live) up++
                                return up + " of " + root.peers.length + " reachable"
                            }
                            color: cAsh
                            font.family: "monospace"; font.pixelSize: root.fs(11)
                        }
                    }

                    // This device's address, one click from the clipboard. The
                    // first thing anybody wants out of this window, and the one
                    // thing they previously had to read off the screen and retype
                    // — which for a 39-character IPv6 address is not a realistic
                    // thing to ask of anybody.
                    Text {
                        text: root.st.overlay ? root.st.overlay : ""
                        color: addrMouse.containsMouse ? cPhosphor : cAsh
                        font.family: "monospace"; font.pixelSize: root.fs(11)
                        font.underline: addrMouse.containsMouse
                        MouseArea {
                            id: addrMouse
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: root.copyText(parent.text)
                        }
                    }
                }

                // --- trouble ---------------------------------------------------
                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredWidth: 0
                    readonly property var rz: root.st ? root.st.rendezvous : undefined
                    visible: root.problem !== "" || (rz !== undefined && rz.ok === false)
                    color: "transparent"
                    border.color: root.problem !== "" ? cRust : cAmber
                    border.width: 1
                    radius: 8
                    implicitHeight: trouble.implicitHeight + 20





                    Text {
                        id: trouble
                        anchors.fill: parent
                        anchors.margins: 10
                        wrapMode: Text.WordWrap
                        color: root.problem !== "" ? cRust : cAmber
                        font.family: "monospace"; font.pixelSize: root.fs(11)
                        text: {
                            if (root.problem !== "")
                                return root.problem
                                     + "\n\nSet `status_file = \"" + root.statusPath
                                     + "\"` in /etc/logos-vpn/config.toml and restart the daemon."
                            // Discovery being down is not peers being offline. They
                            // look identical and have different causes.
                            var r = parent.rz
                            if (r === undefined) return ""
                            return "discovery: " + (r.problem || "unavailable")
                                 + "\nestablished tunnels are unaffected"
                        }
                    }
                }

                // --- graph ------------------------------------------------------
                //
                // Worth drawing because the topology is the interesting part and a
                // list cannot show it: which peers you reach directly, which go
                // through a relay, and which are talking but unreachable are three
                // different problems that look identical in a list.
                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredWidth: 0
                    Layout.preferredHeight: Math.max(root.sz(260), root.height * 0.38)
                    color: cPanel
                    radius: 10

                    Canvas {
                        id: graph
                        anchors.fill: parent
                        anchors.margins: 12

                        // Where the wander is in its cycle, in radians.
                        property real drift: 0

                        // Repaint the moment the mesh changes shape, rather than
                        // waiting up to a frame for the timer below: a peer coming
                        // up should land on the graph when it lands in the list.
                        property string shape: JSON.stringify(root.sortedPeers.map(function(p) {
                            return [p.mesh, p.name, p.live, p.online, p.relayed, p.relay]
                        }))
                        onShapeChanged: requestPaint()
                        onWidthChanged: requestPaint()
                        onHeightChanged: requestPaint()

                        Timer {
                            // A whole wander cycle every 19 seconds, the same period
                            // as the phone's, stepped from this timer's own interval
                            // so the two numbers cannot drift apart when one is
                            // edited. Sixteen frames a second is plenty for motion
                            // this slow and a Canvas repaint is not free.
                            interval: 60
                            running: true; repeat: true
                            onTriggered: {
                                var step = 2 * Math.PI * (interval / 1000) / 19
                                graph.drift = (graph.drift + step) % (2 * Math.PI)
                                graph.requestPaint()
                            }
                        }

                        onPaint: {
                            var ctx = getContext("2d")
                            ctx.reset()

                            var cx = width / 2, cy = height / 2
                            var radius = Math.min(width, height) / 2 * 0.66
                            var ps = root.sortedPeers
                            var n = ps.length
                            if (n === 0) {
                                ctx.fillStyle = root.cAsh
                                ctx.font = root.fs(11) + "px monospace"
                                ctx.textAlign = "center"
                                ctx.fillText("no peers yet", cx, cy)
                                return
                            }
                            var t = drift
                            var i, j, p

                            // How much a curve bows, swaying slowly. One cycle per
                            // drift period, for the same reason as the wander
                            // below: a filament that snaps back to a different
                            // curvature is the same glitch, on the links.
                            function bendOf(seed) {
                                var ph = ((seed >>> 5) & 0xffff) / 65535 * 2 * Math.PI
                                return 0.06 + 0.05 * Math.sin(t + ph)
                            }

                            // Links drawn as curves rather than straight lines.
                            // Pure decoration — the curve carries nothing the line
                            // did not — but the graph is the thing people look at,
                            // and mycelium does not grow in straight lines.
                            function filament(ax, ay, bx, by, bend) {
                                var mx = (ax + bx) / 2 - (by - ay) * bend
                                var my = (ay + by) / 2 + (bx - ax) * bend
                                ctx.beginPath()
                                ctx.moveTo(ax, ay)
                                ctx.quadraticCurveTo(mx, my, bx, by)
                                ctx.stroke()
                            }

                            // Every position for this frame, computed once: the
                            // links and the nodes must agree, and computing the
                            // wander twice invites them not to.
                            var at = []
                            for (i = 0; i < n; i++) {
                                var a = -Math.PI / 2 + 2 * Math.PI * i / n
                                // Seeded from the peer's own identity rather than
                                // from a random number, so a node keeps its motion
                                // across refreshes instead of jumping every time
                                // the roster is polled.
                                var seed = root.hashOf((ps[i].name || "?") + "/" + (ps[i].mesh || ""))
                                var phase = ((seed & 0xffff) / 65535) * 2 * Math.PI
                                var amp = radius * 0.085
                                        * (0.55 + (((seed >>> 8) & 0xff) / 255) * 0.45)
                                // Whole numbers of cycles, always.
                                //
                                // t runs 0..2π and restarts, so a harmonic that is
                                // not an integer lands somewhere else when it wraps
                                // and every node teleports — once every nineteen
                                // seconds, which is exactly long enough to look
                                // like a redraw bug rather than the animation's own
                                // doing. Two different integers give a Lissajous
                                // path instead of a circle, which is the part that
                                // made it look alive, and it closes perfectly.
                                var kx = 1 + ((seed >>> 16) & 1)
                                var ky = 2 + ((seed >>> 20) & 1)
                                at.push({ x: cx + radius * Math.cos(a) + amp * Math.cos(kx * t + phase),
                                          y: cy + radius * Math.sin(a) + amp * Math.sin(ky * t + phase) })
                            }

                            // The relay each mesh has, not the first relay on the
                            // screen. Traffic to a peer on one mesh cannot pass
                            // through a relay on another — different prefix,
                            // different WireGuard device — so bending the line
                            // through one drew a path that cannot exist, and the
                            // whole point of the graph is which path traffic takes.
                            var relayOf = ({})
                            for (i = 0; i < n; i++) {
                                var mk = ps[i].mesh || ""
                                if (ps[i].relay && relayOf[mk] === undefined) relayOf[mk] = i
                            }

                            for (i = 0; i < n; i++) {
                                p = ps[i]
                                var bend = bendOf(root.hashOf(p.name || "?"))
                                if (p.live && p.relayed) {
                                    ctx.lineWidth = 2.5
                                    ctx.strokeStyle = root.cViolet
                                    ctx.globalAlpha = 0.75
                                    ctx.setLineDash([9, 11])
                                    var r = relayOf[p.mesh || ""]
                                    if (r !== undefined && r !== i) {
                                        filament(cx, cy, at[r].x, at[r].y, bend)
                                        filament(at[r].x, at[r].y, at[i].x, at[i].y, bend)
                                    } else {
                                        // Relayed by someone this device cannot
                                        // see, which happens: the relay may be on a
                                        // mesh whose roster we do not carry.
                                        filament(cx, cy, at[i].x, at[i].y, bend)
                                    }
                                } else if (p.live) {
                                    ctx.lineWidth = 2.5
                                    ctx.strokeStyle = root.cPhosphor
                                    ctx.globalAlpha = 0.85
                                    ctx.setLineDash([])
                                    filament(cx, cy, at[i].x, at[i].y, bend)
                                } else if (p.online) {
                                    // Announcing but no tunnel: trying, not
                                    // connected. Drawn faintly so it is visibly not
                                    // the same thing as the line above.
                                    ctx.lineWidth = 1.5
                                    ctx.strokeStyle = root.cAmber
                                    ctx.globalAlpha = 0.5
                                    ctx.setLineDash([9, 11])
                                    filament(cx, cy, at[i].x, at[i].y, bend)
                                } else {
                                    ctx.lineWidth = 1
                                    ctx.strokeStyle = root.cLine
                                    ctx.globalAlpha = 1
                                    ctx.setLineDash([9, 11])
                                    filament(cx, cy, at[i].x, at[i].y, bend)
                                }
                            }

                            // Assumed links, underneath the nodes: same mesh, both
                            // reachable from here, so probably reachable from each
                            // other. Only within a mesh, because peers on different
                            // meshes genuinely cannot reach each other at all.
                            if (root.wholeMesh) {
                                ctx.lineWidth = 1.4
                                // Faint enough to read as a guess, visible enough
                                // to read at all. A tenth of an alpha was honest
                                // and invisible, which is not a useful pair.
                                ctx.globalAlpha = 0.30
                                ctx.setLineDash([5, 9])
                                for (i = 0; i < n; i++) {
                                    if (!ps[i].live) continue
                                    for (j = i + 1; j < n; j++) {
                                        if (!ps[j].live) continue
                                        if ((ps[i].mesh || "") !== (ps[j].mesh || "")) continue
                                        ctx.strokeStyle = root.meshTint(ps[i].mesh || "")
                                        filament(at[i].x, at[i].y, at[j].x, at[j].y,
                                                 bendOf(i * 31 + j))
                                    }
                                }
                            }

                            ctx.setLineDash([])
                            for (i = 0; i < n; i++) {
                                p = ps[i]
                                var col = root.colourOf(p)
                                // Punch the background out so links do not run
                                // under the node or its label.
                                ctx.globalAlpha = 1
                                ctx.fillStyle = root.cVoid
                                ctx.beginPath(); ctx.arc(at[i].x, at[i].y, root.sz(22), 0, 2 * Math.PI); ctx.fill()
                                ctx.globalAlpha = 0.14
                                ctx.fillStyle = col
                                ctx.beginPath(); ctx.arc(at[i].x, at[i].y, root.sz(18), 0, 2 * Math.PI); ctx.fill()
                                ctx.globalAlpha = 1
                                ctx.beginPath(); ctx.arc(at[i].x, at[i].y, root.sz(7), 0, 2 * Math.PI); ctx.fill()
                                if (root.multiMesh) {
                                    // The mesh is the ring, the reachability is the
                                    // node: the colour you look at first says
                                    // whether the peer answers, and the grouping is
                                    // drawn around it.
                                    ctx.globalAlpha = 0.85
                                    ctx.strokeStyle = root.meshTint(p.mesh || "")
                                    ctx.lineWidth = 2
                                    ctx.beginPath()
                                    ctx.arc(at[i].x, at[i].y, root.sz(18), 0, 2 * Math.PI)
                                    ctx.stroke()
                                    ctx.globalAlpha = 1
                                }
                                if (p.relay) {
                                    // Inside the mesh ring rather than on top of
                                    // it: two rings at one radius are one ring, in
                                    // whichever colour was drawn second.
                                    ctx.strokeStyle = root.cViolet
                                    ctx.lineWidth = 1.5
                                    ctx.beginPath()
                                    ctx.arc(at[i].x, at[i].y, root.sz(13), 0, 2 * Math.PI)
                                    ctx.stroke()
                                }
                                ctx.fillStyle = col
                                ctx.font = root.fs(10) + "px monospace"
                                ctx.textAlign = "center"
                                ctx.fillText(p.name || "?", at[i].x, at[i].y + root.sz(32))
                            }

                            // This device last: everything else is drawn relative
                            // to it. One node's view, not a map of the mesh — no
                            // node knows how any two others reach each other. It
                            // breathes rather than wanders, since the rest moving
                            // around it is exactly what the graph means; twice the
                            // drift, so it too closes on the wrap.
                            var breath = 1 + 0.06 * Math.sin(2 * t)
                            ctx.globalAlpha = 1
                            ctx.fillStyle = root.cVoid
                            ctx.beginPath(); ctx.arc(cx, cy, root.sz(26), 0, 2 * Math.PI); ctx.fill()
                            ctx.fillStyle = root.cBone
                            ctx.beginPath(); ctx.arc(cx, cy, root.sz(13) * breath, 0, 2 * Math.PI); ctx.fill()
                            ctx.globalAlpha = 0.18
                            ctx.strokeStyle = root.cBone
                            ctx.lineWidth = 1.5
                            ctx.beginPath(); ctx.arc(cx, cy, root.sz(26) * breath, 0, 2 * Math.PI); ctx.stroke()
                        }
                    }

                    // Only the whole-mesh picture is labelled, and only to disown
                    // the extra links: they are inferred from what peers report, so
                    // an unmarked drawing of them would pass off a guess as a
                    // measurement. The default picture is all measured and needs
                    // nothing said about it.
                    Text {
                        visible: root.wholeMesh && root.peers.length > 0
                        anchors.horizontalCenter: parent.horizontalCenter
                        anchors.top: parent.top
                        anchors.topMargin: 8
                        text: "whole mesh · faint links are assumed"
                        color: cAmber
                        font.family: "monospace"; font.pixelSize: root.fs(10)
                    }
                }

                // The toggle, and what the tints mean. Both belong under the
                // drawing rather than in settings: they change what this picture
                // shows, not what the daemon does.
                RowLayout {
                    Layout.fillWidth: true
                    Layout.preferredWidth: 0
                    spacing: 14
                    visible: root.peers.length > 0





                    Text {
                        text: root.wholeMesh ? "whole mesh ▾" : "whole mesh ▸"
                        color: root.wholeMesh ? cPhosphor : cAsh
                        font.family: "monospace"; font.pixelSize: root.fs(11)
                        MouseArea {
                            anchors.fill: parent
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                root.wholeMesh = !root.wholeMesh
                                graph.requestPaint()
                            }
                        }
                    }

                    Repeater {
                        model: root.multiMesh ? root.meshOrder : []
                        delegate: RowLayout {
                            spacing: 5
                            Rectangle {
                                width: root.sz(7); height: root.sz(7); radius: root.sz(4)
                                color: root.meshTint(modelData)
                            }
                            Text {
                                text: modelData
                                color: root.meshTint(modelData)
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                            }
                        }
                    }
                    Item { Layout.fillWidth: true }
                }

                // One row per mesh, above the roster it groups. Only when there is
                // more than one: on a single-mesh device every line here repeats
                // the header.
                Repeater {
                    model: root.runningMeshes.length > 1 ? root.runningMeshes : []
                    delegate: RowLayout {
                        Layout.fillWidth: true
                        spacing: 10





                        Text {
                            text: modelData.label || "?"
                            color: root.meshTint(modelData.label)
                            font.family: "monospace"; font.pixelSize: root.fs(11)
                            Layout.preferredWidth: root.sz(90)
                        }
                        Text {
                            text: (modelData.overlay || "")
                                  + (modelData.prefix ? "   " + modelData.prefix : "")
                            color: cAsh
                            font.family: "monospace"; font.pixelSize: root.fs(10)
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                            Layout.preferredWidth: 0
                        }
                        Text {
                            text: root.meshPeers(modelData) + " peers"
                            color: cAsh
                            font.family: "monospace"; font.pixelSize: root.fs(10)
                        }
                        Text {
                            // Said out loud because zero relays is a configuration
                            // rather than a fault, and it stays invisible until
                            // somebody is on mobile data and reaches nobody.
                            visible: modelData.relays === 0
                            text: "no relay"
                            color: cAmber
                            font.family: "monospace"; font.pixelSize: root.fs(10)
                        }
                        Text {
                            // Ten days is enough warning to find whoever holds the
                            // admin key, and short enough that it is not on screen
                            // permanently. Renewal is not a thing this view can do.
                            visible: root.endsIn(modelData) <= 10
                            text: root.endsIn(modelData) < 0
                                  ? "membership has ended"
                                  : "membership ends in " + root.endsIn(modelData) + " days"
                            color: cAmber
                            font.family: "monospace"; font.pixelSize: root.fs(10)
                        }
                    }
                }

                // --- list -------------------------------------------------------
                ListView {
                    Layout.fillWidth: true
                    Layout.preferredWidth: 0
                    Layout.fillHeight: true
                    clip: true
                    spacing: 5
                    model: root.rows

                    // One delegate for both kinds of row, switched by `header`.
                    // Two delegates would want a chooser, which lives in a module
                    // this view deliberately does not import.
                    delegate: Item {
                        width: ListView.view.width
                        implicitHeight: modelData.header === true
                                        ? heading.implicitHeight + 12
                                        : card.implicitHeight

                        // Looked up live rather than carried in the model. The
                        // model holds identity only, so it can sit still while
                        // these values change — see rosterShape. A row whose
                        // peer has just left resolves to an empty object, never
                        // undefined: one thrown TypeError per delegate is a
                        // roster that renders half a list.
                        readonly property var p: modelData.header === true
                                                 ? ({})
                                                 : root.peerFor(modelData.mesh || "",
                                                                modelData.name || "")





                        Text {
                            id: heading
                            visible: modelData.header === true
                            anchors.left: parent.left
                            anchors.bottom: parent.bottom
                            text: modelData.mesh ? modelData.mesh : "no mesh label"
                            color: root.meshTint(modelData.mesh)
                            font.family: "monospace"; font.pixelSize: root.fs(11)
                        }

                        Rectangle {
                            id: card
                            visible: modelData.header !== true
                            width: parent.width
                            implicitHeight: row.implicitHeight + 20
                            color: cPanel
                            radius: 8

                            ColumnLayout {
                                id: row
                                anchors.fill: parent
                                anchors.margins: 10
                                spacing: 4

                                RowLayout {
                                    spacing: 8
                                    Layout.fillWidth: true
                                    Layout.preferredWidth: 0
                                    Rectangle {
                                        width: root.sz(8); height: root.sz(8); radius: root.sz(4)
                                        color: root.colourOf(p)
                                    }
                                    Text {
                                        text: p.name || "?"
                                        color: cBone
                                        font.family: "monospace"; font.pixelSize: root.fs(13)
                                    }
                                    Text {
                                        visible: p.relay === true
                                        text: "RELAY"
                                        color: cViolet
                                        font.family: "monospace"; font.pixelSize: root.fs(9)
                                    }
                                    Item { Layout.fillWidth: true }
                                    Text {
                                        text: p.live
                                              ? (p.relayed ? "relayed" : "direct")
                                                + (p.rtt_ms ? " · " + p.rtt_ms + "ms" : "")
                                              : (p.online ? "no tunnel" : "offline")
                                        color: p.relayed ? cViolet : cAsh
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                    }
                                }

// Both forms, each copiable on its own: the name is what you
                                // type at a shell, the address is what you paste into
                                // something that does not resolve mesh names. Two labels
                                // rather than one line, because a click has to yield one of
                                // them and not the two stuck together.
                                RowLayout {
                                    spacing: root.sz(10)
                                    Layout.fillWidth: true
Layout.preferredWidth: 0
                                    Text {
                                        visible: !!p.dns_name
                                        text: p.dns_name || ""
                                        color: nameMouse.containsMouse ? cPhosphor : cAsh
                                        font.family: "monospace"; font.pixelSize: root.fs(10)
                                        font.underline: nameMouse.containsMouse
                                        MouseArea {
                                            id: nameMouse
                                            anchors.fill: parent
                                            hoverEnabled: true
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: root.copyText(parent.text)
                                        }
                                    }
                                    Text {
                                        text: p.overlay || ""
                                        color: peerAddrMouse.containsMouse ? cPhosphor : cAsh
                                        font.family: "monospace"; font.pixelSize: root.fs(10)
                                        font.underline: peerAddrMouse.containsMouse
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                        Layout.preferredWidth: 0
                                        MouseArea {
                                            id: peerAddrMouse
                                            anchors.fill: parent
                                            hoverEnabled: true
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: root.copyText(parent.text)
                                        }
                                    }
                                }

                                // What this peer says it offers, as addresses. Read
                                // only, selectable, and on one line: these are a
                                // claim the peer repeats rather than a health
                                // report, and a roster that grows a table per peer
                                // stops being a roster.
                                TextEdit {
                                    visible: text !== ""
                                    text: root.serviceUrls(p)
                                    readOnly: true
                                    selectByMouse: true
                                    // Dimmed while the peer cannot be reached. The
                                    // list outlives reachability on purpose — a
                                    // sleeping device still offers what it offers
                                    // — but drawn identically it invites somebody
                                    // to try an address that cannot answer.
                                    color: p.live === true ? cPhosphor : cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(10)
                                    wrapMode: TextEdit.Wrap
                                    Layout.fillWidth: true
                                    Layout.preferredWidth: 0
                                }

                                RowLayout {
                                    spacing: 16
                                    visible: p.live === true
                                    Text {
                                        text: "↓ " + root.rate(p.rx_bps)
                                              + "   ↑ " + root.rate(p.tx_bps)
                                        color: cAsh
                                        font.family: "monospace"; font.pixelSize: root.fs(10)
                                    }
                                    Text {
                                        visible: p.tunnel_after_s > 0
                                        // Coerced rather than guarded by `visible`:
                                        // a binding is evaluated whether or not the
                                        // item is shown, so calling toFixed on the
                                        // field an older daemon omits throws even
                                        // on a row nobody can see.
                                        text: "connected in "
                                              + (p.tunnel_after_s || 0).toFixed(1) + "s"
                                        color: cAsh
                                        font.family: "monospace"; font.pixelSize: root.fs(10)
                                    }
                                    Text {
                                        visible: p.handshake_age_s > 0
                                        // Shown always, never a bare "up": a peer
                                        // that restarted leaves the other side
                                        // holding a session that stays valid for a
                                        // while.
                                        text: "handshake " + root.since(p.handshake_age_s) + " ago"
                                        color: cAsh
                                        font.family: "monospace"; font.pixelSize: root.fs(10)
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // --- everything you can do about it ---------------------------
            //
            // Its own width rather than a share of the window: these are forms
            // and lists of short lines, and a form stretched across half a 4K
            // screen is harder to read than one that is not. The graph gets the
            // rest, which is what benefits from the room.
            Rectangle {
                Layout.preferredWidth: Math.max(root.sz(320),
                                                Math.min(root.sz(460), root.width * 0.34))
                Layout.fillHeight: true
                color: cVoid
                border.color: cLine
                border.width: 1
                radius: root.sz(10)

                Flickable {
                    id: panel
                    anchors.fill: parent
                    anchors.margins: root.sz(12)
                    clip: true
                    contentWidth: width
                    contentHeight: panelCol.implicitHeight
                    boundsBehavior: Flickable.StopAtBounds

                    ScrollBar.vertical: ScrollBar {
                        // Only while there is somewhere to go. A permanent bar
                        // on a panel that fits is a control that does nothing.
                        policy: panel.contentHeight > panel.height
                                ? ScrollBar.AlwaysOn : ScrollBar.AlwaysOff
                    }

                    ColumnLayout {
                        id: panelCol
                        // A plain subtraction. This was conditional on the
                        // scroll bar's visibility, reached through an attached
                        // property that does not resolve here — the expression
                        // came out NaN, so every row in the panel laid itself
                        // out at its natural width and ran off the right edge.
                        // The gutter is always there; it costs twelve pixels
                        // and cannot be wrong.
                        width: panel.width - root.sz(14)
                        spacing: root.sz(12)

                        RowLayout {
                            Layout.fillWidth: true
                            spacing: root.sz(8)
                            Text {
                                text: "text size"
                                color: cAsh
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                            }
                            Text {
                                text: "A−"
                                color: cPhosphor
                                font.family: "monospace"; font.pixelSize: root.fs(12)
                                MouseArea {
                                    anchors.fill: parent
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: root.uiNudge -= 0.1
                                }
                            }
                            Text {
                                text: "A+"
                                color: cPhosphor
                                font.family: "monospace"; font.pixelSize: root.fs(12)
                                MouseArea {
                                    anchors.fill: parent
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: root.uiNudge += 0.1
                                }
                            }
                            Text {
                                visible: root.uiNudge !== 0
                                text: "reset"
                                color: cAsh
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                MouseArea {
                                    anchors.fill: parent
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: root.uiNudge = 0
                                }
                            }
                            Item { Layout.fillWidth: true }
                        }

                        // What the last write said, at the top of the panel
                        // rather than tucked under the "settings" heading —
                        // it is the answer to whatever was just clicked, and
                        // an answer below the question it answers is one
                        // nobody reads.
                        //
                        // It holds until the next write instead of fading:
                        // several of these sentences say when a change takes
                        // effect, which is the part worth still being on
                        // screen a minute later.
                        Text {
                            visible: root.said !== ""
                            text: root.said
                            color: root.saidBad ? cRust : cPhosphor
                            font.family: "monospace"; font.pixelSize: root.fs(10)
                            wrapMode: Text.WordWrap
                            Layout.fillWidth: true
                            Layout.preferredWidth: 0
                            Layout.bottomMargin: root.sz(6)
                        }

                    // --- settings -------------------------------------------------
                    //
                    // Collapsed by default. This view's job is to show a mesh, and a
                    // form is not that; but the settings it can change are exactly the
                    // ones that otherwise mean finding a terminal, so they belong here
                    // rather than nowhere (ADR-025).
                    //
                    // What is missing is deliberate and worth knowing while reading
                    // this: nothing here admits or removes a device. That needs the
                    // admin key, which the daemon has never held.
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 8
                        visible: root.haveCore





                        Text {
                            text: root.settingsOpen ? "settings ▾" : "settings ▸"
                            color: cPhosphor
                            font.family: "monospace"; font.pixelSize: root.fs(11)
                            MouseArea {
                                anchors.fill: parent
                                cursorShape: Qt.PointingHandCursor
                                onClicked: root.settingsOpen = !root.settingsOpen
                            }
                        }





                        // Said once, up front, rather than discovered one button at a
                        // time. The daemon and this view ship separately and update
                        // separately, so a view newer than its daemon is the ordinary
                        // case after an update — and every control that the daemon does
                        // not have yet fails in its own way, which reads as "it's all
                        // weirdly half-broken" rather than as one version skew.
                        //
                        // Detected by the version field, which arrived in the same
                        // daemon as the log and the restart. Its absence is exactly the
                        // set of daemons that lack them.
                        Text {
                            visible: root.everLoaded && root.st.version === undefined
                            Layout.fillWidth: true
                            Layout.preferredWidth: 0
                            wrapMode: Text.WordWrap
                            color: cAmber
                            font.family: "monospace"; font.pixelSize: root.fs(10)
                            text: "this daemon is older than this view: the log, the restart "
                                  + "button and joining a mesh will not answer. Everything else "
                                  + "here works. To update it:"
                        }
                        TextEdit {
                            visible: root.everLoaded && root.st.version === undefined
                            Layout.fillWidth: true
                            Layout.preferredWidth: 0
                            readOnly: true
                            selectByMouse: true
                            color: cBone
                            font.family: "monospace"; font.pixelSize: root.fs(10)
                            text: "sudo install -m755 bin/shrooms /usr/local/bin/shrooms && "
                                  + "sudo systemctl restart shrooms"
                        }

                        ColumnLayout {
                            visible: root.settingsOpen
                            Layout.fillWidth: true
                            spacing: 10

                            // This device's name, as its peers see it. Prefilled from
                            // status so the field starts as the truth rather than
                            // empty, which reads as "unset".
                            RowLayout {
                                spacing: 8
                                Layout.fillWidth: true
                                Text {
                                    text: "name"
                                    color: cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    Layout.preferredWidth: root.sz(70)
                                }
                                Rectangle {
                                    Layout.fillWidth: true
                                    height: root.sz(26)
                                    color: cPanel
                                    border.color: cLine
                                    TextInput {
                                        id: nameField
                                        anchors.fill: parent
                                        anchors.leftMargin: 6
                                        verticalAlignment: TextInput.AlignVCenter
                                        color: cBone
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                        selectByMouse: true
                                        text: root.st.name ? root.st.name : ""
                                        onAccepted: root.callWrite("setName", [text])
                                    }
                                }
                                Text {
                                    text: "set"
                                    color: cPhosphor
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: root.callWrite("setName", [nameField.text])
                                    }
                                }
                            }

                            // Services, as the config writes them: "name:port", comma
                            // separated. Shown in the form that is stored so that what
                            // is typed here and what ends up in the file are the same
                            // string.
                            RowLayout {
                                spacing: 8
                                Layout.fillWidth: true
Layout.preferredWidth: 0
                                Text {
                                    text: "services"
                                    color: cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    Layout.preferredWidth: root.sz(70)
                                }
                                Rectangle {
                                    Layout.fillWidth: true
                                    height: root.sz(26)
                                    color: cPanel
                                    border.color: cLine
                                    TextInput {
                                        id: servicesField
                                        anchors.fill: parent
                                        anchors.leftMargin: 6
                                        verticalAlignment: TextInput.AlignVCenter
                                        color: cBone
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                        selectByMouse: true
                                        text: {
                                            var out = []
                                            var svcs = root.st.services || []
                                            for (var i = 0; i < svcs.length; i++)
                                                out.push(svcs[i].name + ":" + svcs[i].port)
                                            return out.join(", ")
                                        }
                                        onAccepted: root.callWrite("setServices", [text])
                                    }
                                }
                                Text {
                                    text: "set"
                                    color: cPhosphor
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: root.callWrite("setServices", [servicesField.text])
                                    }
                                }
                            }

                            // Announcing moved into the mesh rows below, where it
                            // belongs: it is per mesh in the config, and a single
                            // row here could only ever address the first one. It
                            // silently did — clicking it on a two-mesh node
                            // changed a mesh the user was not looking at.

                            // Whether the router is asked for a way in (ADR-024).
                            //
                            // On by default and worth being able to switch off
                            // from here, because it does ask to be reachable
                            // from the internet — a decision somebody may want
                            // to take back without finding a config file.
                            RowLayout {
                                spacing: root.sz(8)
                                Layout.fillWidth: true
                                visible: root.st.port_mapping !== undefined
                                Text {
                                    text: "router"
                                    color: cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    Layout.preferredWidth: root.sz(70)
                                }
                                Text {
                                    text: "ask for a way in"
                                    color: root.pick(root.st.port_mapping === true)
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: root.callWrite("setPortMapping",
                                                                  [root.st.port_mapping !== true])
                                    }
                                }
                                Item { Layout.fillWidth: true }
                            }

                            // Which mode this device runs the *rendezvous* plane
                            // in — Core or Edge, the config's own words.
                            //
                            // It said "light" and "relay", and "relay" already
                            // means something else here and something more
                            // important: a device that forwards WireGuard
                            // traffic for peers that cannot reach each other.
                            // Two unrelated settings under one word, one of
                            // which is per mesh and sits four rows below.
                            //
                            // The bandwidth stays, because what it costs is the
                            // part worth reading.
                            RowLayout {
                                spacing: 8
                                Layout.fillWidth: true
                                Text {
                                    text: "delivery"
                                    color: cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    Layout.preferredWidth: root.sz(70)
                                }
                                Text {
                                    text: "edge  ~3 MB/h"
                                    color: root.pick(root.st.mode === "Edge")
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: root.callWrite("setMode", ["Edge"])
                                    }
                                }
                                Text {
                                    text: "core  ~20 MB/h"
                                    color: root.st.mode === "Core" ? cAmber : cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: root.callWrite("setMode", ["Core"])
                                    }
                                }
                                Item { Layout.fillWidth: true }
                            }

                            Text {
                                // Its own line, wrapped. As the tail of the row above it was a
                                // whole sentence sitting after two short options — the longest
                                // unwrapped label in the panel, which set a width floor the
                                // whole section had to meet and pushed everything else past
                                // the edge.
                                //
                                // The config and the running process disagree exactly between
                                // a change and the restart that applies it, and saying so is
                                // the difference between "my click did nothing" and "my click
                                // is waiting".
                                visible: root.st.mode !== undefined
                                         && root.st.mode_running !== undefined
                                         && root.st.mode !== root.st.mode_running
                                Layout.fillWidth: true
                                Layout.preferredWidth: 0
                                Layout.leftMargin: root.sz(70)
                                wrapMode: Text.WordWrap
                                text: "still running as " + (root.st.mode_running || "?")
                                      + " — restart to apply"
                                color: cAmber
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                            }

                            // The meshes this device belongs to.
                            //
                            // A section with a heading and a row per mesh, rather than
                            // a row per mesh each labelled "mesh" — which is what this
                            // was, and read as a stutter with the state hidden inside
                            // it. Switching one off is not leaving it, and both live
                            // here because both otherwise mean a terminal.
                            ColumnLayout {
                                Layout.fillWidth: true
                                Layout.topMargin: 4
                                spacing: 6
                                visible: root.switchableMeshes.length > 0





                                Text {
                                    text: "meshes"
                                    color: cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                }

                                Repeater {
                                    // Every mesh, switched off ones included — this
                                    // list is the only way back for one that is off,
                                    // and driving it from the running meshes is
                                    // precisely how a mesh became unreachable from
                                    // every screen at once.
                                    model: root.switchableMeshes
                                    delegate: ColumnLayout {
                                        // A small block per mesh rather than one long row. Six switches
                                        // and a sentence do not fit a panel this wide — the state text
                                        // was being elided down to "stops a", which is the one part of
                                        // the row that is not self-explanatory.
                                        //
                                        // Identity and lifecycle on the first line, disclosure on the
                                        // second, and what would be disclosed under both.
                                        Layout.fillWidth: true
                                        Layout.preferredWidth: 0
                                        Layout.topMargin: root.sz(3)
                                        spacing: root.sz(2)

                                        readonly property bool isOn: modelData.disabled !== true
                                        readonly property bool pending: modelData.not_running === true
                                                                        || modelData.left === true
                                        readonly property bool primary: root.isPrimary(modelData)

                                        RowLayout {
                                            Layout.fillWidth: true
                                            spacing: root.sz(10)
                                            Rectangle {
                                                width: root.sz(7); height: root.sz(7); radius: root.sz(4)
                                                color: parent.parent.pending ? "transparent"
                                                     : (parent.parent.isOn ? root.meshTint(modelData.label) : cLine)
                                                border.color: parent.parent.isOn ? root.meshTint(modelData.label) : cAsh
                                                border.width: parent.parent.pending ? 1 : 0
                                            }
                                            Text {
                                                text: modelData.label || "?"
                                                color: parent.parent.isOn ? cBone : cAsh
                                                font.family: "monospace"; font.pixelSize: root.fs(11)
                                                Layout.preferredWidth: root.sz(84)
                                                elide: Text.ElideRight
                                            }
                                            Text {
                                                text: "on"
                                                color: root.pick(parent.parent.isOn)
                                                font.family: "monospace"; font.pixelSize: root.fs(11)
                                                MouseArea {
                                                    anchors.fill: parent
                                                    cursorShape: Qt.PointingHandCursor
                                                    onClicked: root.callWrite("setMeshEnabled", [modelData.label, true])
                                                }
                                            }
                                            Text {
                                                // Absent on the primary mesh rather than present and
                                                // refused: the daemon rejects both of these for it.
                                                visible: !parent.parent.primary
                                                text: "off"
                                                color: root.pick(!parent.parent.isOn)
                                                font.family: "monospace"; font.pixelSize: root.fs(11)
                                                MouseArea {
                                                    anchors.fill: parent
                                                    cursorShape: Qt.PointingHandCursor
                                                    onClicked: root.callWrite("setMeshEnabled", [modelData.label, false])
                                                }
                                            }
                                            Text {
                                                visible: !parent.parent.primary
                                                property bool armed: false
                                                text: armed ? "sure?" : "leave"
                                                color: armed ? cAmber : cRust
                                                font.family: "monospace"; font.pixelSize: root.fs(11)
                                                MouseArea {
                                                    anchors.fill: parent
                                                    cursorShape: Qt.PointingHandCursor
                                                    onClicked: {
                                                        if (!parent.armed) { parent.armed = true; return }
                                                        parent.armed = false
                                                        root.callWrite("leaveMesh", [modelData.label])
                                                    }
                                                }
                                            }
                                            Item { Layout.fillWidth: true }
                                            Text {
                                                text: root.meshState(modelData)
                                                visible: text !== ""
                                                color: cAmber
                                                font.family: "monospace"; font.pixelSize: root.fs(9)
                                            }
                                        }

                                        RowLayout {
                                            Layout.fillWidth: true
                                            Layout.leftMargin: root.sz(17)
                                            spacing: root.sz(10)
                                            Text {
                                                text: "tell peers"
                                                color: cAsh
                                                font.family: "monospace"; font.pixelSize: root.fs(9)
                                            }
                                            Text {
                                                // Per mesh, because it is per mesh in the config: telling
                                                // your own machines what you run and telling somebody
                                                // else's are different decisions (ADR-023).
                                                text: "services"
                                                color: root.pick(modelData.announce_services === true)
                                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                                MouseArea {
                                                    anchors.fill: parent
                                                    cursorShape: Qt.PointingHandCursor
                                                    onClicked: root.callWrite("setAnnounceServices",
                                                                              [modelData.label,
                                                                               modelData.announce_services !== true])
                                                }
                                            }
                                            Text {
                                                // And the ports that merely happen to be listening
                                                // (ADR-026) — separate, because those are discovered
                                                // rather than declared.
                                                text: "ports"
                                                color: root.pick(modelData.announce_bound === true)
                                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                                MouseArea {
                                                    anchors.fill: parent
                                                    cursorShape: Qt.PointingHandCursor
                                                    onClicked: root.callWrite("setAnnounceBound",
                                                                              [modelData.label,
                                                                               modelData.announce_bound !== true])
                                                }
                                            }
                                            Text {
                                                text: "· forward for others"
                                                color: cAsh
                                                font.family: "monospace"; font.pixelSize: root.fs(9)
                                            }
                                            Text {
                                                text: "relay"
                                                color: root.pick(modelData.relay === true)
                                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                                MouseArea {
                                                    anchors.fill: parent
                                                    cursorShape: Qt.PointingHandCursor
                                                    onClicked: root.callWrite("setRelay",
                                                                              [modelData.label, modelData.relay !== true])
                                                }
                                            }
                                            Item { Layout.fillWidth: true }
                                        }

                                        // What "ports" would disclose, under the switch that discloses
                                        // it. Choosing blind is the alternative, and the list changes
                                        // every time somebody starts a server and forgets.
                                        Repeater {
                                            model: root.boundFor(modelData.label)
                                            delegate: RowLayout {
                                                Layout.fillWidth: true
                                                Layout.leftMargin: root.sz(17)
                                                spacing: root.sz(8)
                                                Text {
                                                    text: modelData.spec
                                                    color: modelData.announced ? cPhosphor : cAsh
                                                    font.family: "monospace"; font.pixelSize: root.fs(9)
                                                    Layout.preferredWidth: root.sz(74)
                                                    elide: Text.ElideRight
                                                }
                                                Text {
                                                    text: modelData.addr
                                                    color: bMouse.containsMouse ? cPhosphor : cAsh
                                                    font.family: "monospace"; font.pixelSize: root.fs(9)
                                                    font.underline: bMouse.containsMouse
                                                    elide: Text.ElideRight
                                                    Layout.fillWidth: true
                                                    Layout.preferredWidth: 0
                                                    MouseArea {
                                                        id: bMouse
                                                        anchors.fill: parent
                                                        hoverEnabled: true
                                                        cursorShape: Qt.PointingHandCursor
                                                        onClicked: root.copyText(modelData.addr)
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }






                                Text {
                                    // Only when something is actually pending, so it
                                    // is information rather than decoration.
                                    visible: {
                                        var ms = root.switchableMeshes
                                        for (var i = 0; i < ms.length; i++)
                                            if (ms[i].disabled === true) return true
                                        return false
                                    }
                                    Layout.fillWidth: true
                                    Layout.preferredWidth: 0
                                    Layout.leftMargin: root.sz(12)
                                    wrapMode: Text.WordWrap
                                    color: cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(10)
                                    text: "a mesh switched off keeps its key and credentials — "
                                          + "switching it back on and restarting is all it takes"
                                }
                            }

                            // Whether names resolve on this machine, and where.
                            //
                            // Folded away like the log, because the answer is almost
                            // always "yes" and the details matter only when it is not.
                            // Worth having at all because this fails on its own and
                            // silently: port 53 needs a capability and the system
                            // resolver needs telling about the suffix, and either can
                            // be missing while every tunnel is perfect. Then
                            // `ssh laptop.mesh` does not work and nothing else on this
                            // page hints at why.
                            ColumnLayout {
                                id: dnsBlock
                                Layout.fillWidth: true
                                spacing: 4
                                readonly property var d: root.st.dns || ({})

                                // Hidden entirely against a daemon that does not report
                                // this. A missing field is "it did not say", and
                                // rendering that as "not resolving" would put a red
                                // line on a machine whose names work perfectly.
                                visible: root.st.dns !== undefined

                                RowLayout {
                                    spacing: 8
                                    Layout.fillWidth: true
                                    Text {
                                        text: "names"
                                        color: cAsh
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                        Layout.preferredWidth: root.sz(70)
                                    }
                                    Text {
                                        text: dnsBlock.d.registered ? "resolving \u25b8"
                                            : (dnsBlock.d.serving ? "serving, not registered \u25b8"
                                                                  : "not resolving \u25b8")
                                        color: dnsBlock.d.registered ? cPhosphor
                                             : (dnsBlock.d.serving ? cAmber : cRust)
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                        MouseArea {
                                            anchors.fill: parent
                                            cursorShape: Qt.PointingHandCursor
                                            onClicked: root.dnsOpen = !root.dnsOpen
                                        }
                                    }
                                    Item { Layout.fillWidth: true }
                                }
                                Text {
                                    visible: root.dnsOpen
                                    Layout.fillWidth: true
                                    Layout.preferredWidth: 0
                                    Layout.leftMargin: root.sz(78)
                                    wrapMode: Text.WordWrap
                                    color: cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(10)
                                    text: {
                                        var d = dnsBlock.d, out = []
                                        if (d.address) out.push("served on " + d.address)
                                        if (d.suffix) out.push("suffix ." + d.suffix)
                                        out.push(d.registered
                                                 ? "the host sends " + (d.suffix ? "." + d.suffix : "mesh names") + " here"
                                                 : "the host has not been told to send names here, so only "
                                                   + "a direct query to the address resolves")
                                        if (d.err) out.push(d.err)
                                        return out.join("\n")
                                    }
                                }
                            }

                            RowLayout {
                                spacing: 12
                                Layout.fillWidth: true
                                Text {
                                    text: "apply"
                                    color: cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    Layout.preferredWidth: root.sz(70)
                                }
                                Text {
                                    // Says what it does and what it cannot: services
                                    // change under a running daemon, a mesh coming or
                                    // going does not.
                                    text: "reload"
                                    color: cPhosphor
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: root.callWrite("reload", [])
                                    }
                                }
                                Text {
                                    // The other half of every "on the next restart"
                                    // above. Armed with a second click because it
                                    // drops every tunnel for a few seconds, which is
                                    // not much and is not nothing if somebody is
                                    // copying a file over one.
                                    property bool armed: false
                                    text: armed ? "sure?" : "restart"
                                    color: armed ? cAmber : cPhosphor
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: {
                                            if (!parent.armed) { parent.armed = true; return }
                                            parent.armed = false
                                            root.callWrite("restart", [])
                                        }
                                    }
                                }
                                Item { Layout.fillWidth: true }
                            }


                            Text {
                                // What each verb does, said once under both — rather than
                                // a sentence trailing off each one inside a row that has
                                // room for neither.
                                Layout.fillWidth: true
                                Layout.preferredWidth: 0
                                Layout.leftMargin: root.sz(70)
                                wrapMode: Text.WordWrap
                                text: "reload applies services · restart applies the rest, "
                                      + "and drops every tunnel for a few seconds"
                                color: cAsh
                                font.family: "monospace"; font.pixelSize: root.fs(9)
                            }
                        }
                    }

                    // --- services ---------------------------------------------------
                    //
                    // What everything on every mesh offers, which the roster shows one
                    // peer at a time and this shows as a list you can read down. Both
                    // are worth having: the roster answers "is that box reachable",
                    // this answers "where do I find Immich".
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 8





                        Text {
                            text: (root.servicesOpen ? "services ▾  " : "services ▸  ")
                                  + root.allServices.length
                            color: cPhosphor
                            font.family: "monospace"; font.pixelSize: root.fs(11)
                            MouseArea {
                                anchors.fill: parent
                                cursorShape: Qt.PointingHandCursor
                                onClicked: root.servicesOpen = !root.servicesOpen
                            }
                        }

                        ColumnLayout {
                            visible: root.servicesOpen
                            Layout.fillWidth: true
                            Layout.preferredWidth: 0
                            spacing: 6





                            Text {
                                visible: root.allServices.length === 0
                                Layout.fillWidth: true
                                Layout.preferredWidth: 0
                                wrapMode: Text.WordWrap
                                color: cAsh
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                // Says which of the two reasons it is, because they
                                // have opposite fixes and look identical from here.
                                text: "nothing announced. A peer publishes services and lists them "
                                      + "separately — a device that has not been told to announce "
                                      + "offers exactly what it always did, silently."
                            }

                            Repeater {
                                model: root.serviceRows
                                delegate: Item {
                                    id: svcItem
                                    Layout.fillWidth: true
                                    implicitHeight: modelData.header === true
                                                    ? svcHead.implicitHeight + 8
                                                    : svcRow.implicitHeight + 4

                                    // Named rather than reached through parent.parent:
                                    // a delegate that walks its own tree breaks the
                                    // day anything is wrapped in a layout, and it
                                    // breaks as a binding loop rather than as an
                                    // error.
                                    readonly property var s: modelData.svc || ({})





                                    Text {
                                        id: svcHead
                                        visible: modelData.header === true
                                        anchors.left: parent.left
                                        anchors.bottom: parent.bottom
                                        text: modelData.mesh ? modelData.mesh : "no mesh label"
                                        color: root.meshTint(modelData.mesh)
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                    }

                                    RowLayout {
                                        id: svcRow
                                        visible: modelData.header !== true
                                        width: parent.width
                                        spacing: 10

                                        Rectangle {
                                            width: root.sz(6); height: root.sz(6)
                                            radius: root.sz(3)
                                            color: svcItem.s.live ? cPhosphor : cAsh
                                        }
                                        Text {
                                            // The name opens it. A service is a thing
                                            // you go to, and every other way of getting
                                            // there from here meant retyping an address
                                            // this row is already showing.
                                            //
                                            // Announced services only: a bound port
                                            // (ADR-026) is a host and a port with no
                                            // scheme, and guessing http:// for it would
                                            // open the wrong thing convincingly.
                                            text: svcItem.s.label || ""
                                            color: (svcOpen.containsMouse && !svcItem.s.bound)
                                                   ? cPhosphor : cBone
                                            font.family: "monospace"; font.pixelSize: root.fs(11)
                                            font.underline: svcOpen.containsMouse
                                                            && !svcItem.s.bound
                                            Layout.preferredWidth: root.sz(96)
                                            elide: Text.ElideRight
                                            MouseArea {
                                                id: svcOpen
                                                anchors.fill: parent
                                                enabled: !svcItem.s.bound
                                                hoverEnabled: true
                                                cursorShape: Qt.PointingHandCursor
                                                onClicked: root.openUrl(svcItem.s.addr)
                                            }
                                        }
                                        Text {
                                            // The address copies. Both affordances on
                                            // every row: a desktop that refuses to open
                                            // a URL is a real case, and the clipboard
                                            // always works.
                                            text: svcItem.s.addr || ""
                                            color: svcCopy.containsMouse ? cPhosphor
                                                 : (svcItem.s.live ? cPhosphor : cAsh)
                                            font.family: "monospace"; font.pixelSize: root.fs(10)
                                            font.underline: svcCopy.containsMouse
                                            elide: Text.ElideRight
                                            Layout.fillWidth: true
                                            Layout.preferredWidth: 0
                                            MouseArea {
                                                id: svcCopy
                                                anchors.fill: parent
                                                hoverEnabled: true
                                                cursorShape: Qt.PointingHandCursor
                                                onClicked: root.copyText(svcItem.s.addr)
                                            }
                                        }
                                        Text {
                                            // A bound port is not a forwarded service:
                                            // the process is listening on the mesh
                                            // address itself, so it is a host and a
                                            // port rather than a URL (ADR-026).
                                            visible: svcItem.s.bound === true
                                            text: "bound"
                                            color: cViolet
                                            font.family: "monospace"; font.pixelSize: root.fs(9)
                                        }
                                        Text {
                                            text: svcItem.s.device || ""
                                            color: cAsh
                                            font.family: "monospace"; font.pixelSize: root.fs(10)
                                        }
                                    }
                                }
                            }
                        }
                    }

                    // --- membership -------------------------------------------------
                    //
                    // Who is in, until when, and how somebody else gets in.
                    //
                    // Worth its own section because credentials expiring is the one
                    // failure in this system that is scheduled: it happens on a known
                    // day, it takes a device off the mesh, and nothing else here hints
                    // at it until the device is gone.
                    ColumnLayout {
                        Layout.fillWidth: true
                        Layout.preferredWidth: 0
                        spacing: 8
                        visible: root.haveCore





                        Text {
                            text: root.membersOpen ? "membership ▾" : "membership ▸"
                            color: cPhosphor
                            font.family: "monospace"; font.pixelSize: root.fs(11)
                            MouseArea {
                                anchors.fill: parent
                                cursorShape: Qt.PointingHandCursor
                                onClicked: root.membersOpen = !root.membersOpen
                            }
                        }

                        ColumnLayout {
                            visible: root.membersOpen
                            Layout.fillWidth: true
                            Layout.preferredWidth: 0
                            spacing: 8

                            // This device first, per mesh: its own expiry is the one
                            // nobody else will warn it about.
                            //
                            // Under one heading rather than repeating "this device" on
                            // every row, which is what four meshes turned it into — the
                            // same two words four times, with the useful part after them.
                            Text {
                                visible: (root.st.meshes || []).length > 0
                                text: "this device"
                                color: cAsh
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                            }
                            Repeater {
                                model: root.st.meshes || []
                                delegate: RowLayout {
                                    Layout.fillWidth: true
                                    Layout.preferredWidth: 0
                                    Layout.leftMargin: root.sz(12)
                                    spacing: root.sz(10)
                                    Text {
                                        text: modelData.label || "?"
                                        color: root.meshTint(modelData.label)
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                        Layout.preferredWidth: root.sz(100)
                                        elide: Text.ElideRight
                                    }
                                    Text {
                                        text: root.membershipText(modelData.expires)
                                        color: root.membershipColour(modelData.expires)
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                    }
                                    Item { Layout.fillWidth: true }
                                }
                            }





                            Text {
                                visible: root.peers.length > 0
                                text: "everybody else"
                                color: cAsh
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                Layout.topMargin: root.sz(6)
                            }
                            Repeater {
                                model: root.sortedPeers
                                delegate: RowLayout {
                                    Layout.fillWidth: true
                                    Layout.preferredWidth: 0
                                    Layout.leftMargin: root.sz(12)
                                    spacing: root.sz(10)
                                    Text {
                                        text: modelData.name || "?"
                                        color: cBone
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                        Layout.preferredWidth: root.sz(100)
                                        elide: Text.ElideRight
                                    }
                                    Text {
                                        visible: root.multiMesh
                                        text: modelData.mesh || ""
                                        color: root.meshTint(modelData.mesh || "")
                                        font.family: "monospace"; font.pixelSize: root.fs(10)
                                        Layout.preferredWidth: root.sz(70)
                                        elide: Text.ElideRight
                                    }
                                    Text {
                                        text: root.membershipText(modelData.expires)
                                        color: root.membershipColour(modelData.expires)
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                    }
                                    Item { Layout.fillWidth: true }
                                }
                            }

                            // Joining another mesh. The token comes from whoever is
                            // running `shrooms invite` at the far end, right now — the
                            // exchange is live, so this waits for them and can take a
                            // couple of minutes.
                            RowLayout {
                                spacing: 8
                                Layout.fillWidth: true
                                Text {
                                    text: "join"
                                    color: cAsh
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    Layout.preferredWidth: root.sz(70)
                                }
                                Rectangle {
                                    Layout.fillWidth: true
                                    height: root.sz(26)
                                    color: cPanel
                                    border.color: cLine
                                    TextInput {
                                        id: tokenField
                                        anchors.fill: parent
                                        anchors.leftMargin: 6
                                        verticalAlignment: TextInput.AlignVCenter
                                        color: cBone
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                        selectByMouse: true
                                    }
                                    // A label rather than a placeholder property,
                                    // which TextInput does not have. Shown only while
                                    // the field is empty.
                                    //
                                    // Not echoed as dots: a token admits one device
                                    // once and is useless afterwards, and hiding it
                                    // would only stop somebody checking they pasted
                                    // the whole thing.
                                    Text {
                                        visible: tokenField.text === ""
                                        anchors.left: parent.left
                                        anchors.leftMargin: 6
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: "invite token"
                                        color: cAsh
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                    }
                                }
                                Rectangle {
                                    Layout.preferredWidth: root.sz(110)
                                    height: root.sz(26)
                                    color: cPanel
                                    border.color: cLine
                                    TextInput {
                                        id: labelField
                                        anchors.fill: parent
                                        anchors.leftMargin: 6
                                        verticalAlignment: TextInput.AlignVCenter
                                        color: cBone
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                        selectByMouse: true
                                    }
                                    Text {
                                        visible: labelField.text === ""
                                        anchors.left: parent.left
                                        anchors.leftMargin: 6
                                        anchors.verticalCenter: parent.verticalCenter
                                        text: "label"
                                        color: cAsh
                                        font.family: "monospace"; font.pixelSize: root.fs(11)
                                    }
                                }
                                Text {
                                    text: "join"
                                    color: cPhosphor
                                    font.family: "monospace"; font.pixelSize: root.fs(11)
                                    MouseArea {
                                        anchors.fill: parent
                                        cursorShape: Qt.PointingHandCursor
                                        onClicked: {
                                            root.said = "redeeming — the far side has to be "
                                                      + "running `shrooms invite` right now"
                                            root.saidBad = false
                                            root.callWrite("joinWithInvite",
                                                           [tokenField.text, root.st.name || "",
                                                            labelField.text])
                                        }
                                    }
                                }
                            }
                            Text {
                                Layout.fillWidth: true
                                Layout.preferredWidth: 0
                                Layout.leftMargin: root.sz(78)
                                wrapMode: Text.WordWrap
                                color: cAsh
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                text: "the second box is the local label — what this device will call "
                                      + "that mesh, as in laptop.<label>.mesh. A joined mesh starts on "
                                      + "the next restart."
                            }

                            // The honest limit, stated where somebody would look for
                            // the button. Issuing an invite means signing a credential
                            // with the admin key, and the daemon has never held it —
                            // that separation is what keeps handing out this socket a
                            // bounded grant rather than a way to admit anybody.
                            Text {
                                Layout.fillWidth: true
                                Layout.preferredWidth: 0
                                wrapMode: Text.WordWrap
                                color: cAsh
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                text: "inviting somebody, and revoking them, needs the admin key, which "
                                      + "this daemon deliberately does not hold:"
                            }
                            // One click, like every other address here. A
                            // command you have to retype is a command you
                            // mistype, and this one ends in a name somebody is
                            // going to edit anyway.
                            Text {
                                Layout.fillWidth: true
                                Layout.preferredWidth: 0
                                text: "shrooms invite --name their-laptop"
                                color: inviteCopy.containsMouse ? cPhosphor : cBone
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                font.underline: inviteCopy.containsMouse
                                elide: Text.ElideRight
                                MouseArea {
                                    id: inviteCopy
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: root.copyText(parent.text)
                                }
                            }
                        }
                    }

                }
            }
        }
    }

            // --- log --------------------------------------------------------
            //
            // The same pane the phone has. Last, because it is the thing you
            // open when everything above has failed to explain itself.
            ColumnLayout {
                Layout.fillWidth: true
                spacing: 6
                visible: root.haveCore





                Text {
                    text: root.logsOpen ? "log ▾" : "log ▸"
                    color: cPhosphor
                    font.family: "monospace"; font.pixelSize: root.fs(11)
                    MouseArea {
                        anchors.fill: parent
                        cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            root.logsOpen = !root.logsOpen
                            // Fetched immediately on opening rather than at the
                            // next tick: two seconds of an empty box reads as
                            // "there is no log".
                            if (root.logsOpen) root.pumpLogs()
                        }
                    }
                }

                Rectangle {
                    visible: root.logsOpen
                    Layout.fillWidth: true
                    Layout.preferredWidth: 0
                    Layout.preferredHeight: root.sz(220)
                    color: cPanel
                    radius: 8

                    ListView {
                        id: logView
                        anchors.fill: parent
                        anchors.margins: 8
                        clip: true
                        spacing: 2
                        model: root.logLines

                        // Follow the tail, but only while the reader is already
                        // at the bottom: yanking the view down under somebody
                        // who has scrolled up to read something is the single
                        // most annoying thing a log pane can do.
                        property bool atEnd: true
                        onContentYChanged: atEnd = (contentY + height >= contentHeight - 24)
                        onCountChanged: if (atEnd) positionViewAtEnd()

                        delegate: RowLayout {
                            width: ListView.view.width
                            spacing: 8
                            Text {
                                text: root.ago(modelData.t)
                                color: cAsh
                                font.family: "monospace"; font.pixelSize: root.fs(9)
                                Layout.preferredWidth: root.sz(34)
                                horizontalAlignment: Text.AlignRight
                            }
                            Text {
                                text: modelData.msg || ""
                                color: root.levelColour(modelData.level)
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                Layout.preferredWidth: root.sz(200)
                                elide: Text.ElideRight
                            }
                            Text {
                                text: modelData.attrs || ""
                                color: cAsh
                                font.family: "monospace"; font.pixelSize: root.fs(10)
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                                Layout.preferredWidth: 0
                            }
                        }
                    }





                    Text {
                        visible: root.logLines.length === 0
                        anchors.fill: parent
                        anchors.margins: 16
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                        wrapMode: Text.WordWrap
                        text: root.logProblem !== "" ? root.logProblem
                                                     : "nothing logged since this pane was opened"
                        color: root.logProblem !== "" ? cAmber : cAsh
                        font.family: "monospace"; font.pixelSize: root.fs(10)
                    }
                }
            }
        }
    }

    // ========================================================================
    // Agents (docs/agents.md): the Claude Code sessions on the owner's
    // machines, as the Android app shows them. Everything that touches the
    // network runs in shrooms_core on threads of its own; this only reads what
    // it collected, so no call here can stall the view.
    // ========================================================================

    property bool agentsOpen: false
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
    property string voiceLang: "cs"

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
        hosts.sort(function(a, b) { return a.name < b.name ? -1 : 1 })
        root.agentHosts = hosts
    }

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

    function openSession(h, s) {
        root.agentOpen = { address: h.address, name: h.name, mesh: h.mesh, session: s }
        root.agentEventsList = []
        root.agentEarlier = []
        root.agentStreaming = ""
        root.agentNext = 0
        root.chatStick = true
        root.agentAttached = []
        chatModel.clear()
        agentCall("agentWatch", [h.address, s])
        // After the first paint: a call during construction of what it fills
        // freezes the view.
        Qt.callLater(function() {
            if (!root.agentOpen) return
            var r = agentCall("agentGet", [root.agentOpen.address,
                "/v1/sessions/" + root.agentOpen.session + "/history?limit=30"])
            if (r && r.history) { root.agentEarlier = r.history; rebuildChat() }
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
            if (e.kind === "answer" && e.data) answers[e.data.prompt] = (e.data.allow ? "allowed" : "denied") + (e.by ? " from " + e.by : "")
            if (e.kind === "stopped") lastStop = e.seq
        }
        var first = 0
        for (i = 0; i < evs.length; i++) if (epoch(evs[i].time)) { first = epoch(evs[i].time); break }
        for (i = 0; i < agentEarlier.length; i++) {
            var h = agentEarlier[i], ht = epoch(h.time)
            if (first && ht >= first) continue
            out.push({ key: "h" + i, kind: h.role === "user" ? "you" : "said", earlier: true, text: h.text, time: ht, by: "" })
        }
        var per = {}
        function add(e, item) {
            per[e.seq] = (per[e.seq] || 0) + 1
            item.key = "e" + e.seq + "-" + per[e.seq]
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
        if (root.chatStick) Qt.callLater(function() { chatList.positionViewAtEnd() })
    }
    function row(it) {
        return { key: it.key, kind: it.kind, text: it.text || "", by: it.by || "", time: it.time || 0,
                 earlier: !!it.earlier, error: !!it.error, pid: it.id || "", tool: it.tool || "",
                 description: it.description || "", open: !!it.open, answer: it.answer || "",
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
            if (agentCall("agentRecord", ["stop", agentOpen.address, agentOpen.session, voiceLang]) !== null)
                root.agentTranscribing = true
        }
    }
    function cycleVoiceLang() {
        var ls = ["cs", "en", "auto"]
        root.voiceLang = ls[(ls.indexOf(voiceLang) + 1) % ls.length]
        savePref("voice_lang", voiceLang)
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
    function setAutoApprove(on) {
        if (!agentOpen) return
        if (agentCall("agentPost", [agentOpen.address, "/v1/sessions/" + agentOpen.session + "/settings",
                                    JSON.stringify({ auto_approve: on })]) !== null) refreshAgents()
    }

    ListModel { id: chatModel }
    // For the harness, which cannot reach an id inside this component.
    function chatModelCount() { return chatModel.count }
    function chatModelAt(i) { return chatModel.get(i) }
    function composerText() { return composer.text }

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
        onTriggered: { root.pumpAgent(); root.pumpJobs() }
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

        // A click must not fall through to the mesh view underneath.
        MouseArea { anchors.fill: parent; acceptedButtons: Qt.AllButtons; onWheel: function(w) { w.accepted = true } }

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
                    Lnk { text: "back to the mesh"; base: cAsh; onClicked: { root.agentsOpen = false } }
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
                    delegate: Column {
                        id: hostCol
                        required property var modelData
                        width: hostList.width
                        spacing: root.sz(6)
                        RowLayout {
                            width: parent.width
                            spacing: 8
                            Rectangle { width: root.sz(9); height: width; radius: width / 2; color: "transparent"; border.width: 2; border.color: root.meshTint(hostCol.modelData.mesh) }
                            Text { text: hostCol.modelData.name; color: cBone; font.family: "monospace"; font.pixelSize: root.fs(14) }
                            Text { text: hostCol.modelData.mesh; color: root.meshTint(hostCol.modelData.mesh); font.family: "monospace"; font.pixelSize: root.fs(10) }
                            Item { Layout.fillWidth: true }
                            Lnk { text: "+ session"; onClicked: { newSession.host = hostCol.modelData; root.agentCreating = true } }
                        }
                        Text {
                            visible: hostCol.modelData.sessions.length === 0
                            text: "no sessions yet"; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10)
                        }
                        Repeater {
                            model: hostCol.modelData.sessions
                            delegate: Rectangle {
                                id: srow
                                required property var modelData
                                readonly property bool isOpen: root.agentOpen !== null && root.agentOpen.address === hostCol.modelData.address && root.agentOpen.session === srow.modelData.name
                                width: hostCol.width
                                height: sCol.implicitHeight + root.sz(16)
                                radius: root.sz(8)
                                color: isOpen ? Qt.rgba(0.21, 0.94, 0.63, 0.08) : cPanel
                                border.width: 1
                                border.color: srow.modelData.state === "waiting" ? cAmber : (isOpen ? cPhosphor : cLine)
                                Column {
                                    id: sCol
                                    anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
                                    anchors.margins: root.sz(8)
                                    spacing: 3
                                    RowLayout {
                                        width: parent.width
                                        Text { text: srow.modelData.name; color: cBone; font.family: "monospace"; font.pixelSize: root.fs(12); elide: Text.ElideRight; Layout.fillWidth: true }
                                        Pulse { visible: srow.modelData.state !== "idle"; tint: srow.modelData.state === "waiting" ? cAmber : cPhosphor }
                                        Text {
                                            text: srow.modelData.state === "waiting" ? "NEEDS YOU" : (srow.modelData.state === "working" ? "WORKING" : (srow.modelData.running ? "idle" : "asleep"))
                                            color: srow.modelData.state === "waiting" ? cAmber : (srow.modelData.state === "working" ? cPhosphor : cAsh)
                                            font.family: "monospace"; font.pixelSize: root.fs(9); font.letterSpacing: 1
                                        }
                                    }
                                    Text {
                                        visible: (srow.modelData.preview || "") !== ""
                                        width: parent.width
                                        text: srow.modelData.preview || ""
                                        color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10)
                                        wrapMode: Text.Wrap; maximumLineCount: 2; elide: Text.ElideRight
                                    }
                                    Text {
                                        width: parent.width
                                        text: [root.clock(root.epoch(srow.modelData.last_time)),
                                               root.contextLabel(srow.modelData.context_used, srow.modelData.context_window),
                                               root.shortModel(srow.modelData.model),
                                               srow.modelData.auto_approve ? "auto-approve" : ""].filter(function(x) { return x !== "" }).join("  ·  ")
                                        color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(9); elide: Text.ElideRight
                                    }
                                }
                                MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.openSession(hostCol.modelData, srow.modelData.name) }
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
                    CheckBox { id: nsAuto; text: "auto-approve — never ask, like --dangerously-skip-permissions"; contentItem: Text { leftPadding: nsAuto.indicator.width + 6; text: nsAuto.text; color: cAsh; font.family: "monospace"; font.pixelSize: root.fs(10); verticalAlignment: Text.AlignVCenter } }
                    RowLayout {
                        Lnk {
                            text: "create"
                            onClicked: {
                                var r = root.agentCall("agentPost", [newSession.host.address, "/v1/sessions",
                                    JSON.stringify({ name: nsName.text.trim(), dir: nsDir.text.trim(), auto_approve: nsAuto.checked })])
                                if (r !== null) {
                                    root.agentCreating = false
                                    root.refreshAgents()
                                    root.openSession(newSession.host, nsName.text.trim())
                                    nsName.text = ""
                                }
                            }
                        }
                        Lnk { text: "cancel"; base: cAsh; onClicked: root.agentCreating = false }
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
                            readonly property bool on: root.agentInfo !== null && !!root.agentInfo.auto_approve
                            text: on ? "AUTO-APPROVE ON" : "asks first"
                            base: on ? cPhosphor : cAsh
                            onClicked: root.setAutoApprove(!on)
                        }
                        Lnk { visible: root.agentWorking; text: "stop"; base: cRust
                              onClicked: root.agentCall("agentPost", [root.agentOpen.address, "/v1/sessions/" + root.agentOpen.session + "/interrupt", ""]) }
                    }
                    Text {
                        text: root.agentOpen ? [root.agentOpen.name, root.agentOpen.mesh,
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

                ListView {
                    id: chatList
                    visible: !root.agentCreating && root.agentOpen !== null
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
                        visible: !root.chatStick
                        anchors.right: parent.right; anchors.bottom: parent.bottom; anchors.margins: root.sz(12)
                        width: root.sz(34); height: width; radius: width / 2
                        color: cPanel; border.color: cPhosphor
                        z: 5
                        Text { anchors.centerIn: parent; text: "↓"; color: cPhosphor; font.pixelSize: root.fs(16) }
                        MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor
                                    onClicked: { root.chatStick = true; chatList.positionViewAtEnd() } }
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
                                    text: root.agentStreaming + " ▍"
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
                                border.color: crow.kind === "you" ? Qt.rgba(0.21, 0.94, 0.63, 0.35) : cLine
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
                                        textFormat: crow.kind === "said" ? TextEdit.MarkdownText : TextEdit.PlainText
                                        text: crow.text
                                        color: cBone; selectionColor: Qt.rgba(0.21, 0.94, 0.63, 0.35)
                                        font.family: "monospace"; font.pixelSize: root.fs(12)
                                        onLinkActivated: function(link) { root.openUrl(link) }
                                    }
                                }
                            }

                            Text {
                                visible: crow.kind === "tool"
                                width: parent.width
                                text: "▸ " + crow.text
                                color: cViolet; font.family: "monospace"; font.pixelSize: root.fs(11)
                                elide: Text.ElideRight
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
                    // The language to transcribe in: naming it halves the time,
                    // since detecting it costs the model a whole extra pass.
                    Lnk {
                        text: root.voiceLang.toUpperCase()
                        base: root.voiceLang === "auto" ? cAsh : cSky
                        font.pixelSize: root.fs(10)
                        onClicked: root.cycleVoiceLang()
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
