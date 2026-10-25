package app.geotracker

import org.junit.Assert.assertEquals
import org.junit.Test

class FixGateTest {
    private val t0 = 1_700_000_000_000L
    private fun gate() = FixGate({ 50.0 }, { 0.0 }, { 120 })
    private fun fix(sec: Long, lonOffset: Double = 0.0, acc: Double = 10.0, speed: Double? = 10.0) =
        Fix(t0 + sec * 1000, 52.52, 13.405 + lonOffset, acc = acc, speed = speed)

    private fun feed(g: FixGate, f: Fix): Verdict = g.check(f).also { if (it == Verdict.KEEP) g.commit(f) }

    @Test
    fun busWifiFixIsDropped() {
        val g = gate()
        assertEquals(Verdict.KEEP, feed(g, fix(0)))
        assertEquals(Verdict.KEEP, feed(g, fix(10, 0.0014)))
        // A Wi-Fi fix reporting 20 m accuracy but 3 km away, while the chip says 10 m/s.
        assertEquals(Verdict.JUMP, feed(g, fix(20, 0.05, acc = 20.0)))
        assertEquals(Verdict.KEEP, feed(g, fix(30, 0.0042)))
    }

    @Test
    fun persistentJumpIsAcceptedAfterThreeRejections() {
        val g = gate()
        feed(g, fix(0))
        val v = (1..4).map { feed(g, fix(it * 10L, 0.5)) }
        assertEquals(listOf(Verdict.JUMP, Verdict.JUMP, Verdict.JUMP, Verdict.KEEP), v)
    }

    @Test
    fun poorAccuracyDuplicateAndLateFixes() {
        val g = gate()
        assertEquals(Verdict.ACCURACY, feed(g, fix(0, acc = 80.0)))
        assertEquals(Verdict.KEEP, feed(g, fix(10)))
        assertEquals(Verdict.DUPLICATE, feed(g, fix(10)))
        assertEquals(Verdict.OUT_OF_ORDER, feed(g, fix(5, 0.0001)))
    }

    @Test
    fun clockRollbackRecoversAfterThreeOutOfOrderFixes() {
        val g = gate()
        feed(g, fix(100))
        // The phone's clock was corrected backwards: everything now arrives "before" the saved fix.
        val v = (1..4).map { feed(g, fix(100 - it * 10L, 0.0001 * it)) }
        assertEquals(listOf(Verdict.OUT_OF_ORDER, Verdict.OUT_OF_ORDER, Verdict.OUT_OF_ORDER, Verdict.KEEP), v)
        // Recording continues against the new (earlier) clock.
        assertEquals(Verdict.KEEP, feed(g, fix(100 - 20 + 15, 0.001)))
    }

    @Test
    fun thinsByDistanceButHeartbeatsWhenStill() {
        val g = FixGate({ 50.0 }, { 20.0 }, { 120 })
        feed(g, fix(0))
        assertEquals(Verdict.THINNED, feed(g, fix(10, 0.00001)))
        assertEquals(Verdict.KEEP, feed(g, fix(130, 0.00001)))
    }
}
