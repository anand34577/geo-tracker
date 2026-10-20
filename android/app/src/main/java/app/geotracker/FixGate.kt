package app.geotracker

/** Why the recorder kept or dropped a fix. Anything but [KEEP] is not stored. */
enum class Verdict { KEEP, ACCURACY, DUPLICATE, OUT_OF_ORDER, JUMP, THINNED }

/**
 * The recorder's accept/reject rules, kept free of Android types so they can be tested with
 * recorded traces. Order: accuracy limit, duplicate and out-of-order delivery, impossible jumps
 * (the same rule as the server timeline), then thinning by distance.
 */
class FixGate(
    private val maxAccuracy: () -> Double,
    private val minDistance: () -> Double,
    private val stillInterval: () -> Int,
) {
    var lastSaved: Fix? = null
        private set
    private var spikes = 0

    fun check(f: Fix): Verdict {
        if (f.acc != null && f.acc > maxAccuracy()) return Verdict.ACCURACY
        val prev = lastSaved ?: return Verdict.KEEP
        if (f.ts == prev.ts && f.lat == prev.lat && f.lon == prev.lon) return Verdict.DUPLICATE
        val dt = (f.ts - prev.ts) / 1000.0
        if (dt <= 0) return Verdict.OUT_OF_ORDER
        // After 3 rejections in a row the saved fix is probably the wrong one, so let this through.
        if (TimelineBuilder.isJump(prev, f) && spikes < 3) {
            spikes++
            return Verdict.JUMP
        }
        spikes = 0
        val d = distance(prev.lat, prev.lon, f.lat, f.lon)
        // Distance filter, but never stay silent longer than the stationary interval.
        return if (d >= minDistance() || dt >= stillInterval()) Verdict.KEEP else Verdict.THINNED
    }

    fun commit(f: Fix) {
        lastSaved = f
    }
}
