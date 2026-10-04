package xyz.vpavlin.shrooms

import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.io.File

/**
 * Something written to a session, kept on the phone until its agent has it:
 * a message, or a voice note waiting to be sent as one. [id] is made here and
 * goes with it, so sending again — the network went before the answer came —
 * is harmless: the agent takes an id once.
 */
data class Outgoing(
    val id: String,
    val address: String,
    val host: String,
    val session: String,
    /** "text" or "voice". */
    val kind: String,
    val text: String = "",
    /** The recording, for a voice note: a file under the app's own storage. */
    val file: String = "",
    val created: Long = 0,
    /** Why the last try failed, for showing; empty if never tried or fine. */
    val lastError: String = "",
)

/**
 * The outbox: typed or recorded with the machine unreachable — or the phone
 * offline — and sent when it can be, in order per session, by whichever is
 * running: the conversation on screen, or the watcher in the background.
 */
object Outbox {
    /** Bumped on every change, for what shows the outbox to redraw. */
    val changed = MutableStateFlow(0L)
    private val lock = Mutex()

    private fun file(ctx: Context) = File(ctx.filesDir, "outbox.json")

    /** Where a voice note waits: inside the app, not in a cache that may be cleared. */
    fun voiceDir(ctx: Context) = File(ctx.filesDir, "outbox").apply { mkdirs() }

    fun encode(items: List<Outgoing>): String = JSONArray(items.map {
        JSONObject().put("id", it.id).put("address", it.address).put("host", it.host).put("session", it.session)
            .put("kind", it.kind).put("text", it.text).put("file", it.file).put("created", it.created)
            .put("error", it.lastError)
    }).toString()

    fun decode(json: String): List<Outgoing> = runCatching {
        val a = JSONArray(json)
        (0 until a.length()).map { i ->
            val o = a.getJSONObject(i)
            Outgoing(o.optString("id"), o.optString("address"), o.optString("host"), o.optString("session"),
                o.optString("kind"), o.optString("text"), o.optString("file"), o.optLong("created"),
                o.optString("error"))
        }.filter { it.id.isNotEmpty() && (it.kind == "text" || it.kind == "voice") }
    }.getOrDefault(emptyList())

    @Synchronized fun list(ctx: Context): List<Outgoing> =
        file(ctx).takeIf { it.exists() }?.let { decode(it.readText()) } ?: emptyList()

    @Synchronized private fun write(ctx: Context, items: List<Outgoing>) {
        val f = file(ctx)
        val tmp = File(f.path + ".tmp")
        tmp.writeText(encode(items))
        tmp.renameTo(f)
        changed.value = changed.value + 1
    }

    @Synchronized fun add(ctx: Context, o: Outgoing) = write(ctx, list(ctx) + o)

    /** Takes one out — sent, or given up on — with its recording. */
    @Synchronized fun remove(ctx: Context, id: String) {
        val items = list(ctx)
        items.firstOrNull { it.id == id }?.file?.takeIf { it.isNotEmpty() }?.let { File(it).delete() }
        write(ctx, items.filterNot { it.id == id })
    }

    @Synchronized private fun failed(ctx: Context, id: String, why: String) =
        write(ctx, list(ctx).map { if (it.id == id) it.copy(lastError = why) else it })

    fun newId(): String = "o-" + System.currentTimeMillis().toString(36) + "-" + (0..999_999).random().toString(36)

    /**
     * What to send now, in order: per session, its oldest first, and nothing
     * after one that has not gone — the order things were said in is kept.
     */
    fun nextPerSession(items: List<Outgoing>): List<Outgoing> =
        items.sortedBy { it.created }.groupBy { it.address + "/" + it.session }.values.map { it.first() }

    /**
     * Sends what can be sent: per session, oldest first, until one fails.
     * [only] limits it — the conversation on screen flushes its own.
     */
    suspend fun flush(ctx: Context, only: (Outgoing) -> Boolean = { true }) = lock.withLock {
        withContext(Dispatchers.IO) {
            while (true) {
                val next = nextPerSession(list(ctx)).filter(only)
                if (next.isEmpty()) break
                var sent = false
                for (o in next) {
                    val r = runCatching {
                        val c = AgentClient(o.address)
                        when (o.kind) {
                            "voice" -> c.voice(o.session, File(o.file).name, File(o.file).readBytes(), o.id)
                            else -> c.send(o.session, o.text, o.id)
                        }
                    }
                    r.onSuccess { remove(ctx, o.id); sent = true }
                        .onFailure { failed(ctx, o.id, it.message ?: "not sent") }
                }
                // Another round only while something went: the rest of a
                // session's queue goes once its head has.
                if (!sent) break
            }
        }
    }
}
