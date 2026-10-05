package xyz.vpavlin.shrooms

import org.junit.Assert.assertEquals
import org.junit.Test

class UsageTest {
    private val laptop = """{"machine":"laptop","rows":[
        {"day":"2026-10-05","session":"shrooms","by":"nothing.office","model":"claude-opus-5[1m]","harness":"claude","turns":3,"input":10,"cache_read":1000,"cache_write":200,"output":900,"cost_usd":1.5,"busy_ms":120000},
        {"day":"2026-10-05","session":"shrooms","by":"","model":"claude-opus-5[1m]","harness":"claude","turns":1,"input":5,"cache_read":0,"cache_write":0,"output":100,"cost_usd":0.5,"busy_ms":60000}]}"""
    private val jimmy = """{"machine":"jimmy-crib","rows":[
        {"day":"2026-10-04","session":"vpavlin","by":"nothing.office","model":"ollama/qwen3","harness":"pi","turns":10,"input":5000,"cache_read":0,"cache_write":0,"output":4000,"cost_usd":0,"busy_ms":3600000}]}"""

    // The machines' rows together: who asked, where it ran, which model.
    @Test fun summedAcrossMachines() {
        val rows = UsageView.parse("laptop", laptop) + UsageView.parse("jimmy-crib", jimmy)
        val who = UsageView.group(rows, UsageView.Measure.OUTPUT, UsageView::device)
        assertEquals(listOf("nothing", "laptop"), who.map { it.name })
        assertEquals(13, who[0].turns)
        assertEquals(4900L, who[0].output)
        assertEquals(1.5, who[0].costUsd, 1e-9)
        assertEquals(5000L + 10 + 1000 + 200, who[0].input)
        // The laptop asking its own agent and asking another are one device.
        val mixed = rows + UsageView.parse("atlas", """{"rows":[{"day":"2026-10-05","by":"laptop.default","turns":2,"output":50}]}""")
        val laptop = UsageView.group(mixed, UsageView.Measure.TURNS, UsageView::device).single { it.name == "laptop" }
        assertEquals(3, laptop.turns)

        val where = UsageView.group(rows, UsageView.Measure.TURNS) { it.machine }
        assertEquals(listOf("jimmy-crib" to 10, "laptop" to 4), where.map { it.name to it.turns })
        // By cost, the local model's free turns come last.
        assertEquals("laptop", UsageView.group(rows, UsageView.Measure.COST) { it.machine }.first().name)
        assertEquals("1h 0m", UsageView.format(where[0], UsageView.Measure.BUSY))
        assertEquals("4.9k", UsageView.count(4900))
    }

    @Test fun periodsStartOnTheRightDay() {
        val today = java.time.LocalDate.of(2026, 10, 5)
        assertEquals("2026-10-05", UsageView.since(1, today))
        assertEquals("2026-09-29", UsageView.since(7, today))
        assertEquals("", UsageView.since(0, today))
    }
}
