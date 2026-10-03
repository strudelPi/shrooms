package xyz.vpavlin.shrooms

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The conversation as the phone shows it, built from events shaped exactly as
 * shrooms-agent sends them — which are Claude Code's stream-json messages,
 * verbatim, recorded from a real session on 2026-10-03 (docs/agents.md).
 */
class AgentChatTest {
    private fun ev(seq: Long, kind: String, data: String, by: String = "") =
        AgentEvent(seq, kind, by, JSONObject(data))

    private val asked = listOf(
        ev(1, "message", """{"text":"run the tests"}""", by = "nothing.home"),
        ev(2, "claude", """{"type":"system","subtype":"init","session_id":"s1"}"""),
        ev(3, "claude", """{"type":"assistant","message":{"role":"assistant","content":[
            {"type":"thinking","thinking":""},
            {"type":"text","text":"Running them."},
            {"type":"tool_use","id":"t1","name":"Bash","input":{"command":"make test","description":"Run tests"}}]}}"""),
        ev(4, "claude", """{"type":"control_request","request_id":"p1","request":{"subtype":"can_use_tool",
            "tool_name":"Bash","input":{"command":"make test"},"description":"Run tests"}}"""),
    )

    // A search result jumps to its event: the first of that event's items, in a
    // list laid out from the bottom with the live row as item 0.
    @Test fun aSearchResultIsFoundInTheList() {
        val items = AgentChat.items(asked, listOf(Earlier(1, "user", "long ago")))
        // Earlier, You(1), Said(3), Tool(3), Prompt(4): event 3 is the third
        // from the top, so list index 5 - 2 = 3 (the live row, Prompt, Tool, Said).
        assertEquals(3, AgentChat.listIndexOf(items, 3))
        assertEquals(4, AgentChat.listIndexOf(items, 1))
        assertEquals("not loaded", null, AgentChat.listIndexOf(items, 99))
        // seq 0 is the transcript's, never an event to jump to.
        assertEquals(null, AgentChat.listIndexOf(items, 0))
    }

    @Test fun aResultFurtherBackWidensTheTail() {
        assertEquals("reaches event 500 of 1000, and some before it", 521, AgentChat.tailReaching(300, 1000, 500))
        assertEquals("never narrower than it was", 300, AgentChat.tailReaching(300, 1000, 990))
        assertEquals("everything stays everything", 0, AgentChat.tailReaching(0, 1000, 5))
    }

    @Test fun aPromptNobodyAnsweredIsOpen() {
        val items = AgentChat.items(asked)
        val p = items.filterIsInstance<ChatItem.Prompt>().single()
        assertTrue("an unanswered prompt must offer allow and deny", p.open)
        assertEquals("p1", p.id)
        assertEquals("make test", p.summary)
        // The user's turn says which device sent it.
        assertEquals("nothing.home", items.filterIsInstance<ChatItem.You>().single().by)
        // Thinking is not shown; text and the tool use are, in order.
        assertEquals(listOf("You", "Said", "Tool", "Prompt"), items.map { it::class.simpleName })
    }

    @Test fun anAnsweredPromptIsClosedAndSaysHow() {
        val items = AgentChat.items(asked + listOf(
            ev(5, "answer", """{"prompt":"p1","allow":false,"message":""}""", by = "nothing.home"),
            ev(6, "claude", """{"type":"result","subtype":"success","total_cost_usd":0.0123}"""),
        ))
        val p = items.filterIsInstance<ChatItem.Prompt>().single()
        assertFalse(p.open)
        assertEquals("denied from nothing.home", p.answer)
        assertEquals("done  ·  $0.012", (items.last() as ChatItem.Done).note)
    }

    // A process that ended takes its prompts with it: offering allow on one
    // would send an answer the server can only refuse.
    @Test fun aPromptOrphanedByAStoppedProcessIsClosed() {
        val items = AgentChat.items(asked + ev(5, "stopped", """{"reason":"finished"}"""))
        val p = items.filterIsInstance<ChatItem.Prompt>().single()
        assertFalse(p.open)
        assertTrue(p.answer.contains("stopped"))
    }

    @Test fun toolResultsAreShownFolded() {
        val items = AgentChat.items(listOf(
            ev(7, "claude", """{"type":"user","message":{"role":"user","content":[
                {"type":"tool_result","tool_use_id":"t1","content":"ok 12 tests\nall passed","is_error":false}]}}"""),
            ev(8, "claude", """{"type":"user","message":{"role":"user","content":[
                {"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"boom"}],"is_error":true}]}}"""),
        ))
        val outs = items.filterIsInstance<ChatItem.Output>()
        assertEquals("ok 12 tests\nall passed", outs[0].text)
        assertFalse(outs[0].error)
        assertEquals("boom", outs[1].text)
        assertTrue(outs[1].error)
    }

    @Test fun aToolIsSummarisedByWhatYouWouldWantToSee() {
        assertEquals("ls -la", AgentChat.summarise(JSONObject("""{"command":"ls -la","description":"List"}""")))
        assertEquals("/etc/hosts", AgentChat.summarise(JSONObject("""{"file_path":"/etc/hosts","content":"x"}""")))
        assertEquals("TODO", AgentChat.summarise(JSONObject("""{"pattern":"TODO","path":"src"}""")))
    }
}

/**
 * The app may speak plain HTTP only because it is inside the tunnel. This is
 * the guard that keeps it there.
 */
class MeshAddressTest {
    @Test fun meshAddressesAreAccepted() {
        assertTrue(AgentClient.isMeshAddress("fdb0:9afc:a5ef:388c:8264:7716:36fc:64eb"))
        assertTrue(AgentClient.isMeshAddress("198.18.57.99"))
        assertTrue(AgentClient.isMeshAddress("198.19.245.139"))
    }

    @Test fun anythingElseIsRefused() {
        for (a in listOf(
            "2a00:102a:504f:2c8e::e6", // a real public IPv6
            "128.140.55.128",           // the VPS's public IPv4
            "192.168.0.1", "10.0.0.1", "127.0.0.1", "::1",
            "198.20.0.1",               // next to the alias range, not in it
            "cafe.bad",                 // a name made of hex letters: must not be resolved
            "example.com", "",
        )) {
            assertFalse("$a must not count as a mesh address", AgentClient.isMeshAddress(a))
        }
    }
}
