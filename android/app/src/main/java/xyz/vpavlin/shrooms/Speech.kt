package xyz.vpavlin.shrooms

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.os.Bundle
import android.speech.tts.TextToSpeech
import android.speech.tts.UtteranceProgressListener
import kotlinx.coroutines.flow.MutableStateFlow
import org.json.JSONObject
import java.util.Locale

/**
 * The model's replies read aloud, with the phone's own speech engine: offline,
 * nothing to install on the agent's machine, and it starts at once — so
 * research results can be listened to on the go. A ▶ on each reply; and per
 * session, auto-play of new replies (the model's text only: not tool calls,
 * their output or its thinking), from the conversation on screen or, with the
 * phone in a pocket, from the background watcher.
 */
object Speech {
    /** The reply being read ("host/session/seq"), or null. */
    val playing = MutableStateFlow<String?>(null)

    // --- what is said: pure, for the tests ------------------------------------

    /**
     * Markdown as it should sound: code blocks are named rather than read out
     * symbol by symbol, links become their text, and the marks of emphasis,
     * headings, lists and tables go.
     */
    fun speakable(markdown: String): String {
        var s = markdown.replace("\r", "")
        // Fenced code: named, with its language when it has one.
        s = Regex("```([A-Za-z0-9_+-]*)[^\\n]*\\n[\\s\\S]*?(```|$)").replace(s) { m ->
            val lang = m.groupValues[1]
            if (lang.isNotEmpty()) "\n($lang code)\n" else "\n(code)\n"
        }
        s = Regex("!\\[([^\\]]*)\\]\\([^)]*\\)").replace(s) { it.groupValues[1] } // images
        s = Regex("\\[([^\\]]+)\\]\\([^)]*\\)").replace(s) { it.groupValues[1] }  // links
        s = Regex("<(https?://[^>]+)>").replace(s) { "link" }
        s = Regex("https?://\\S+").replace(s, "link")
        s = Regex("`([^`]*)`").replace(s) { it.groupValues[1] }
        s = s.lines().joinToString("\n") { line ->
            var l = line
            l = Regex("^\\s{0,3}#{1,6}\\s+").replace(l, "")           // headings
            l = Regex("^\\s*>\\s?").replace(l, "")                    // quotes
            l = Regex("^\\s*([-*+]|\\d+[.)])\\s+").replace(l, "")     // list markers
            if (Regex("^\\s*\\|?[\\s:|-]+\\|[\\s:|-]*$").matches(l)) l = "" // table rule
            l = l.replace("|", ", ")
            if (Regex("^\\s*([-*_]\\s*){3,}$").matches(l)) l = ""     // horizontal rule
            l
        }
        s = Regex("(\\*\\*|__|\\*|_|~~)(?=\\S)(.+?)(?<=\\S)\\1").replace(s) { it.groupValues[2] }
        s = Regex("\\n{3,}").replace(s, "\n\n")
        return s.trim()
    }

    private val czech = Regex("[ěščřžýůťďňĚŠČŘŽÝŮŤĎŇ]")

    /**
     * Czech or English, from the text: Czech has letters English never uses,
     * and a reply of any length in Czech uses them. Only these two — the
     * languages these conversations are in.
     */
    fun isCzech(text: String): Boolean {
        val letters = text.count { it.isLetter() }
        if (letters == 0) return false
        return czech.findAll(text).count() * 100 >= letters // 1% and up
    }

    /** Text cut into pieces the engine takes, at paragraphs, then sentences. */
    fun chunks(text: String, max: Int): List<String> {
        val out = ArrayList<String>()
        fun add(piece: String) {
            var p = piece.trim()
            while (p.length > max) {
                val cut = p.lastIndexOfAny(charArrayOf('.', '!', '?', ';', ',', ' '), max - 1).let { if (it <= 0) max else it + 1 }
                out += p.substring(0, cut).trim()
                p = p.substring(cut).trim()
            }
            if (p.isNotEmpty()) out += p
        }
        var current = StringBuilder()
        for (para in text.split(Regex("\\n\\s*\\n"))) {
            if (current.isNotEmpty() && current.length + para.length + 2 > max) {
                add(current.toString()); current = StringBuilder()
            }
            if (current.isNotEmpty()) current.append("\n\n")
            current.append(para)
        }
        add(current.toString())
        return out.filter { it.isNotBlank() }
    }

    /** The model's text in an event, or null for anything else. */
    fun replyText(e: AgentEvent): String? {
        if (e.kind != "claude" || e.data.optString("type") != "assistant") return null
        val content = e.data.optJSONObject("message")?.optJSONArray("content") ?: return null
        val parts = (0 until content.length()).mapNotNull { content.optJSONObject(it) }
            .filter { it.optString("type") == "text" }.map { it.optString("text").trim() }.filter { it.isNotEmpty() }
        return parts.joinToString("\n\n").ifEmpty { null }
    }

    /**
     * Of [events], the replies to read for a session with auto-play: those
     * after [spoken], the last one already read. Null [spoken] reads nothing —
     * auto-play reads what is new from when it was switched on, never the past.
     */
    fun toRead(events: List<AgentEvent>, spoken: Long?): List<Pair<Long, String>> =
        if (spoken == null) emptyList()
        else events.filter { it.seq > spoken }.mapNotNull { e -> replyText(e)?.let { e.seq to it } }

    // --- auto-play, per session -------------------------------------------------

    private fun prefs(ctx: Context) = ctx.getSharedPreferences("speech", Context.MODE_PRIVATE)
    private fun key(host: String, session: String) = "$host/$session"
    val autoChanged = MutableStateFlow(0L)

    fun autoPlay(ctx: Context, host: String, session: String): Boolean =
        prefs(ctx).getBoolean("auto:" + key(host, session), false)

    /** Switched on, it reads what comes after [lastSeq], the newest event now. */
    fun setAutoPlay(ctx: Context, host: String, session: String, on: Boolean, lastSeq: Long) {
        val e = prefs(ctx).edit().putBoolean("auto:" + key(host, session), on)
        if (on) e.putLong("spoken:" + key(host, session), lastSeq)
        e.apply()
        if (!on) stop()
        autoChanged.value = autoChanged.value + 1
    }

    @Synchronized
    private fun spoken(ctx: Context, host: String, session: String): Long? =
        prefs(ctx).takeIf { it.contains("spoken:" + key(host, session)) }?.getLong("spoken:" + key(host, session), 0)

    /**
     * New events of a session, from whichever saw them — the screen or the
     * watcher: its new replies are queued once, in order, when auto-play is
     * on. The mark of what was read moves past every event given, so neither
     * reads them again.
     */
    @Synchronized
    fun heard(ctx: Context, host: String, session: String, events: List<AgentEvent>) {
        if (events.isEmpty() || !autoPlay(ctx, host, session)) return
        val read = toRead(events, spoken(ctx, host, session))
        val last = events.maxOf { it.seq }
        if (last > (spoken(ctx, host, session) ?: 0)) prefs(ctx).edit().putLong("spoken:" + key(host, session), last).apply()
        for ((seq, text) in read) say(ctx, "$host/$session/$seq", text, queue = true)
    }

    // --- the engine ---------------------------------------------------------------

    private var tts: TextToSpeech? = null
    private var ready = false
    private val waiting = ArrayList<Triple<String, String, Boolean>>()
    private var focus: AudioFocusRequest? = null
    private var pending = 0

    /** Reads a reply: now, stopping what was being read, or after it ([queue]). */
    @Synchronized
    fun say(ctx: Context, id: String, markdown: String, queue: Boolean = false) {
        val app = ctx.applicationContext
        if (tts == null) {
            tts = TextToSpeech(app) { status ->
                synchronized(this) {
                    ready = status == TextToSpeech.SUCCESS
                    if (ready) {
                        tts?.setAudioAttributes(AudioAttributes.Builder()
                            .setUsage(AudioAttributes.USAGE_ASSISTANT)
                            .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
                        tts?.setOnUtteranceProgressListener(listener(app))
                        val w = waiting.toList(); waiting.clear()
                        for ((i, t, q) in w) speakNow(app, i, t, q)
                    } else waiting.clear()
                }
            }
        }
        if (!ready) { waiting += Triple(id, markdown, queue); return }
        speakNow(app, id, markdown, queue)
    }

    private fun speakNow(ctx: Context, id: String, markdown: String, queue: Boolean) {
        val engine = tts ?: return
        val text = speakable(markdown)
        val locale = if (isCzech(text)) Locale("cs", "CZ") else Locale.ENGLISH
        if (engine.isLanguageAvailable(locale) >= TextToSpeech.LANG_AVAILABLE) engine.language = locale
        val pieces = chunks(text, TextToSpeech.getMaxSpeechInputLength().coerceAtMost(3500))
        if (!queue) { engine.stop(); pending = 0 }
        if (pieces.isEmpty()) return
        requestFocus(ctx)
        pieces.forEachIndexed { i, p ->
            pending++
            engine.speak(p, if (!queue && i == 0) TextToSpeech.QUEUE_FLUSH else TextToSpeech.QUEUE_ADD, Bundle(), "$id#$i")
        }
    }

    @Synchronized
    fun stop() {
        tts?.stop()
        pending = 0
        playing.value = null
        focus?.let { f -> appAudio?.abandonAudioFocusRequest(f) }
    }

    private var appAudio: AudioManager? = null

    private fun requestFocus(ctx: Context) {
        val am = ctx.getSystemService(AudioManager::class.java) ?: return
        appAudio = am
        val f = focus ?: AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN_TRANSIENT_MAY_DUCK)
            .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_ASSISTANT)
                .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build()).build().also { focus = it }
        am.requestAudioFocus(f)
    }

    private fun listener(ctx: Context) = object : UtteranceProgressListener() {
        override fun onStart(utteranceId: String) { playing.value = utteranceId.substringBefore('#') }
        override fun onDone(utteranceId: String) = finished()
        @Deprecated("Deprecated in Java") override fun onError(utteranceId: String) = finished()
        override fun onStop(utteranceId: String, interrupted: Boolean) = finished()
        private fun finished() {
            synchronized(Speech) {
                pending = (pending - 1).coerceAtLeast(0)
                if (pending == 0) {
                    playing.value = null
                    focus?.let { f -> appAudio?.abandonAudioFocusRequest(f) }
                }
            }
        }
    }
}
