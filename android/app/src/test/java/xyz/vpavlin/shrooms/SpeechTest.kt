package xyz.vpavlin.shrooms

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SpeechTest {
    @Test fun markdownIsReadAsProse() {
        val md = """
            ## Results
            The **agent** found [three papers](https://x.io/a) and `go test` passed.

            - first point
            - second _point_

            ```go
            func main() {}
            ```
            See https://example.org/long/url for more.

            | a | b |
            |---|---|
            | 1 | 2 |
        """.trimIndent()
        val s = Speech.speakable(md)
        assertTrue(s, s.startsWith("Results\nThe agent found three papers and go test passed."))
        assertTrue(s, s.contains("first point\nsecond point"))
        assertTrue(s, s.contains("(go code)"))
        assertFalse(s, s.contains("func main"))
        assertTrue(s, s.contains("See link for more."))
        assertFalse(s, s.contains("**") || s.contains("##") || s.contains("```") || s.contains("|---"))
    }

    @Test fun czechIsToldFromEnglish() {
        assertTrue(Speech.isCzech("Tak jo, to bylo fakt rychlé, tohle se mi líbí a Parakeet je rozhodně lepší."))
        assertFalse(Speech.isCzech("The tests pass and the release is published to the repo."))
        // An English reply quoting one Czech name stays English.
        assertFalse(Speech.isCzech("Václav asked for the release notes to be written up properly before Friday, " +
            "with the changes grouped by component and the breaking ones first."))
        assertFalse(Speech.isCzech("12345 ..."))
    }

    @Test fun longTextIsCutAtParagraphsThenSentences() {
        val para = "One sentence here. Another sentence there. "
        val text = (1..5).joinToString("\n\n") { para.repeat(10).trim() }
        val pieces = Speech.chunks(text, 600)
        assertTrue(pieces.all { it.length <= 600 })
        assertEquals(text.replace(Regex("\\s+"), " "), pieces.joinToString(" ").replace(Regex("\\s+"), " "))
        assertTrue(pieces.all { it.endsWith(".") })
        assertEquals(listOf("short"), Speech.chunks("short", 600))
    }

    private fun assistant(seq: Long, vararg blocks: Pair<String, String>) = AgentEvent(seq, "claude", "",
        JSONObject().put("type", "assistant").put("message", JSONObject().put("content",
            JSONArray(blocks.map { (t, v) -> JSONObject().put("type", t).put(if (t == "text") "text" else "name", v) }))))

    @Test fun onlyTheModelsTextIsRead() {
        val tool = AgentEvent(2, "claude", "", JSONObject().put("type", "user"))
        val events = listOf(
            assistant(1, "text" to "Looking at it."),
            tool,
            assistant(3, "tool_use" to "Bash"),
            assistant(4, "thinking" to "", "text" to "Done: all **green**."),
            AgentEvent(5, "message", "phone", JSONObject().put("text", "thanks")),
        )
        assertEquals(listOf(1L to "Looking at it.", 4L to "Done: all **green**."), Speech.toRead(events, 0))
        // Only what is new since the last read.
        assertEquals(listOf(4L to "Done: all **green**."), Speech.toRead(events, 1))
        // Auto-play not switched on (no mark): nothing from the past.
        assertEquals(emptyList<Pair<Long, String>>(), Speech.toRead(events, null))
    }
}
