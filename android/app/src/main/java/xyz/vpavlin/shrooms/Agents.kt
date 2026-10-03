package xyz.vpavlin.shrooms

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** A machine running shrooms-agent, reached at [address] on [mesh]. */
data class AgentHost(
    val name: String,
    val mesh: String,
    val address: String,
    val sessions: List<AgentSession>,
)

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

@Composable
fun AgentsScreen(peers: List<Peer>, onClose: () -> Unit) {
    var hosts by remember { mutableStateOf<List<AgentHost>?>(null) }
    var open by remember { mutableStateOf<Pair<AgentHost, String>?>(null) }
    var creatingOn by remember { mutableStateOf<AgentHost?>(null) }
    var refresh by remember { mutableStateOf(0) }
    // Machines added by name, kept across launches.
    val prefs = androidx.compose.ui.platform.LocalContext.current
        .getSharedPreferences("agents", android.content.Context.MODE_PRIVATE)
    var named by remember { mutableStateOf(prefs.getStringSet("named", emptySet())!!.sorted()) }
    var adding by remember { mutableStateOf("") }

    // Refreshed while the list is on screen, so a session that starts waiting
    // for an answer shows it without a pull.
    LaunchedEffect(refresh, open, named) {
        if (open != null) return@LaunchedEffect
        while (isActive) {
            hosts = discoverAgents(peers, named)
            delay(10_000)
        }
    }

    val o = open
    if (o != null) {
        BackHandler { open = null }
        SessionScreen(o.first, o.second, onBack = { open = null })
        return
    }
    BackHandler { onClose() }

    Column(Modifier.fillMaxSize().padding(horizontal = 24.dp)) {
        Spacer(Modifier.height(32.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("AGENTS", style = MaterialTheme.typography.labelSmall, color = Palette.Bone,
                modifier = Modifier.weight(1f))
            Text("refresh", style = MaterialTheme.typography.bodySmall, color = Palette.Phosphor,
                modifier = Modifier.clickable { refresh++ }.padding(8.dp))
            Text("close", style = MaterialTheme.typography.bodySmall, color = Palette.Phosphor,
                modifier = Modifier.clickable { onClose() }.padding(8.dp))
        }
        Spacer(Modifier.height(16.dp))

        val hs = hosts
        when {
            hs == null -> Label("looking for agents on the mesh…")
            hs.isEmpty() -> Text(
                "No agents found. An agent is shrooms-agent running on one of your machines, " +
                    "listening on its mesh address (port $AGENT_PORT). Only online peers are checked.",
                style = MaterialTheme.typography.bodySmall, color = Palette.Ash,
            )
        }

        // By name, for a machine the roster does not show — and the only way in
        // the preview build, which has no roster of its own.
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 10.dp)) {
            Box(Modifier.weight(1f)) { Field("add a machine, e.g. laptop.office.mesh", adding) { adding = it.trim() } }
            Text("add", style = MaterialTheme.typography.bodySmall,
                color = if (adding.isNotEmpty()) Palette.Phosphor else Palette.Ash,
                modifier = Modifier.clickable(enabled = adding.isNotEmpty()) {
                    named = (named + adding).distinct().sorted()
                    prefs.edit().putStringSet("named", named.toSet()).apply()
                    adding = ""
                    hosts = null
                }.padding(10.dp))
        }
        if (named.isNotEmpty()) {
            Text("by name: " + named.joinToString(", ") + "   (clear)", style = MaterialTheme.typography.bodySmall,
                color = Palette.Ash, modifier = Modifier.clickable {
                    named = emptyList()
                    prefs.edit().remove("named").apply()
                }.padding(vertical = 4.dp))
        }

        val c = creatingOn
        if (c != null) {
            NewSession(c, onDone = { creatingOn = null; refresh++ })
            return@Column
        }

        LazyColumn(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            for (h in hs.orEmpty()) {
                item(key = "h-" + h.name) {
                    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 14.dp)) {
                        Text(h.name, style = MaterialTheme.typography.titleSmall, color = Palette.Bone)
                        Spacer(Modifier.width(8.dp))
                        Label(h.mesh)
                        Spacer(Modifier.weight(1f))
                        Text("new session", style = MaterialTheme.typography.bodySmall, color = Palette.Phosphor,
                            modifier = Modifier.clickable { creatingOn = h }.padding(6.dp))
                    }
                }
                if (h.sessions.isEmpty()) {
                    item(key = "e-" + h.name) { Label("no sessions yet") }
                }
                items(h.sessions, key = { h.name + "/" + it.name }) { s ->
                    SessionRow(s) { open = h to s.name }
                }
            }
        }
    }
}

@Composable
private fun SessionRow(s: AgentSession, onOpen: () -> Unit) {
    val (badge, colour) = when (s.state) {
        "waiting" -> "NEEDS YOU" to Palette.Amber
        "working" -> "working" to Palette.Phosphor
        else -> (if (s.running) "idle" else "asleep") to Palette.Ash
    }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier.fillMaxWidth()
            .background(Palette.Panel, RoundedCornerShape(10.dp))
            .border(1.dp, if (s.state == "waiting") Palette.Amber else Palette.Line, RoundedCornerShape(10.dp))
            .clickable { onOpen() }
            .padding(14.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(s.name, style = MaterialTheme.typography.bodyMedium, color = Palette.Bone)
            Text(s.dir, style = MaterialTheme.typography.bodySmall, color = Palette.Ash,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Text(badge, style = MaterialTheme.typography.labelSmall, color = colour)
    }
}

@Composable
private fun NewSession(h: AgentHost, onDone: () -> Unit) {
    var name by remember { mutableStateOf("") }
    var dir by remember { mutableStateOf("~/") }
    var error by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    BackHandler { onDone() }
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text("New session on ${h.name}", style = MaterialTheme.typography.titleSmall, color = Palette.Bone)
        Label("A session is a name and a directory on that machine, like a cl session. ~ is that machine's home.")
        Field("name", name) { name = it }
        Field("directory", dir) { dir = it }
        if (error.isNotEmpty()) Text(error, style = MaterialTheme.typography.bodySmall, color = Palette.Rust)
        Action("CREATE", enabled = !busy && name.isNotBlank() && dir.isNotBlank()) {
            busy = true
            scope.launch {
                val r = withContext(Dispatchers.IO) {
                    runCatching { AgentClient(h.address).create(name.trim(), dir.trim()) }
                }
                busy = false
                r.onSuccess { onDone() }.onFailure { error = it.message ?: "could not create it" }
            }
        }
        Text("cancel", style = MaterialTheme.typography.bodySmall, color = Palette.Ash,
            modifier = Modifier.clickable { onDone() }.padding(6.dp))
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

@Composable
private fun SessionScreen(h: AgentHost, session: String, onBack: () -> Unit) {
    val client = remember(h.address) { AgentClient(h.address) }
    val events = remember { mutableStateListOf<AgentEvent>() }
    var connError by remember { mutableStateOf("") }
    var input by remember { mutableStateOf("") }
    var sendError by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()
    val list = rememberLazyListState()

    // Follow the session for as long as it is on screen. A dropped connection
    // is normal on mobile data: reconnect from the last event seen, which the
    // server keeps, so nothing is lost or shown twice.
    LaunchedEffect(h.address, session) {
        // Advanced as each event arrives, on the reading thread, so a drop at
        // any point resumes exactly after the last event delivered.
        val after = java.util.concurrent.atomic.AtomicLong(0)
        while (isActive) {
            val r = withContext(Dispatchers.IO) {
                runCatching {
                    client.follow(session, after.get(), stop = { !isActive }) { e ->
                        after.set(e.seq)
                        scope.launch { events += e; connError = "" }
                    }
                }
            }
            r.onFailure { connError = it.message ?: "connection lost" }
            delay(2000)
        }
    }

    val items = remember(events.size) { AgentChat.items(events.toList()) }
    LaunchedEffect(items.size) { if (items.isNotEmpty()) list.animateScrollToItem(items.size - 1) }
    // A turn is in progress from the moment one is sent until it ends.
    val working = items.isNotEmpty() && items.last() !is ChatItem.Done && items.last() !is ChatItem.Stopped

    Column(Modifier.fillMaxSize().imePadding()) {
        Row(verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier.padding(start = 24.dp, end = 16.dp, top = 32.dp, bottom = 8.dp)) {
            Column(Modifier.weight(1f)) {
                Text(session, style = MaterialTheme.typography.titleSmall, color = Palette.Bone)
                Label("${h.name} · ${h.mesh}")
            }
            if (working) {
                Text("stop", style = MaterialTheme.typography.bodySmall, color = Palette.Rust,
                    modifier = Modifier.clickable {
                        scope.launch(Dispatchers.IO) { runCatching { client.interrupt(session) } }
                    }.padding(8.dp))
            }
            Text("back", style = MaterialTheme.typography.bodySmall, color = Palette.Phosphor,
                modifier = Modifier.clickable { onBack() }.padding(8.dp))
        }
        if (connError.isNotEmpty()) {
            Text("reconnecting: $connError", style = MaterialTheme.typography.bodySmall, color = Palette.Amber,
                modifier = Modifier.padding(horizontal = 24.dp))
        }
        LazyColumn(
            state = list,
            verticalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.weight(1f).padding(horizontal = 16.dp),
        ) {
            // By position: one event can make several items (text and a tool
            // use in one message), and positions never change — items only
            // ever arrive at the end.
            itemsIndexed(items, key = { i, _ -> i }) { _, item ->
                ChatRow(item) { prompt, allow ->
                    scope.launch(Dispatchers.IO) {
                        runCatching { client.answer(session, prompt, allow) }
                            .onFailure { sendError = it.message ?: "could not answer" }
                    }
                }
            }
        }
        if (sendError.isNotEmpty()) {
            Text(sendError, style = MaterialTheme.typography.bodySmall, color = Palette.Rust,
                modifier = Modifier.padding(horizontal = 24.dp))
        }
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(12.dp)) {
            Box(Modifier.weight(1f)) {
                OutlinedTextField(
                    value = input, onValueChange = { input = it },
                    placeholder = { Text("message", color = Palette.Ash) },
                    textStyle = MaterialTheme.typography.bodyMedium.copy(color = Palette.Bone),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedBorderColor = Palette.Phosphor, unfocusedBorderColor = Palette.Line,
                        cursorColor = Palette.Phosphor,
                    ),
                    maxLines = 5,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            Text("send", style = MaterialTheme.typography.bodyMedium,
                color = if (input.isNotBlank()) Palette.Phosphor else Palette.Ash,
                modifier = Modifier.clickable(enabled = input.isNotBlank()) {
                    val text = input.trim()
                    input = ""
                    sendError = ""
                    scope.launch(Dispatchers.IO) {
                        runCatching { client.send(session, text) }
                            .onFailure { sendError = it.message ?: "could not send"; input = text }
                    }
                }.padding(12.dp))
        }
    }
}

@Composable
private fun ChatRow(item: ChatItem, onAnswer: (String, Boolean) -> Unit) {
    when (item) {
        is ChatItem.You -> Bubble(item.text, Palette.Phosphor.copy(alpha = 0.10f), Palette.Bone,
            label = if (item.by.isNotEmpty()) "you · ${item.by}" else "you")
        is ChatItem.Said -> Bubble(item.text, Palette.Panel, Palette.Bone)
        is ChatItem.Tool -> Text("▸ ${item.name}  ${item.summary}", style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace, color = Palette.Sky, maxLines = 3, overflow = TextOverflow.Ellipsis)
        is ChatItem.Output -> {
            var expanded by remember { mutableStateOf(false) }
            Text(
                if (expanded) item.text else item.text.lineSequence().firstOrNull().orEmpty().take(120) +
                    if (item.text.contains('\n') || item.text.length > 120) "  …" else "",
                style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace,
                color = if (item.error) Palette.Rust else Palette.Ash,
                modifier = Modifier.clickable { expanded = !expanded }.padding(start = 12.dp),
            )
        }
        is ChatItem.Prompt -> PromptCard(item, onAnswer)
        is ChatItem.Done -> Label("— ${item.note}")
        is ChatItem.Stopped -> Label("— session asleep; the next message wakes it")
    }
}

@Composable
private fun Bubble(text: String, bg: Color, fg: Color, label: String = "") {
    Column(Modifier.fillMaxWidth().background(bg, RoundedCornerShape(10.dp)).padding(12.dp)) {
        if (label.isNotEmpty()) Label(label)
        Text(text, style = MaterialTheme.typography.bodyMedium, color = fg)
    }
}

/**
 * The reason this screen exists: something on another machine wants to run,
 * and waits for you. What it would do is shown in full, monospace, before the
 * buttons — the summary is what you are saying yes to.
 */
@Composable
private fun PromptCard(p: ChatItem.Prompt, onAnswer: (String, Boolean) -> Unit) {
    val border = if (p.open) Palette.Amber else Palette.Line
    Column(
        Modifier.fillMaxWidth()
            .border(1.dp, border, RoundedCornerShape(10.dp))
            .background(Palette.Panel, RoundedCornerShape(10.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(if (p.open) "${p.tool} WANTS TO RUN" else p.tool,
            style = MaterialTheme.typography.labelSmall, color = if (p.open) Palette.Amber else Palette.Ash)
        Text(p.summary, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace,
            color = Palette.Bone)
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
