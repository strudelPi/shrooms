package xyz.vpavlin.shrooms

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Tasks from caged agents, as a session's owner decides them (ADR-044,
 * ADR-070): every cage, or the ones allowed here by name. A task from any
 * other cage waits here until it is allowed, which starts it and lets that
 * cage give more, or declined, which rejects it and tells the cage.
 * Entries are those of files (AgentFiles): a session, or a whole machine.
 */
@Composable
fun CagedTasksDialog(client: AgentClient, session: String, start: AgentSession?, onChanged: (AgentSession) -> Unit, onClose: () -> Unit) {
    var info by remember { mutableStateOf(start) }
    var adding by remember { mutableStateOf("") }
    var said by remember { mutableStateOf("") }
    var round by remember { mutableStateOf(0) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(round) {
        withContext(Dispatchers.IO) {
            runCatching { client.sessions().firstOrNull { it.name == session } }.getOrNull()?.let { info = it; onChanged(it) }
        }
    }
    fun change(what: String, call: () -> Unit) {
        scope.launch {
            withContext(Dispatchers.IO) { runCatching(call) }
                .onSuccess { said = ""; round++ }
                .onFailure { said = "could not $what: ${it.message}" }
        }
    }
    val allowed = info?.acceptCagedFrom.orEmpty()
    val all = info?.acceptCaged == true
    AlertDialog(
        onDismissRequest = onClose,
        title = { Text("Caged tasks · $session") },
        confirmButton = { TextButton(onClick = onClose) { Text("Done") } },
        text = {
            Column(Modifier.heightIn(max = 520.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                if (said.isNotEmpty()) Text(said, color = Palette.Rust, style = MaterialTheme.typography.labelSmall)
                val asks = info?.taskRequests.orEmpty()
                if (asks.isNotEmpty()) {
                    Text("WANTS TO GIVE IT A TASK", style = MaterialTheme.typography.labelSmall, color = Palette.Amber)
                    // One row per cage: allowing or declining acts on all its held tasks.
                    for ((from, tasks) in asks.groupBy { it.from }) Row(verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f)) {
                            Text(from, style = MaterialTheme.typography.bodySmall, color = Palette.Bone)
                            for (t in tasks) Text(t.title.ifEmpty { t.task }, style = MaterialTheme.typography.labelSmall,
                                color = Palette.Ash, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                        Text("allow", color = Palette.Phosphor, style = MaterialTheme.typography.labelSmall,
                            modifier = Modifier.clickable { change("allow") { client.setAcceptCagedFrom(session, AgentFiles.allow(allowed, from)) } }.padding(6.dp))
                        Text("decline", color = Palette.Rust, style = MaterialTheme.typography.labelSmall,
                            modifier = Modifier.clickable { change("decline") { client.declineTasksFrom(session, from) } }.padding(6.dp))
                    }
                }
                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 8.dp)) {
                    Column(Modifier.weight(1f)) {
                        Text("FROM EVERY CAGED AGENT", style = MaterialTheme.typography.labelSmall, color = Palette.Ash)
                        Text(if (all) "on: any cage on the mesh may give it tasks" else "off: only those below",
                            style = MaterialTheme.typography.labelSmall, color = if (all) Palette.Violet else Palette.Ash)
                    }
                    Text(if (all) "turn off" else "turn on", color = if (all) Palette.Rust else Palette.Violet,
                        style = MaterialTheme.typography.labelSmall,
                        modifier = Modifier.clickable { change("change it") { client.setAcceptCaged(session, !all) } }.padding(6.dp))
                }
                Text("TAKES TASKS FROM", style = MaterialTheme.typography.labelSmall, color = Palette.Ash, modifier = Modifier.padding(top = 8.dp))
                if (allowed.isEmpty()) Text("no cage by name", style = MaterialTheme.typography.bodySmall, color = Palette.Ash)
                for (e in allowed) Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(e, style = MaterialTheme.typography.bodySmall, color = Palette.Bone, modifier = Modifier.weight(1f))
                    Text("×", color = Palette.Rust, modifier = Modifier.clickable {
                        change("remove") { client.setAcceptCagedFrom(session, AgentFiles.deny(allowed, e)) }
                    }.padding(horizontal = 10.dp, vertical = 4.dp))
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    OutlinedTextField(adding, { adding = it }, modifier = Modifier.weight(1f), singleLine = true,
                        placeholder = { Text("machine/session or machine/*", style = MaterialTheme.typography.labelSmall) },
                        textStyle = MaterialTheme.typography.bodySmall)
                    Spacer(Modifier.width(8.dp))
                    Text("add", color = if (AgentFiles.valid(adding)) Palette.Phosphor else Palette.Ash,
                        style = MaterialTheme.typography.labelSmall, modifier = Modifier.clickable {
                            if (AgentFiles.valid(adding)) {
                                val e = adding.trim(); adding = ""
                                change("add") { client.setAcceptCagedFrom(session, AgentFiles.allow(allowed, e)) }
                            }
                        }.padding(6.dp))
                }
            }
        },
    )
}
