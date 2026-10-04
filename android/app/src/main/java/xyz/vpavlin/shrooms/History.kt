package xyz.vpavlin.shrooms

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject
import java.io.File

/**
 * The end of each conversation opened here, kept on the phone: what a
 * session shows while its machine cannot be reached — the phone offline, the
 * laptop asleep — instead of an empty screen. Shown marked as kept, and
 * replaced by the machine's own events as soon as they arrive.
 */
data class KeptHistory(val saved: Long, val events: List<AgentEvent>, val earlier: List<Earlier>)

object History {
    /** At most this many events are kept per session… */
    const val EVENTS = SESSION_TAIL
    /** …and at most this much of them: a tool's output can be megabytes. */
    const val BYTES = 1 shl 20

    /**
     * The newest of [events] that fit in [EVENTS] and [BYTES], oldest first.
     * Streamed text is never kept: the whole message that follows it is.
     */
    fun encode(saved: Long, events: List<AgentEvent>, earlier: List<Earlier>): String {
        val kept = ArrayList<JSONObject>()
        var size = 0
        for (e in events.asReversed()) {
            if (e.kind == "partial") continue
            val o = JSONObject().put("seq", e.seq).put("kind", e.kind).put("by", e.by).put("data", e.data)
                .put("time", if (e.time > 0) java.time.Instant.ofEpochMilli(e.time).toString() else "")
            val n = o.toString().length
            if (kept.size >= EVENTS || size + n > BYTES) break
            kept += o
            size += n
        }
        return JSONObject().put("saved", saved)
            .put("events", JSONArray(kept.asReversed()))
            .put("earlier", JSONArray(earlier.map { JSONObject().put("time", it.time).put("role", it.role).put("text", it.text) }))
            .toString()
    }

    fun decode(json: String): KeptHistory? = runCatching {
        val o = JSONObject(json)
        val ev = o.optJSONArray("events") ?: JSONArray()
        val ea = o.optJSONArray("earlier") ?: JSONArray()
        KeptHistory(
            o.optLong("saved"),
            (0 until ev.length()).map { AgentClient.parseEvent(ev.getJSONObject(it)) },
            (0 until ea.length()).map { i ->
                ea.getJSONObject(i).let { Earlier(it.optLong("time"), it.optString("role"), it.optString("text")) }
            },
        )
    }.getOrNull()

    private fun file(ctx: Context, host: String, session: String): File {
        val key = java.security.MessageDigest.getInstance("SHA-256")
            .digest("$host/$session".toByteArray()).joinToString("") { "%02x".format(it) }.take(32)
        return File(File(ctx.filesDir, "history").apply { mkdirs() }, "$key.json")
    }

    fun load(ctx: Context, host: String, session: String): KeptHistory? =
        runCatching { file(ctx, host, session).readText() }.getOrNull()?.let(::decode)

    fun save(ctx: Context, host: String, session: String, events: List<AgentEvent>, earlier: List<Earlier>) {
        runCatching {
            val f = file(ctx, host, session)
            val tmp = File(f.path + ".tmp")
            tmp.writeText(encode(System.currentTimeMillis(), events, earlier))
            tmp.renameTo(f)
        }
    }

    fun forget(ctx: Context, host: String, session: String) {
        file(ctx, host, session).delete()
    }
}
