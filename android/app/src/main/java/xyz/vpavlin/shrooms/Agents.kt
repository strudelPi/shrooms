package xyz.vpavlin.shrooms

import androidx.activity.compose.BackHandler
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import kotlin.math.PI

/** A machine running shrooms-agent, reached at [address] on [mesh]. */
data class AgentHost(
    val name: String,
    val mesh: String,
    val address: String,
    val sessions: List<AgentSession>,
)

/** A session to open straight away — from a notification. */
data class OpenSession(val address: String, val host: String, val mesh: String, val session: String)

/**
 * Finds every agent among the peers: one probe per online device on each of
 * its addresses, first answer wins. A probe rather than reading what peers
 * announce, because announcing bound ports is off by default (ADR-026) and an
 * agent should be found with nothing configured.
 */
suspend fun discoverAgents(peers: List<Peer>, byName: List<String> = emptyList()): List<AgentHost> =
    withContext(Dispatchers.IO) {
        val fromPeers = peers.filter { it.online }.groupBy { it.name }.map { (name, ps) ->
            async {
                ps.firstNotNullOfOrNull { p ->
                    listOf(p.overlay, p.overlayV4).filter { it.isNotEmpty() }.firstNotNullOfOrNull { a ->
                        runCatching { AgentHost(name, p.mesh, a, AgentClient(a).sessions(3000)) }.getOrNull()
                    }
                }
            }
        }
        val named = byName.filter { n -> peers.none { it.name == n.substringBefore('.') } }.map { n ->
            async {
                meshAddressesOf(n).firstNotNullOfOrNull { a ->
                    runCatching {
                        AgentHost(n.substringBefore('.'), n.substringAfter('.', "").substringBefore('.'), a,
                            AgentClient(a).sessions(3000))
                    }.getOrNull()
                }
            }
        }
        (fromPeers + named).awaitAll().filterNotNull().distinctBy { it.name }.sortedBy { it.name }
    }

/**
 * A machine named rather than found among the peers — `laptop.office.mesh` —
 * resolved by whatever answers .mesh names on this phone (the shrooms app's
 * resolver, when its tunnel is up), keeping only answers that are mesh
 * addresses: a name is not trusted to point inside the tunnel just because it
 * ends in .mesh.
 */
fun meshAddressesOf(name: String): List<String> =
    runCatching { java.net.InetAddress.getAllByName(name).map { it.hostAddress ?: "" } }
        .getOrDefault(emptyList())
        .map { it.substringBefore('%') }
        .filter { AgentClient.isMeshAddress(it) }

// --- the look ---------------------------------------------------------------

/**
 * The same spores the mesh screen draws, faint, drifting behind everything:
 * the agents are part of the same app, not a utility bolted onto it.
 */
@Composable
private fun SporeBackdrop(alpha: Float = 0.16f) {
    val motes = remember { sporeField(26) }
    val t by rememberInfiniteTransition(label = "agents-spores").animateFloat(
        initialValue = 0f, targetValue = 2f * PI.toFloat(),
        animationSpec = infiniteRepeatable(tween(60_000, easing = LinearEasing), RepeatMode.Restart),
        label = "drift",
    )
    Canvas(Modifier.fillMaxSize()) { drawSpores(motes, t, alpha) }
}

/** A spore that breathes: something is happening. */
@Composable
private fun Pulse(colour: Color, size: Int = 8) {
    val a by rememberInfiniteTransition(label = "pulse").animateFloat(
        0.25f, 1f, infiniteRepeatable(tween(900), RepeatMode.Reverse), label = "a",
    )
    Box(Modifier.size(size.dp).background(colour.copy(alpha = a), CircleShape))
}

private fun meshColour(mesh: String, meshes: List<String>): Color =
    meshTints[meshes.indexOf(mesh).coerceAtLeast(0) % meshTints.size]

private val clock = SimpleDateFormat("HH:mm", Locale.getDefault())
private val dated = SimpleDateFormat("d MMM HH:mm", Locale.getDefault())

/** "14:02" today, "2 Oct 14:02" otherwise. */
fun whenSaid(ms: Long): String {
    if (ms <= 0) return ""
    val day = 24 * 3600 * 1000L
    val tz = java.util.TimeZone.getDefault()
    val today = (System.currentTimeMillis() + tz.getOffset(System.currentTimeMillis())) / day
    val that = (ms + tz.getOffset(ms)) / day
    return (if (that == today) clock else dated).format(Date(ms))
}

/** "45% of 1M", or "" when the agent has not said. */
fun contextLabel(used: Long, window: Long): String {
    if (used <= 0 || window <= 0) return ""
    val pct = (used * 100 / window).coerceIn(0, 100)
    val w = if (window >= 1_000_000) "${window / 1_000_000}M" else "${window / 1000}k"
    return "$pct% of $w"
}

/** "claude-opus-5[1m]" → "opus-5 1m". */
fun shortModel(m: String): String =
    m.removePrefix("claude-").replace("[", " ").replace("]", "").replace(Regex("""-\d{8}$"""), "")

private fun contextColour(used: Long, window: Long): Color {
    val f = if (window > 0) used.toFloat() / window else 0f
    return when {
        f >= 0.9f -> Palette.Rust
        f >= 0.7f -> Palette.Amber
        else -> Palette.Phosphor
    }
}

@Composable
private fun Link(text: String, colour: Color = Palette.Phosphor, onClick: () -> Unit) =
    Text(text, style = MaterialTheme.typography.labelSmall, color = colour,
        modifier = Modifier.clickable { onClick() }.padding(horizontal = 8.dp, vertical = 10.dp))

// --- the list ---------------------------------------------------------------

@Composable
fun AgentsScreen(peers: List<Peer>, onClose: () -> Unit, initial: OpenSession? = null) {
    val ctx = LocalContext.current
    var hosts by remember { mutableStateOf<List<AgentHost>?>(null) }
    var open by remember(initial) { mutableStateOf(initial) }
    var creatingOn by remember { mutableStateOf<AgentHost?>(null) }
    var refresh by remember { mutableStateOf(0) }
    // Machines added by name, kept across launches.
    val prefs = remember { ctx.getSharedPreferences("agents", android.content.Context.MODE_PRIVATE) }
    var named by remember { mutableStateOf(prefs.getStringSet("named", emptySet())!!.sorted()) }
    var addingMachine by remember { mutableStateOf(false) }

    // Refreshed while the list is on screen, so a session that starts waiting
    // for an answer shows it without a pull. What is found is remembered for
    // the notification watcher (AgentWatch).
    LaunchedEffect(refresh, open, named) {
        if (open != null) return@LaunchedEffect
        while (isActive) {
            val found = discoverAgents(peers, named)
            hosts = found
            if (found.isNotEmpty()) AgentHosts.save(ctx, found.map { AgentHosts.Host(it.name, it.mesh, it.address) })
            delay(10_000)
        }
    }

    Box(Modifier.fillMaxSize().background(Palette.Void)) {
        SporeBackdrop()
        val o = open
        if (o != null) {
            BackHandler { open = null }
            SessionScreen(o, onBack = { open = null; refresh++ })
            return@Box
        }
        BackHandler { if (creatingOn != null) creatingOn = null else onClose() }

        Column(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing).padding(horizontal = 20.dp)) {
            Spacer(Modifier.height(12.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                Pulse(Palette.Phosphor, 10)
                Spacer(Modifier.width(10.dp))
                Text("AGENTS", style = MaterialTheme.typography.labelSmall, color = Palette.Phosphor)
            }
            // Actions on a line of their own, so nothing has to squeeze in
            // beside the title.
            Row(Modifier.padding(top = 2.dp)) {
                Link("refresh") { refresh++; hosts = null }
                Link(if (addingMachine) "cancel" else "+ machine") { addingMachine = !addingMachine }
                Spacer(Modifier.weight(1f))
                Link("close", Palette.Ash) { onClose() }
            }

            if (addingMachine) {
                var adding by remember { mutableStateOf("") }
                Field("machine, e.g. laptop.office.mesh", adding) { adding = it.trim() }
                Row {
                    Link("add", if (adding.isNotEmpty()) Palette.Phosphor else Palette.Ash) {
                        if (adding.isNotEmpty()) {
                            named = (named + adding).distinct().sorted()
                            prefs.edit().putStringSet("named", named.toSet()).apply()
                            addingMachine = false
                            hosts = null
                        }
                    }
                    if (named.isNotEmpty()) Link("forget ${named.size} named", Palette.Ash) {
                        named = emptyList()
                        prefs.edit().remove("named").apply()
                    }
                }
            }

            val c = creatingOn
            if (c != null) {
                NewSession(c, onDone = { creatingOn = null; refresh++ })
                return@Column
            }

            val hs = hosts
            when {
                hs == null -> Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 20.dp)) {
                    Pulse(Palette.Amber); Spacer(Modifier.width(10.dp)); Label("looking for agents on the mesh…")
                }
                hs.isEmpty() -> Text(
                    "No agents found. An agent is shrooms-agent running on one of your machines, " +
                        "on its mesh address, port $AGENT_PORT. Only online peers are checked — " +
                        "or add a machine by name.",
                    style = MaterialTheme.typography.bodySmall, color = Palette.Ash,
                    modifier = Modifier.padding(top = 20.dp),
                )
            }

            val meshes = hs.orEmpty().map { it.mesh }.distinct().sorted()
            LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(top = 8.dp)) {
                for (h in hs.orEmpty()) {
                    item(key = "h-" + h.name) {
                        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 14.dp)) {
                            Box(Modifier.size(10.dp).border(2.dp, meshColour(h.mesh, meshes), CircleShape))
                            Spacer(Modifier.width(10.dp))
                            Text(h.name, style = MaterialTheme.typography.titleMedium, color = Palette.Bone)
                            Spacer(Modifier.width(8.dp))
                            Text(h.mesh, style = MaterialTheme.typography.labelSmall, color = meshColour(h.mesh, meshes))
                            Spacer(Modifier.weight(1f))
                            Link("+ session") { creatingOn = h }
                        }
                    }
                    if (h.sessions.isEmpty()) {
                        item(key = "e-" + h.name) { Label("no sessions yet") }
                    }
                    itemsIndexed(h.sessions, key = { _, s -> h.name + "/" + s.name }) { _, s ->
                        SessionRow(s) { open = OpenSession(h.address, h.name, h.mesh, s.name) }
                    }
                }
                item(key = "bottom") { Spacer(Modifier.height(24.dp)) }
            }
        }
    }
}

@Composable
private fun SessionRow(s: AgentSession, onOpen: () -> Unit) {
    val (badge, colour) = when (s.state) {
        "waiting" -> "NEEDS YOU" to Palette.Amber
        "working" -> "WORKING" to Palette.Phosphor
        else -> (if (s.running) "idle" else "asleep") to Palette.Ash
    }
    Column(
        Modifier.fillMaxWidth()
            .background(Palette.Panel.copy(alpha = 0.85f), RoundedCornerShape(12.dp))
            .border(1.dp, if (s.state == "waiting") Palette.Amber else Palette.Line, RoundedCornerShape(12.dp))
            .clickable { onOpen() }
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(s.name, style = MaterialTheme.typography.titleMedium, color = Palette.Bone,
                maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (s.state != "idle") { Pulse(colour); Spacer(Modifier.width(6.dp)) }
            Text(badge, style = MaterialTheme.typography.labelSmall, color = colour)
        }
        if (s.preview.isNotEmpty()) {
            Text(s.preview, style = MaterialTheme.typography.bodySmall, color = Palette.Ash,
                maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        val meta = listOf(whenSaid(s.lastTime), contextLabel(s.contextUsed, s.contextWindow),
            shortModel(s.model), if (s.autoApprove) "auto-approve" else "").filter { it.isNotEmpty() }
        Text((meta + s.dir.replace(Regex("^/home/[^/]+"), "~")).joinToString("  ·  "),
            style = MaterialTheme.typography.labelSmall, color = Palette.Ash,
            maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun NewSession(h: AgentHost, onDone: () -> Unit) {
    var name by remember { mutableStateOf("") }
    var dir by remember { mutableStateOf("~/") }
    var auto by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    Column(verticalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.padding(top = 16.dp)) {
        Text("NEW SESSION ON ${h.name.uppercase()}", style = MaterialTheme.typography.labelSmall, color = Palette.Phosphor)
        Label("A name and a directory on that machine, like a cl session. ~ is that machine's home.")
        Field("name", name) { name = it }
        Field("directory", dir) { dir = it }
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.clickable { auto = !auto }) {
            Box(Modifier.size(12.dp).border(1.dp, if (auto) Palette.Phosphor else Palette.Ash, RoundedCornerShape(3.dp))
                .background(if (auto) Palette.Phosphor else Color.Transparent, RoundedCornerShape(3.dp)))
            Spacer(Modifier.width(10.dp))
            Text("auto-approve — never ask, like --dangerously-skip-permissions",
                style = MaterialTheme.typography.bodySmall, color = if (auto) Palette.Bone else Palette.Ash)
        }
        if (error.isNotEmpty()) Text(error, style = MaterialTheme.typography.bodySmall, color = Palette.Rust)
        Action("CREATE", enabled = !busy && name.isNotBlank() && dir.isNotBlank()) {
            busy = true
            scope.launch {
                val r = withContext(Dispatchers.IO) {
                    runCatching { AgentClient(h.address).create(name.trim(), dir.trim(), auto) }
                }
                busy = false
                r.onSuccess { onDone() }.onFailure { error = it.message ?: "could not create it" }
            }
        }
        Link("cancel", Palette.Ash) { onDone() }
    }
}

@Composable
private fun Field(label: String, value: String, onChange: (String) -> Unit) {
    OutlinedTextField(
        value = value, onValueChange = onChange, singleLine = true,
        label = { Text(label) },
        textStyle = MaterialTheme.typography.bodyMedium.copy(color = Palette.Bone),
        colors = OutlinedTextFieldDefaults.colors(
            focusedBorderColor = Palette.Phosphor, unfocusedBorderColor = Palette.Line,
            focusedLabelColor = Palette.Phosphor, unfocusedLabelColor = Palette.Ash,
            cursorColor = Palette.Phosphor,
        ),
        modifier = Modifier.fillMaxWidth(),
    )
}

// --- a conversation ---------------------------------------------------------

@Composable
private fun SessionScreen(o: OpenSession, onBack: () -> Unit) {
    val client = remember(o.address) { AgentClient(o.address) }
    val events = remember(o) { mutableStateListOf<AgentEvent>() }
    var earlier by remember(o) { mutableStateOf<List<Earlier>>(emptyList()) }
    var info by remember(o) { mutableStateOf<AgentSession?>(null) }
    var streaming by remember(o) { mutableStateOf("") }
    var connError by remember { mutableStateOf("") }
    var input by remember { mutableStateOf("") }
    var actionError by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()
    val list = rememberLazyListState()

    // Not notified about while it is on screen — and only while the app is in
    // front: a phone in a pocket on this screen should still buzz.
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    DisposableEffect(o, lifecycle) {
        val key = "${o.address}/${o.session}"
        val obs = LifecycleEventObserver { _, e ->
            when (e) {
                Lifecycle.Event.ON_RESUME -> AgentWatch.visible = key
                Lifecycle.Event.ON_PAUSE -> if (AgentWatch.visible == key) AgentWatch.visible = null
                else -> {}
            }
        }
        lifecycle.addObserver(obs)
        if (lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)) AgentWatch.visible = key
        onDispose {
            lifecycle.removeObserver(obs)
            if (AgentWatch.visible == key) AgentWatch.visible = null
        }
    }

    // What came before this agent had the conversation, once.
    LaunchedEffect(o) {
        earlier = withContext(Dispatchers.IO) { runCatching { client.history(o.session, 30) }.getOrDefault(emptyList()) }
    }
    // The session's own figures — context, model, auto-approve — kept fresh.
    LaunchedEffect(o) {
        while (isActive) {
            withContext(Dispatchers.IO) { runCatching { client.sessions() } }.getOrNull()
                ?.firstOrNull { it.name == o.session }?.let { info = it }
            delay(10_000)
        }
    }
    // Follow the session for as long as it is on screen. A dropped connection
    // is normal on mobile data: reconnect from the last event seen, which the
    // server keeps, so nothing is lost or shown twice.
    LaunchedEffect(o) {
        val after = java.util.concurrent.atomic.AtomicLong(0)
        while (isActive) {
            val r = withContext(Dispatchers.IO) {
                runCatching {
                    client.follow(o.session, after.get(), stop = { !isActive }) { e ->
                        if (e.kind != "partial") after.set(e.seq)
                        scope.launch {
                            connError = ""
                            when {
                                e.kind == "partial" -> streaming += e.data.optString("text")
                                else -> {
                                    // The whole message replaces what was streamed of it.
                                    val t = e.data.optString("type")
                                    if (e.kind == "claude" && (t == "assistant" || t == "result")) streaming = ""
                                    events += e
                                }
                            }
                        }
                    }
                }
            }
            r.onFailure { connError = it.message ?: "connection lost" }
            delay(2000)
        }
    }

    val items = remember(events.size, earlier) { AgentChat.items(events.toList(), earlier) }
    val keys = remember(items) { keysOf(items) }
    val last = items.lastOrNull()
    val working = info?.state == "working" ||
        (items.isNotEmpty() && last !is ChatItem.Done && last !is ChatItem.Stopped &&
            last !is ChatItem.Earlier && last !is ChatItem.Note && !(last is ChatItem.Prompt && !last.open))
    val waiting = items.any { it is ChatItem.Prompt && it.open }

    // Stick to the bottom only when already there; otherwise leave the reader
    // where they are. Jumping on every event is what threw the view about.
    val atBottom by remember {
        derivedStateOf {
            val li = list.layoutInfo
            li.totalItemsCount == 0 || (li.visibleItemsInfo.lastOrNull()?.index ?: 0) >= li.totalItemsCount - 2
        }
    }
    var placed by remember(o) { mutableStateOf(false) }
    val total = items.size + 1 // + the live row
    LaunchedEffect(items.size, streaming.length / 80, earlier.size) {
        if (total <= 1) return@LaunchedEffect
        if (!placed || atBottom) {
            list.scrollToItem(total - 1)
            placed = true
        }
    }

    val i = info
    Column(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing).imePadding()) {
        // Header: the name on its own line, the facts under it, then actions.
        Column(Modifier.padding(start = 12.dp, end = 12.dp, top = 8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("‹", style = MaterialTheme.typography.titleMedium.copy(fontSize = 22.sp), color = Palette.Phosphor,
                    modifier = Modifier.clickable { onBack() }.padding(horizontal = 8.dp, vertical = 4.dp))
                Text(o.session, style = MaterialTheme.typography.titleMedium, color = Palette.Bone,
                    maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                when {
                    waiting -> { Pulse(Palette.Amber); Spacer(Modifier.width(6.dp)); Label("NEEDS YOU") }
                    working -> { Pulse(Palette.Phosphor); Spacer(Modifier.width(6.dp)); Label("WORKING") }
                }
            }
            val facts = listOf(o.host, o.mesh, shortModel(i?.model ?: ""),
                contextLabel(i?.contextUsed ?: 0, i?.contextWindow ?: 0)).filter { it.isNotEmpty() }
            Text(facts.joinToString("  ·  "), style = MaterialTheme.typography.labelSmall, color = Palette.Ash,
                maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 30.dp))
            if (i != null && i.contextWindow > 0) {
                val f = (i.contextUsed.toFloat() / i.contextWindow).coerceIn(0f, 1f)
                Box(Modifier.padding(start = 30.dp, top = 6.dp, end = 8.dp).fillMaxWidth().height(2.dp).background(Palette.Line)) {
                    Box(Modifier.fillMaxWidth(f).height(2.dp).background(contextColour(i.contextUsed, i.contextWindow)))
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(start = 22.dp)) {
                val auto = i?.autoApprove == true
                Link(if (auto) "AUTO-APPROVE ON" else "asks first", if (auto) Palette.Phosphor else Palette.Ash) {
                    scope.launch {
                        withContext(Dispatchers.IO) { runCatching { client.setAutoApprove(o.session, !auto) } }
                            .onSuccess { info = i?.copy(autoApprove = !auto) }
                            .onFailure { actionError = it.message ?: "could not change it" }
                    }
                }
                Spacer(Modifier.weight(1f))
                if (working) Link("stop", Palette.Rust) {
                    scope.launch(Dispatchers.IO) { runCatching { client.interrupt(o.session) } }
                }
            }
            if (connError.isNotEmpty()) {
                Text("reconnecting — $connError", style = MaterialTheme.typography.labelSmall, color = Palette.Amber,
                    modifier = Modifier.padding(start = 30.dp))
            }
        }

        LazyColumn(
            state = list,
            verticalArrangement = Arrangement.spacedBy(10.dp),
            modifier = Modifier.weight(1f).padding(horizontal = 14.dp),
        ) {
            // Stable keys: history arriving after the events, or a reply
            // growing, must not move what the reader is looking at.
            itemsIndexed(items, key = { idx, _ -> keys[idx] }) { idx, item ->
                val prev = items.getOrNull(idx - 1)
                if (item is ChatItem.Earlier && prev !is ChatItem.Earlier) Label("— earlier, from the transcript —")
                if (item !is ChatItem.Earlier && prev is ChatItem.Earlier) Label("— on this phone —")
                ChatRow(item) { prompt, allow ->
                    scope.launch(Dispatchers.IO) {
                        runCatching { client.answer(o.session, prompt, allow) }
                            .onFailure { actionError = it.message ?: "could not answer" }
                    }
                }
            }
            item(key = "live") {
                when {
                    streaming.isNotEmpty() -> Bubble(Palette.Panel) {
                        MarkdownText(streaming)
                        Text("▍", color = Palette.Phosphor, style = MaterialTheme.typography.bodyMedium)
                    }
                    working && !waiting -> Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(4.dp)) {
                        Pulse(Palette.Phosphor); Spacer(Modifier.width(8.dp)); Label("thinking…")
                    }
                    else -> Spacer(Modifier.height(4.dp))
                }
            }
        }

        if (actionError.isNotEmpty()) {
            Text(actionError, style = MaterialTheme.typography.bodySmall, color = Palette.Rust,
                modifier = Modifier.padding(horizontal = 20.dp).clickable { actionError = "" })
        }
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(10.dp)) {
            Box(Modifier.weight(1f)) {
                OutlinedTextField(
                    value = input, onValueChange = { input = it },
                    placeholder = { Text("message", color = Palette.Ash) },
                    textStyle = MaterialTheme.typography.bodyMedium.copy(color = Palette.Bone),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedBorderColor = Palette.Phosphor, unfocusedBorderColor = Palette.Line,
                        cursorColor = Palette.Phosphor,
                    ),
                    shape = RoundedCornerShape(14.dp),
                    maxLines = 6,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            Spacer(Modifier.width(8.dp))
            Box(
                Modifier.size(44.dp)
                    .background(if (input.isNotBlank()) Palette.Phosphor else Palette.Line, CircleShape)
                    .clickable(enabled = input.isNotBlank()) {
                        val text = input.trim()
                        input = ""
                        actionError = ""
                        scope.launch(Dispatchers.IO) {
                            runCatching { client.send(o.session, text) }
                                .onFailure { actionError = it.message ?: "could not send"; input = text }
                        }
                    },
                contentAlignment = Alignment.Center,
            ) { Text("↑", color = Palette.Void, style = MaterialTheme.typography.titleMedium) }
        }
    }
}

/**
 * Keys that do not change when an item does — a prompt being answered, say —
 * so the list never re-lays itself out under the reader. One event can make
 * several items (text and a tool use in one message), so an item is its
 * event's number and its position among that event's items.
 */
fun keysOf(items: List<ChatItem>): List<String> {
    val seen = mutableMapOf<Long, Int>()
    var earlier = 0
    return items.map {
        if (it is ChatItem.Earlier) "h${earlier++}"
        else { val n = seen.merge(it.seq, 1, Int::plus)!!; "e${it.seq}-$n" }
    }
}

@Composable
private fun Bubble(bg: Color, border: Color = Palette.Line, content: @Composable () -> Unit) {
    Column(Modifier.fillMaxWidth().background(bg.copy(alpha = 0.9f), RoundedCornerShape(12.dp))
        .border(1.dp, border, RoundedCornerShape(12.dp)).padding(12.dp)) { content() }
}

@Composable
private fun Stamp(text: String, colour: Color = Palette.Ash) {
    Text(text, style = MaterialTheme.typography.labelSmall, color = colour, modifier = Modifier.padding(bottom = 4.dp))
}

@Composable
private fun ChatRow(item: ChatItem, onAnswer: (String, Boolean) -> Unit) {
    when (item) {
        is ChatItem.You -> Bubble(Palette.Phosphor.copy(alpha = 0.08f), Palette.Phosphor.copy(alpha = 0.35f)) {
            Stamp(listOf("YOU", item.by, whenSaid(item.time)).filter { it.isNotEmpty() }.joinToString("  ·  "), Palette.Phosphor)
            Text(item.text, style = MaterialTheme.typography.bodyMedium, color = Palette.Bone)
        }
        is ChatItem.Said -> Bubble(Palette.Panel) {
            Stamp(whenSaid(item.time))
            MarkdownText(item.text)
        }
        is ChatItem.Earlier -> if (item.user) {
            Bubble(Palette.Phosphor.copy(alpha = 0.05f), Palette.Line) {
                Stamp(listOf("YOU", whenSaid(item.time)).joinToString("  ·  "), Palette.Phosphor.copy(alpha = 0.6f))
                Text(item.text, style = MaterialTheme.typography.bodyMedium, color = Palette.Bone.copy(alpha = 0.8f))
            }
        } else {
            Bubble(Palette.Panel.copy(alpha = 0.6f)) {
                Stamp(whenSaid(item.time))
                MarkdownText(item.text)
            }
        }
        is ChatItem.Tool -> Row(verticalAlignment = Alignment.Top) {
            Text("▸ ", style = MaterialTheme.typography.bodySmall, color = Palette.Violet)
            Text(buildAnnotatedString {
                withStyle(SpanStyle(color = Palette.Violet, fontWeight = FontWeight.Medium)) { append(item.name) }
                append("  ")
                withStyle(SpanStyle(color = Palette.Sky)) { append(item.summary) }
            }, style = MaterialTheme.typography.bodySmall, maxLines = 3, overflow = TextOverflow.Ellipsis)
        }
        is ChatItem.Output -> {
            var expanded by remember { mutableStateOf(false) }
            val firstLine = item.text.lineSequence().firstOrNull().orEmpty().take(120)
            val more = item.text.contains('\n') || item.text.length > 120
            Text(
                if (expanded) item.text else firstLine + if (more) "  …" else "",
                style = MaterialTheme.typography.bodySmall,
                color = if (item.error) Palette.Rust else Palette.Ash,
                modifier = Modifier.clickable(enabled = more) { expanded = !expanded }.padding(start = 16.dp),
            )
        }
        is ChatItem.Prompt -> PromptCard(item, onAnswer)
        is ChatItem.Done -> Label("— ${item.note}${whenSaid(item.time).let { if (it.isEmpty()) "" else "  ·  $it" }}")
        is ChatItem.Stopped -> Label("— asleep; the next message wakes it")
        is ChatItem.Note -> Label("— ${item.text}")
    }
}

/**
 * The reason this screen exists: something on another machine wants to run,
 * and waits for you. What it would do is shown in full before the buttons —
 * the summary is what you are saying yes to.
 */
@Composable
private fun PromptCard(p: ChatItem.Prompt, onAnswer: (String, Boolean) -> Unit) {
    Column(
        Modifier.fillMaxWidth()
            .background(Palette.Panel, RoundedCornerShape(12.dp))
            .border(1.dp, if (p.open) Palette.Amber else Palette.Line, RoundedCornerShape(12.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (p.open) { Pulse(Palette.Amber); Spacer(Modifier.width(8.dp)) }
            Text(if (p.open) "${p.tool.uppercase()} WANTS TO RUN" else p.tool,
                style = MaterialTheme.typography.labelSmall, color = if (p.open) Palette.Amber else Palette.Ash)
        }
        CodeBox(p.summary)
        if (p.description.isNotEmpty() && p.description != p.summary) {
            Text(p.description, style = MaterialTheme.typography.bodySmall, color = Palette.Ash)
        }
        if (p.open) {
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Box(Modifier.weight(1f)) { Action("ALLOW", enabled = true) { onAnswer(p.id, true) } }
                Box(Modifier.weight(1f)) { Action("DENY", enabled = true, danger = true) { onAnswer(p.id, false) } }
            }
        } else {
            Label(p.answer)
        }
    }
}

// --- markdown ---------------------------------------------------------------

@Composable
private fun CodeBox(text: String) {
    Box(Modifier.fillMaxWidth().background(Palette.Void, RoundedCornerShape(8.dp))
        .border(1.dp, Palette.Line, RoundedCornerShape(8.dp))
        .horizontalScroll(rememberScrollState()).padding(10.dp)) {
        Text(text, style = MaterialTheme.typography.bodySmall, color = Palette.Chartreuse, softWrap = false)
    }
}

private fun spans(s: List<Markdown.Span>, base: Color = Palette.Bone): AnnotatedString = buildAnnotatedString {
    for (sp in s) {
        val style = SpanStyle(
            color = when {
                sp.code -> Palette.Chartreuse
                sp.link != null -> Palette.Sky
                sp.bold -> Color.White
                else -> base
            },
            fontWeight = if (sp.bold) FontWeight.Bold else null,
            fontStyle = if (sp.italic) FontStyle.Italic else null,
            background = if (sp.code) Palette.Void else Color.Unspecified,
            textDecoration = if (sp.link != null) TextDecoration.Underline else null,
        )
        withStyle(style) { append(sp.text) }
    }
}

/** Claude's markdown, in the app's own look. */
@Composable
fun MarkdownText(src: String) {
    val blocks = remember(src) { Markdown.parse(src) }
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        for (b in blocks) when (b) {
            is Markdown.Block.Heading -> Text(spans(b.text, Palette.Phosphor),
                style = MaterialTheme.typography.titleMedium.copy(fontSize = if (b.level <= 2) 15.sp else 13.sp),
                modifier = Modifier.padding(top = 4.dp))
            is Markdown.Block.Para -> Text(spans(b.text), style = MaterialTheme.typography.bodyMedium)
            is Markdown.Block.Item -> Row(Modifier.padding(start = (b.indent * 14).dp)) {
                Text(b.marker + " ", style = MaterialTheme.typography.bodyMedium, color = Palette.Phosphor)
                Text(spans(b.text), style = MaterialTheme.typography.bodyMedium)
            }
            is Markdown.Block.Quote -> Row {
                Box(Modifier.width(2.dp).height(18.dp).background(Palette.Violet))
                Spacer(Modifier.width(8.dp))
                Text(spans(b.text, Palette.Ash), style = MaterialTheme.typography.bodyMedium)
            }
            is Markdown.Block.Code -> CodeBox(b.text)
            is Markdown.Block.Table -> Column(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())
                .border(1.dp, Palette.Line, RoundedCornerShape(8.dp)).padding(8.dp)) {
                b.rows.forEachIndexed { r, row ->
                    Row {
                        row.forEach { cell ->
                            Text(spans(cell, if (r == 0) Palette.Phosphor else Palette.Bone),
                                style = MaterialTheme.typography.bodySmall,
                                modifier = Modifier.width(140.dp).padding(end = 10.dp, bottom = 4.dp))
                        }
                    }
                }
            }
            Markdown.Block.Rule -> Box(Modifier.fillMaxWidth().height(1.dp).background(Palette.Line))
        }
    }
}
