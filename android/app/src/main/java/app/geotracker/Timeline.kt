package app.geotracker

import kotlin.math.asin
import kotlin.math.cos
import kotlin.math.min
import kotlin.math.sin
import kotlin.math.sqrt

// Port of internal/timeline/timeline.go: the same visits and trips the server computes,
// so the app gives identical answers with or without a server. Keep the two in step.

data class Fix(
    val ts: Long, val lat: Double, val lon: Double,
    val acc: Double? = null, val alt: Double? = null, val speed: Double? = null,
    val bearing: Double? = null, val batt: Int? = null,
)

data class Visit(
    val start: Long, val end: Long, val lat: Double, val lon: Double,
    val radius: Double, val points: Int,
    val name: String? = null, val address: String? = null, val city: String? = null, val country: String? = null,
)

data class Trip(val start: Long, val end: Long, val distance: Double, val mode: String, val confidence: Double)

fun distance(lat1: Double, lon1: Double, lat2: Double, lon2: Double): Double {
    val p1 = Math.toRadians(lat1)
    val p2 = Math.toRadians(lat2)
    val dp = p2 - p1
    val dl = Math.toRadians(lon2 - lon1)
    val a = sin(dp / 2) * sin(dp / 2) + cos(p1) * cos(p2) * sin(dl / 2) * sin(dl / 2)
    return 2 * 6_371_008.8 * asin(sqrt(min(1.0, a)))
}

class TimelineBuilder(
    private val stayRadius: Double = 100.0,
    private val stayMinMs: Long = 5 * 60_000L,
    private val maxAccuracy: Double = 100.0,
) {
    val visits = ArrayList<Visit>()
    val trips = ArrayList<Trip>()

    private val cluster = ArrayList<Fix>()
    private var sumLat = 0.0
    private var sumLon = 0.0
    private var trip = ArrayList<Fix>()
    private var last: Fix? = null
    private var rejected = 0

    fun add(p: Fix) {
        if (p.acc != null && p.acc > maxAccuracy) return
        last?.let { l ->
            if (p.ts <= l.ts) return
            // Reject spikes, but after 3 in a row assume the previous point was the bad one.
            if (isJump(l, p) && rejected < 3) {
                rejected++
                return
            }
        }
        rejected = 0
        last = p

        if (cluster.isEmpty()) {
            push(p); return
        }
        val d = distance(sumLat / cluster.size, sumLon / cluster.size, p.lat, p.lon)
        if (d <= stayRadius) {
            push(p); return
        }

        // p left the cluster: decide whether the cluster was a stay.
        val lastC = cluster.last()
        var end = lastC.ts
        val gap = p.ts - lastC.ts
        if (gap >= stayMinMs && d / (gap / 1000.0) < STILL_SPEED) {
            // Phones go quiet when stationary: assume we stayed until we had to leave to reach p.
            end = maxOf(lastC.ts, p.ts - (d / WALK_SPEED * 1000).toLong())
        }
        if (end - cluster[0].ts >= stayMinMs) emitStay(end) else trip.addAll(cluster)
        cluster.clear(); sumLat = 0.0; sumLon = 0.0
        push(p)
    }

    fun finish(): TimelineBuilder {
        if (cluster.isEmpty()) return this
        val lastC = cluster.last()
        if (lastC.ts - cluster[0].ts >= stayMinMs) {
            emitStay(lastC.ts); return this
        }
        trip.addAll(cluster)
        closeTrip()
        return this
    }

    private fun push(p: Fix) {
        cluster.add(p); sumLat += p.lat; sumLon += p.lon
    }

    private fun emitStay(end: Long) {
        val lat = percentile(cluster.map { it.lat }.toDoubleArray(), 0.5)
        val lon = percentile(cluster.map { it.lon }.toDoubleArray(), 0.5)
        val radius = maxOf(20.0, percentile(cluster.map { distance(lat, lon, it.lat, it.lon) }.toDoubleArray(), 0.8))
        val v = Visit(cluster[0].ts, end, lat, lon, radius, cluster.size)

        // Drifting briefly out of range and back is still the same visit.
        val prev = visits.lastOrNull()
        if (prev != null && v.start - prev.end <= MERGE_GAP_MS &&
            distance(prev.lat, prev.lon, v.lat, v.lon) <= stayRadius &&
            trip.all { distance(prev.lat, prev.lon, it.lat, it.lon) <= 2 * stayRadius }
        ) {
            val merged = prev.copy(end = v.end, points = prev.points + v.points, radius = maxOf(prev.radius, v.radius))
            visits[visits.size - 1] = merged
            anchor(merged)
            return
        }
        trip.add(cluster[0])
        closeTrip()
        visits.add(v)
        anchor(v)
    }

    // anchor starts the next trip at the visit's center, when the visit ended.
    private fun anchor(v: Visit) {
        trip = arrayListOf(Fix(v.end, v.lat, v.lon))
    }

    private fun closeTrip() {
        val pts = trip
        trip = ArrayList()
        if (pts.size < 2 || pts.last().ts <= pts[0].ts) return
        var dist = 0.0
        val speeds = ArrayList<Double>()
        for (i in 1 until pts.size) {
            val d = distance(pts[i - 1].lat, pts[i - 1].lon, pts[i].lat, pts[i].lon)
            dist += d
            val dt = (pts[i].ts - pts[i - 1].ts) / 1000.0
            if (dt >= 5) speeds.add(d / dt)
        }
        if (dist < MIN_TRIP_M) return
        val (mode, conf) = classify(speeds, dist, pts.last().ts - pts[0].ts)
        trips.add(Trip(pts[0].ts, pts.last().ts, dist, mode, conf))
    }

    companion object {
        /** Same rule as timeline.IsJump on the server; keep the two in step. */
        fun isJump(a: Fix, b: Fix): Boolean {
            val dt = (b.ts - a.ts) / 1000.0
            if (dt <= 0) return false
            val implied = distance(a.lat, a.lon, b.lat, b.lon) / dt
            if (implied > MAX_SPEED) return true
            if (dt < 1 || dt > 300 || implied <= 20) return false
            if ((a.acc ?: 0.0) > 200 || (b.acc ?: 0.0) > 200) return false
            val v = b.speed
            return if (v != null) implied > 5 * maxOf(v, 1.0) else dt <= 60 && implied > 85
        }

        const val MAX_SPEED = 350.0
        const val STILL_SPEED = 1.5
        const val WALK_SPEED = 1.4
        const val MIN_TRIP_M = 30.0
        const val MERGE_GAP_MS = 30 * 60_000L

        // ponytail: transparent heuristics, no ML; users can correct the mode later.
        fun classify(speeds: List<Double>, dist: Double, durMs: Long): Pair<String, Double> {
            if (durMs <= 0) return "unknown" to 0.0
            val avg = dist / (durMs / 1000.0)
            val s = (if (speeds.isEmpty()) listOf(avg) else speeds).toDoubleArray()
            val med = percentile(s, 0.5)
            val p95 = percentile(s, 0.95)
            val conf = 0.5 + 0.4 * min(1.0, s.size / 20.0)
            return when {
                avg > 70 || p95 > 90 -> "flight" to conf
                p95 < 2.5 -> "walk" to conf
                med < 7 && p95 < 11 -> "cycle" to conf
                med > 38 && dist > 50_000 -> "train" to conf * 0.8
                else -> "drive" to conf
            }
        }

        fun percentile(xs: DoubleArray, q: Double): Double {
            xs.sort()
            return xs[((xs.size - 1) * q).toInt()]
        }
    }
}
