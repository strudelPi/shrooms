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

    // AskUserQuestion, as Claude Code 2.1.288 sends it: its own card with the
    // questions and options, not a permission, and once answered, what was said.
    private val asking = listOf(
        ev(1, "claude", """{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t","name":"AskUserQuestion",
            "input":{"questions":[{"question":"Which user?","header":"VPS user","multiSelect":false,
            "options":[{"label":"agent","description":"no sudo"},{"label":"root","description":""}]}]}}]}}"""),
        ev(2, "claude", """{"type":"control_request","request_id":"q1","request":{"subtype":"can_use_tool",
            "tool_name":"AskUserQuestion","input":{"questions":[{"question":"Which user?","header":"VPS user","multiSelect":false,
            "options":[{"label":"agent","description":"no sudo"},{"label":"root","description":""}]}]}}}"""),
    )

    @Test fun aQuestionIsShownAsOne() {
        val items = AgentChat.items(asking)
        assertEquals("the tool row says what it asks, not its JSON", "Which user?", (items[0] as ChatItem.Tool).summary)
        val p = items[1] as ChatItem.Prompt
        assertTrue(p.open)
        assertEquals(listOf(ChatItem.Question("Which user?", "VPS user", false,
            listOf(ChatItem.Option("agent", "no sudo"), ChatItem.Option("root", "")))), p.questions)
        val answered = AgentChat.items(asking + ev(3, "answer",
            """{"prompt":"q1","allow":true,"answers":{"Which user?":"agent"}}""", by = "nothing.office"))
        val q = answered.filterIsInstance<ChatItem.Prompt>().single()
        assertFalse(q.open)
        assertEquals("answered: agent from nothing.office", q.answer)
        // A permission prompt carries no questions.
        assertTrue(AgentChat.items(asked).filterIsInstance<ChatItem.Prompt>().single().questions.isEmpty())
    }

    @Test fun answersAreSentOnlyWhenEveryQuestionHasOne() {
        val qs = listOf(
            ChatItem.Question("Which?", "", false, listOf(ChatItem.Option("a", ""), ChatItem.Option("b", ""))),
            ChatItem.Question("Also?", "", true, listOf(ChatItem.Option("x", ""), ChatItem.Option("y", ""), ChatItem.Option("z", ""))),
        )
        assertEquals(null, AgentChat.answersFor(qs, mapOf("Which?" to setOf("a")), emptyMap()))
        // Several picked: in the order offered, joined as Claude Code joins them.
        assertEquals(mapOf("Which?" to "a", "Also?" to "x, z"),
            AgentChat.answersFor(qs, mapOf("Which?" to setOf("a"), "Also?" to setOf("z", "x")), emptyMap()))
        // Typed words stand in for a pick; blank ones do not.
        assertEquals(mapOf("Which?" to "neither, really", "Also?" to "y"),
            AgentChat.answersFor(qs, mapOf("Also?" to setOf("y")), mapOf("Which?" to "  neither, really ")))
        assertEquals(null, AgentChat.answersFor(qs, mapOf("Also?" to setOf("y")), mapOf("Which?" to "  ")))
    }

    // Claude Code is the usual harness and goes unsaid; another is named.
    @Test fun aHarnessIsNamedWhenItIsNotClaudeCode() {
        assertEquals("", harnessLabel("claude"))
        assertEquals("", harnessLabel(""))
        assertEquals("pi", harnessLabel("pi"))
        // An agent from before harnesses: Claude Code, which asks.
        val old = AgentSession("s", "/x", "idle", 0, false, 0)
        assertEquals("claude", old.harness)
        assertTrue(old.approves)
    }

    // Starred sessions come first, from every machine, and leave their machine's list.
    @Test fun starredSessionsAreListedFirst() {
        fun sess(n: String, star: Boolean = false) = AgentSession(n, "/x", "idle", 0, false, 0, starred = star)
        val hosts = listOf(
            AgentHost("vps", "home", "fd::1", listOf(sess("web"), sess("shrooms", true))),
            AgentHost("atlas", "office", "fd::2", listOf(sess("agents", true), sess("misc"))),
        )
        val (starred, rest) = starredFirst(hosts)
        assertEquals(listOf("atlas/agents", "vps/shrooms"), starred.map { it.first.name + "/" + it.second.name })
        assertEquals(listOf(listOf("web"), listOf("misc")), rest.map { h -> h.sessions.map { it.name } })
        // Starring shows at once, on that machine's session only.
        val after = withStar(hosts, "vps", "web", true)
        assertEquals(listOf(true, true), after[0].sessions.map { it.starred })
        assertEquals(listOf(true, false), after[1].sessions.map { it.starred })
    }

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
