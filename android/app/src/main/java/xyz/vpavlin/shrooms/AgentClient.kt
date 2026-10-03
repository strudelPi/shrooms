package xyz.vpavlin.shrooms

import org.json.JSONObject
import java.io.BufferedReader
import java.io.InputStreamReader
import java.net.HttpURLConnection
import java.net.InetAddress
import java.net.URL

/**
 * The agent API of shrooms-agent on another device (docs/agents.md).
 *
 * Plain HTTP, and only ever to a mesh address: the traffic is inside the
 * WireGuard tunnel, which is what encrypts it and what decides who can reach
 * the agent at all. [AgentClient] refuses any other address, so the cleartext
 * this app is allowed (res/xml/network_security_config.xml) can only ever go
 * through the mesh.
 */
const val AGENT_PORT = 7387

data class AgentSession(
    val name: String,
    val dir: String,
    /** idle, working, or waiting — a permission prompt needs an answer. */
    val state: String,
    val pending: Int,
    val running: Boolean,
    val lastSeq: Long,
)

data class AgentEvent(
    val seq: Long,
    /** claude, message, answer or stopped. */
    val kind: String,
    val by: String,
    val data: JSONObject,
)

class AgentClient(address: String) {
    private val base: String

    init {
        require(isMeshAddress(address)) { "$address is not a mesh address" }
        base = if (address.contains(':')) "http://[$address]:$AGENT_PORT" else "http://$address:$AGENT_PORT"
    }

    fun sessions(timeoutMs: Int = 4000): List<AgentSession> {
        val o = JSONObject(request("GET", "/v1/sessions", null, timeoutMs))
        val a = o.optJSONArray("sessions") ?: return emptyList()
        return (0 until a.length()).map { i ->
            val s = a.getJSONObject(i)
            AgentSession(
                name = s.optString("name"),
                dir = s.optString("dir"),
                state = s.optString("state"),
                pending = s.optInt("pending"),
                running = s.optBoolean("running"),
                lastSeq = s.optLong("last_seq"),
            )
        }
    }

    fun create(name: String, dir: String) {
        request("POST", "/v1/sessions", JSONObject().put("name", name).put("dir", dir).toString())
    }

    fun remove(name: String) {
        request("DELETE", "/v1/sessions/${enc(name)}", null)
    }

    fun send(session: String, text: String) {
        request("POST", "/v1/sessions/${enc(session)}/messages", JSONObject().put("text", text).toString())
    }

    fun answer(session: String, prompt: String, allow: Boolean, message: String = "") {
        request(
            "POST", "/v1/sessions/${enc(session)}/prompts/${enc(prompt)}",
            JSONObject().put("allow", allow).put("message", message).toString(),
        )
    }

    fun interrupt(session: String) {
        request("POST", "/v1/sessions/${enc(session)}/interrupt", null)
    }

    /**
     * Follows a session's events after [after], calling [onEvent] for each, until
     * the connection ends or [stop] says so. Returns the last seq seen, so the
     * caller reconnects from there and loses nothing — the server keeps every
     * event, numbered.
     */
    fun follow(session: String, after: Long, stop: () -> Boolean, onEvent: (AgentEvent) -> Unit): Long {
        var last = after
        val c = open("GET", "/v1/sessions/${enc(session)}/events?after=$after", 10_000)
        // The server sends a comment every 20 seconds; 60 without anything is a
        // dead connection, which on mobile data is the normal way they end.
        c.readTimeout = 60_000
        c.setRequestProperty("Accept", "text/event-stream")
        try {
            if (c.responseCode != 200) throw AgentError(errorOf(c))
            BufferedReader(InputStreamReader(c.inputStream, Charsets.UTF_8)).use { r ->
                while (!stop()) {
                    val line = r.readLine() ?: break
                    if (!line.startsWith("data: ")) continue
                    val e = parseEvent(JSONObject(line.removePrefix("data: ")))
                    if (e.seq <= last) continue
                    last = e.seq
                    onEvent(e)
                }
            }
        } finally {
            c.disconnect()
        }
        return last
    }

    private fun open(method: String, path: String, timeoutMs: Int): HttpURLConnection {
        val c = URL(base + path).openConnection() as HttpURLConnection
        c.requestMethod = method
        c.connectTimeout = timeoutMs
        c.readTimeout = timeoutMs
        return c
    }

    private fun request(method: String, path: String, body: String?, timeoutMs: Int = 15_000): String {
        val c = open(method, path, timeoutMs)
        try {
            if (body != null) {
                c.doOutput = true
                c.setRequestProperty("Content-Type", "application/json")
                c.outputStream.use { it.write(body.toByteArray()) }
            }
            if (c.responseCode / 100 != 2) throw AgentError(errorOf(c))
            return c.inputStream.bufferedReader().use { it.readText() }
        } finally {
            c.disconnect()
        }
    }

    private fun errorOf(c: HttpURLConnection): String {
        val body = runCatching { c.errorStream?.bufferedReader()?.use { it.readText() } }.getOrNull()
        val msg = body?.let { runCatching { JSONObject(it).optString("error") }.getOrNull() }
        return msg?.takeIf { it.isNotEmpty() } ?: "HTTP ${c.responseCode}"
    }

    private fun enc(s: String) = java.net.URLEncoder.encode(s, "UTF-8").replace("+", "%20")

    companion object {
        private val ipv4 = Regex("""^\d{1,3}(\.\d{1,3}){3}$""")

        /**
         * Overlay addresses are ULA (fd00::/8, ADR: cryptographic addressing);
         * the IPv4 aliases are in 198.18.0.0/15 (ADR-021). Anything else is not
         * the mesh, and the cleartext allowance must never reach it.
         */
        fun isMeshAddress(address: String): Boolean {
            // Literals only. getByName on a name would resolve it, and a name
            // can resolve to anything; on a literal it parses and nothing more.
            val literal = address.contains(':') || ipv4.matches(address)
            if (!literal) return false
            val a = runCatching { InetAddress.getByName(address) }.getOrNull() ?: return false
            val b = a.address
            return when (b.size) {
                16 -> (b[0].toInt() and 0xff) == 0xfd
                4 -> (b[0].toInt() and 0xff) == 198 && ((b[1].toInt() and 0xff) == 18 || (b[1].toInt() and 0xff) == 19)
                else -> false
            }
        }

        fun parseEvent(o: JSONObject) = AgentEvent(
            seq = o.optLong("seq"),
            kind = o.optString("kind"),
            by = o.optString("by"),
            data = o.optJSONObject("data") ?: JSONObject(),
        )
    }
}

class AgentError(message: String) : Exception(message)
