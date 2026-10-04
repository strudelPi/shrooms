package xyz.vpavlin.shrooms

import org.junit.Assert.assertEquals
import org.junit.Test

class UnreadTest {
    @Test fun repliesSinceTheLastLook() {
        assertEquals(0, Unread.count(null, 42))   // never seen here: its past is not news
        assertEquals(0, Unread.count(42, 42))
        assertEquals(3, Unread.count(42, 45))
        assertEquals(0, Unread.count(42, 2))      // made again: counts from scratch
        assertEquals(0, Unread.count(5, -1))      // an agent that does not count turns
    }
}
