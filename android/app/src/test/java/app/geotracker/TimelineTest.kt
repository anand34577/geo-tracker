package app.geotracker

import org.junit.Assert.assertEquals
import org.junit.Test
import kotlin.math.sin

// Mirrors TestHomeDriveWork in internal/timeline/timeline_test.go.
class TimelineTest {
    private val minute = 60_000L
    private val t0 = 1_700_000_000_000L

    private fun dwell(start: Long, dur: Long, step: Long, lat: Double, lon: Double) =
        (0..(dur / step).toInt()).map { i ->
            val j = 0.0001 * sin(i.toDouble())
            Fix(start + i * step, lat + j, lon - j, acc = 10.0)
        }

    private fun move(start: Long, dur: Long, step: Long, lat1: Double, lon1: Double, lat2: Double, lon2: Double) =
        generateSequence(start + step) { it + step }.takeWhile { it < start + dur }.map { ts ->
            val f = (ts - start).toDouble() / dur
            Fix(ts, lat1 + (lat2 - lat1) * f, lon1 + (lon2 - lon1) * f)
        }.toList()

    @Test
    fun homeDriveWork() {
        val pts = dwell(t0, 30 * minute, minute, 52.52, 13.405) +
            move(t0 + 30 * minute, 10 * minute, 30_000, 52.52, 13.405, 52.52, 13.48) +
            dwell(t0 + 40 * minute, 60 * minute, minute, 52.52, 13.48)
        val b = TimelineBuilder()
        pts.forEach(b::add)
        b.finish()
        assertEquals(2, b.visits.size)
        assertEquals(1, b.trips.size)
        assertEquals("drive", b.trips[0].mode)
    }
}
