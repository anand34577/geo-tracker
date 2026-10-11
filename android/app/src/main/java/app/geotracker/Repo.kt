package app.geotracker

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

data class ModeStat(val mode: String, val distance: Double, val ms: Long, val trips: Int)
data class PlaceStat(val name: String, val lat: Double, val lon: Double, val visits: Int, val ms: Long)
data class AreaStat(val name: String, val visits: Int, val ms: Long)
data class DayStat(val day: String, val distance: Double, val movingMs: Long, val visits: Int, val points: Int)

data class Insights(
    val distance: Double, val movingMs: Long, val visits: Int, val trips: Int, val places: Int, val daysTracked: Int,
    val modes: List<ModeStat>, val topPlaces: List<PlaceStat>, val countries: List<AreaStat>, val cities: List<AreaStat>,
    val daily: List<DayStat>, val longest: Trip?,
)

data class Day(val visits: List<Visit>, val trips: List<Trip>, val path: List<Fix>, val fromServer: Boolean, val note: String? = null)

/**
 * One place for every screen to read history. With a server it asks the server (named places,
 * imports, corrections from the web); without one, or when the server can't be reached, it
 * computes the same answers on the device from the local database.
 */
object Repo {
    private val db get() = App.db
    private val prefs get() = App.prefs
    val zone: ZoneId get() = ZoneId.systemDefault()

    fun dayRange(d: LocalDate) = d.atStartOfDay(zone).toInstant().toEpochMilli() to d.plusDays(1).atStartOfDay(zone).toInstant().toEpochMilli()

    suspend fun day(d: LocalDate, user: Long? = null): Day = withContext(Dispatchers.IO) {
        val (from, to) = dayRange(d)
        if (prefs.connected) {
            try {
                val (v, t) = Api.timeline(from, to, user)
                val path = Api.points(from, to, user).first.map { it.fix() }
                return@withContext Day(v, t, path, true)
            } catch (e: Exception) {
                if (user != null) return@withContext Day(emptyList(), emptyList(), emptyList(), true, e.message)
                return@withContext localDay(from, to).copy(note = "Offline · computed on this phone")
            }
        }
        localDay(from, to)
    }

    private suspend fun localDay(from: Long, to: Long): Day {
        // Start early so a stay that began yesterday is recognised as one.
        val pts = db.points().range(from - 12 * 3_600_000L, to)
        val b = TimelineBuilder(maxAccuracy = 100.0)
        pts.forEach { b.add(it.fix()) }
        b.finish()
        val (visits, trips) = applyLocal(b.visits, b.trips)
        return Day(
            visits.filter { it.end > from && it.start < to },
            trips.filter { it.end > from && it.start < to },
            pts.filter { it.ts >= from }.map { it.fix() }, false,
        )
    }

    /** Names visits after saved places and applies corrections made on the device. */
    private suspend fun applyLocal(visits: List<Visit>, trips: List<Trip>): Pair<List<Visit>, List<Trip>> {
        val places = db.places().list()
        val edits = db.edits().all().associateBy { it.kind to it.start }
        val v = visits.mapNotNull { v ->
            if (edits["hide" to v.start] != null) return@mapNotNull null
            val name = edits["visit" to v.start]?.value ?: matchPlace(places, v.lat, v.lon)?.name
            v.copy(name = name)
        }
        val t = trips.map { t -> edits["trip" to t.start]?.let { t.copy(mode = it.value, confidence = 1.0) } ?: t }
        return v to t
    }

    fun matchPlace(places: List<PlaceRow>, lat: Double, lon: Double) =
        places.filter { distance(it.lat, it.lon, lat, lon) <= it.radius }.minByOrNull { distance(it.lat, it.lon, lat, lon) }

    suspend fun setTripMode(t: Trip, mode: String) = withContext(Dispatchers.IO) {
        if (prefs.connected) Api.setTripMode(t.start, mode) else db.edits().put(EditRow(t.start, "trip", mode))
    }

    suspend fun renameVisit(v: Visit, name: String) = withContext(Dispatchers.IO) {
        if (prefs.connected) Api.renameVisit(v.start, name) else db.edits().put(EditRow(v.start, "visit", name))
    }

    suspend fun hideVisit(v: Visit, hidden: Boolean = true) = withContext(Dispatchers.IO) {
        when {
            prefs.connected -> Api.hideVisit(v.start, hidden)
            hidden -> db.edits().put(EditRow(v.start, "hide", "1"))
            else -> db.edits().delete(v.start, "hide")
        }
    }

    suspend fun insights(from: LocalDate, to: LocalDate): Pair<Insights, Boolean> = withContext(Dispatchers.IO) {
        val f = dayRange(from).first
        val t = dayRange(to).second
        if (prefs.connected) {
            runCatching { return@withContext Api.insights(f, t, zone.id) to true }
        }
        localInsights(f, t) to false
    }

    /** Same statistics as the server's /insights, streamed through the database in pages. */
    private suspend fun localInsights(from: Long, to: Long): Insights {
        val b = TimelineBuilder()
        val pointsPerDay = HashMap<String, Int>()
        var after = from - 1
        while (true) {
            val page = db.points().page(after, to, 20_000)
            if (page.isEmpty()) break
            page.forEach {
                b.add(it.fix())
                pointsPerDay.merge(dayOf(it.ts), 1, Int::plus)
            }
            after = page.last().ts
        }
        b.finish()
        val (visits, trips) = applyLocal(b.visits, b.trips)

        val daily = HashMap<String, DayStat>()
        fun day(k: String) = daily.getOrPut(k) { DayStat(k, 0.0, 0, 0, pointsPerDay[k] ?: 0) }
        pointsPerDay.keys.forEach { day(it) }
        trips.forEach { t -> day(dayOf(t.start)).let { daily[it.day] = it.copy(distance = it.distance + t.distance, movingMs = it.movingMs + t.end - t.start) } }
        visits.forEach { v -> day(dayOf(v.start)).let { daily[it.day] = it.copy(visits = it.visits + 1) } }

        // Places: a saved place by name, else a ~100 m cell, like the server does.
        val top = HashMap<String, PlaceStat>()
        for (v in visits) {
            val key = v.name ?: "%.3f,%.3f".format(v.lat, v.lon)
            val ms = minOf(v.end, to) - maxOf(v.start, from)
            val p = top[key] ?: PlaceStat(v.name ?: "Unnamed place", v.lat, v.lon, 0, 0)
            top[key] = p.copy(visits = p.visits + 1, ms = p.ms + maxOf(ms, 0))
        }
        return Insights(
            distance = trips.sumOf { it.distance }, movingMs = trips.sumOf { it.end - it.start },
            visits = visits.size, trips = trips.size, places = top.size, daysTracked = pointsPerDay.size,
            modes = trips.groupBy { it.mode }.map { (m, ts) -> ModeStat(m, ts.sumOf { it.distance }, ts.sumOf { it.end - it.start }, ts.size) }
                .sortedByDescending { it.distance },
            topPlaces = top.values.sortedByDescending { it.ms }.take(10),
            countries = emptyList(), cities = emptyList(),
            daily = daily.values.sortedBy { it.day },
            longest = trips.maxByOrNull { it.distance },
        )
    }

    private fun dayOf(ts: Long) = Instant.ofEpochMilli(ts).atZone(zone).toLocalDate().toString()

    /** Local days with data in a month, for calendar dots. */
    suspend fun activeDays(month: LocalDate): Set<LocalDate> = withContext(Dispatchers.IO) {
        val first = month.withDayOfMonth(1)
        val (from, _) = dayRange(first)
        val (_, to) = dayRange(first.plusMonths(1).minusDays(1))
        // 15-minute buckets, converted here: correct across DST changes and half-hour zones.
        db.points().quarterHours(from, to).map { Instant.ofEpochMilli(it * 900_000L).atZone(zone).toLocalDate() }.toSet()
    }

    /** Re-reads the server's places, keeping each one's device-only pause flag. */
    private suspend fun pullPlaces(override: PlaceRow? = null) {
        val paused = db.places().list().filter { it.pause }.mapNotNull { it.remoteId }.toSet()
        db.places().replaceRemote(Api.places().map { r ->
            val isNew = override != null && override.remoteId == null && r.name == override.name &&
                distance(r.lat, r.lon, override.lat, override.lon) < 1
            r.copy(pause = if (override != null && (r.remoteId == override.remoteId || isNew)) override.pause else r.remoteId in paused)
        })
    }

    suspend fun refreshPlaces() = withContext(Dispatchers.IO) {
        if (prefs.connected) runCatching { pullPlaces() }
    }

    suspend fun savePlace(p: PlaceRow) = withContext(Dispatchers.IO) {
        if (prefs.connected && (p.remoteId != null || p.id == 0L)) {
            Api.savePlace(p)
            pullPlaces(p)
        } else {
            db.places().upsert(p)
        }
    }

    suspend fun deletePlace(p: PlaceRow) = withContext(Dispatchers.IO) {
        p.remoteId?.let { Api.deletePlace(it) }
        db.places().delete(p.id)
    }
}
