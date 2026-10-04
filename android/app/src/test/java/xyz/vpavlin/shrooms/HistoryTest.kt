package xyz.vpavlin.shrooms

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HistoryTest {
    private fun ev(seq: Long, kind: String = "message", text: String = "m$seq") =
        AgentEvent(seq, kind, "phone", JSONObject().put("text", text), time = 1_759_000_000_000 + seq)

    @Test fun keptAndReadBackAsTheSameConversation() {
        val events = listOf(ev(1), ev(2, "partial"), ev(3))
        val earlier = listOf(Earlier(5, "user", "before"))
        val h = History.decode(History.encode(42, events, earlier))!!
        assertEquals(42L, h.saved)
        assertEquals(listOf(1L, 3L), h.events.map { it.seq })
        assertEquals("m3", h.events[1].data.getString("text"))
        assertEquals(events[2].time, h.events[1].time)
        assertEquals("phone", h.events[1].by)
        assertEquals(earlier, h.earlier)
        // What is shown from it is what the live events would show; and the
        // transcript's turns only above the session's first event.
        assertEquals(0, AgentChat.items(listOf(ev(40)), earlier).count { it is ChatItem.Earlier })
        assertEquals(AgentChat.items(listOf(events[0], events[2]), earlier), AgentChat.items(h.events, h.earlier))
    }

    @Test fun onlyTheNewestAreKept() {
        val h = History.decode(History.encode(0, (1L..1000L).map { ev(it) }, emptyList()))!!
        assertEquals(History.EVENTS, h.events.size)
        assertEquals(1000L, h.events.last().seq)
        assertEquals(1000L - History.EVENTS + 1, h.events.first().seq)
    }

    // A tool's output is kept cut, so a busy session's copy still holds its
    // last events rather than the three that fit whole.
    @Test fun aHugeOutputIsKeptCut() {
        val big = "x".repeat(History.BYTES / 3)
        val events = (1L..10L).map { e ->
            AgentEvent(e, "claude", "", JSONObject().put("type", "user").put("message", JSONObject()
                .put("content", org.json.JSONArray().put(JSONObject().put("type", "tool_result").put("content", big)))))
        }
        val json = History.encode(0, events, emptyList())
        assertTrue(json.length < History.BYTES)
        val h = History.decode(json)!!
        assertEquals(10, h.events.size)
        val kept = h.events[0].data.getJSONObject("message").getJSONArray("content").getJSONObject(0).getString("content")
        assertEquals(History.STRING + 1, kept.length)
        assertEquals(big.length, events[0].data.getJSONObject("message").getJSONArray("content").getJSONObject(0).getString("content").length)
    }

    @Test fun theMegabyteStillHolds() {
        val json = History.encode(0, (1L..1000L).map { ev(it, text = "y".repeat(History.STRING)) }, emptyList())
        assertTrue(json.length <= History.BYTES + 100)
    }

    @Test fun somethingUnreadableIsNothing() {
        assertEquals(null, History.decode("not json"))
    }
}

class HistoryRefreshTest {
    private fun ev(seq: Long) = AgentEvent(seq, "message", "", JSONObject().put("text", "m$seq"))
    private fun kept(vararg seqs: Long) = KeptHistory(1, seqs.map { ev(it) }, emptyList())

    // Only what the copy lacks is read; nothing when it is up to date; the
    // whole tail when there is none, or when the session was made again.
    @Test fun readsOnlyWhatIsMissing() {
        assertEquals(0L, History.after(null, 40))
        assertEquals(null, History.after(null, 0))
        assertEquals(null, History.after(kept(9, 10), 10))
        assertEquals(10L, History.after(kept(9, 10), 14))
        assertEquals(0L, History.after(kept(9, 10), 3))
    }

    @Test fun addsToTheCopyOrReplacesIt() {
        assertEquals(listOf(9L, 10L, 11L), History.extend(kept(9, 10), 10, listOf(ev(11))).map { it.seq })
        assertEquals(listOf(1L, 2L), History.extend(kept(9, 10), 0, listOf(ev(1), ev(2))).map { it.seq })
        assertEquals(listOf(5L), History.extend(null, 4, listOf(ev(5))).map { it.seq })
    }
}

class ReplayTest {
    // A conversation with no copy is shown once its replay has reached the
    // session's newest event — not at the first pause, which over the mesh
    // comes mid-replay.
    @Test fun shownOnceCaughtUp() {
        assertEquals(false, AgentChat.replayCaughtUp(null, 0, 500))        // nothing yet
        assertEquals(false, AgentChat.replayCaughtUp(700, 1000, 0))
        assertEquals(false, AgentChat.replayCaughtUp(700, 1000, 5))        // a pause mid-replay
        assertEquals(true, AgentChat.replayCaughtUp(1000, 1000, 0))
        assertEquals(true, AgentChat.replayCaughtUp(1003, 1000, 0))
        assertEquals(false, AgentChat.replayCaughtUp(40, 0, 10))           // target unknown: wait
        assertEquals(true, AgentChat.replayCaughtUp(40, 0, 80))            // …but not for ever
    }
}
