package xyz.vpavlin.shrooms

import org.json.JSONObject

/**
 * One line of a conversation as the phone shows it, built from the session's
 * events. Pure, so the rules — which prompts are still open, what is shown and
 * what is noise — are tested without a device (AgentChatTest).
 */
sealed class ChatItem {
    abstract val seq: Long

    /** A turn somebody sent, and from which device. */
    data class You(override val seq: Long, val text: String, val by: String) : ChatItem()

    /** What the model said. */
    data class Said(override val seq: Long, val text: String) : ChatItem()

    /** A tool it used, summarised: "Bash  make test". */
    data class Tool(override val seq: Long, val name: String, val summary: String) : ChatItem()

    /** What the tool returned, folded by default. */
    data class Output(override val seq: Long, val text: String, val error: Boolean) : ChatItem()

    /**
     * A permission prompt. [open] while it can still be answered: not yet
     * answered, and its process has not ended since.
     */
    data class Prompt(
        override val seq: Long,
        val id: String,
        val tool: String,
        val summary: String,
        val description: String,
        val open: Boolean,
        val answer: String,
    ) : ChatItem()

    /** A turn ended: how, and what it cost. */
    data class Done(override val seq: Long, val ok: Boolean, val note: String) : ChatItem()

    /** The process stopped; the conversation continues on the next message. */
    data class Stopped(override val seq: Long, val reason: String) : ChatItem()
}

object AgentChat {
    fun items(events: List<AgentEvent>): List<ChatItem> {
        // Which prompts are settled, and how: answered, or orphaned by the
        // process ending before anybody answered.
        val answers = mutableMapOf<String, String>()
        var lastStop = -1L
        for (e in events) {
            when (e.kind) {
                "answer" -> answers[e.data.optString("prompt")] =
                    if (e.data.optBoolean("allow")) "allowed${by(e)}" else "denied${by(e)}"
                "stopped" -> lastStop = e.seq
            }
        }

        val out = mutableListOf<ChatItem>()
        for (e in events) {
            when (e.kind) {
                "message" -> out += ChatItem.You(e.seq, e.data.optString("text"), e.by)
                "stopped" -> out += ChatItem.Stopped(e.seq, e.data.optString("reason"))
                "claude" -> claude(e, answers, lastStop, out)
            }
        }
        return out
    }

    private fun by(e: AgentEvent) = if (e.by.isNotEmpty()) " from ${e.by}" else ""

    private fun claude(e: AgentEvent, answers: Map<String, String>, lastStop: Long, out: MutableList<ChatItem>) {
        val d = e.data
        when (d.optString("type")) {
            "assistant" -> {
                val content = d.optJSONObject("message")?.optJSONArray("content") ?: return
                for (i in 0 until content.length()) {
                    val c = content.getJSONObject(i)
                    when (c.optString("type")) {
                        "text" -> c.optString("text").takeIf { it.isNotBlank() }
                            ?.let { out += ChatItem.Said(e.seq, it.trim()) }
                        "tool_use" -> out += ChatItem.Tool(
                            e.seq, c.optString("name"), summarise(c.optJSONObject("input")),
                        )
                    }
                }
            }
            "user" -> {
                // Tool results come back as a "user" message from Claude Code.
                val content = d.optJSONObject("message")?.optJSONArray("content") ?: return
                for (i in 0 until content.length()) {
                    val c = content.optJSONObject(i) ?: continue
                    if (c.optString("type") != "tool_result") continue
                    out += ChatItem.Output(e.seq, resultText(c.opt("content")), c.optBoolean("is_error"))
                }
            }
            "control_request" -> {
                val r = d.optJSONObject("request") ?: return
                if (r.optString("subtype") != "can_use_tool") return
                val id = d.optString("request_id")
                val answer = answers[id]
                    ?: if (lastStop > e.seq) "the session stopped before it was answered" else ""
                out += ChatItem.Prompt(
                    seq = e.seq,
                    id = id,
                    tool = r.optString("tool_name"),
                    summary = summarise(r.optJSONObject("input")),
                    description = r.optString("description"),
                    open = answer.isEmpty(),
                    answer = answer,
                )
            }
            "result" -> {
                val sub = d.optString("subtype")
                val cost = d.optDouble("total_cost_usd", Double.NaN)
                val note = buildString {
                    append(if (sub == "success") "done" else sub.replace('_', ' '))
                    if (!cost.isNaN()) append("  ·  $" + String.format("%.3f", cost))
                }
                out += ChatItem.Done(e.seq, sub == "success", note)
            }
        }
    }

    /**
     * A tool's input in one line: the command for Bash, the path for file
     * tools, the pattern for searches — what you would want to see before
     * saying yes.
     */
    fun summarise(input: JSONObject?): String {
        if (input == null) return ""
        for (k in listOf("command", "file_path", "pattern", "path", "url", "query", "description", "prompt")) {
            val v = input.optString(k)
            if (v.isNotEmpty()) return v
        }
        return input.toString().take(200)
    }

    private fun resultText(content: Any?): String = when (content) {
        is String -> content
        is org.json.JSONArray -> (0 until content.length()).joinToString("\n") {
            content.optJSONObject(it)?.optString("text") ?: ""
        }
        else -> ""
    }
}
