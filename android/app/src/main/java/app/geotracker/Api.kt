package app.geotracker

import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.net.ConnectException
import java.net.HttpURLConnection
import java.net.MalformedURLException
import java.net.SocketTimeoutException
import java.net.URL
import java.net.URLEncoder
import java.net.UnknownHostException
import javax.net.ssl.SSLException

class ApiError(val status: Int, message: String) : IOException(message)

data class Person(val id: Long, val name: String, val color: String, val point: Fix?, val place: String?, val paused: Boolean)
data class Share(val id: Long, val name: String, val kind: String, val precision: String, val expiresAt: Long, val views: Int)

/** Thin client for the GeoTracker server API (docs/API.md). Blocking: call from Dispatchers.IO. */
object Api {
    private val prefs get() = App.prefs

    /**
     * Turns whatever was typed into a base URL: adds https:// when the scheme is missing, repairs
     * a swallowed slash ("https:/host"), drops spaces and trailing slashes. Null if it can't be a server.
     */
    fun normalizeServer(raw: String): String? {
        var s = raw.filterNot { it.isWhitespace() }
        if (s.isEmpty()) return null
        s = s.replace(Regex("^(?i)(https?):/{0,2}(?=[^/])"), "$1://")
        if (!s.contains("://")) s = "https://$s"
        s = s.trimEnd('/')
        val host = runCatching { java.net.URI(s).host }.getOrNull()
        return if (host.isNullOrEmpty()) null else s
    }

    private fun friendly(e: IOException): IOException = when (e) {
        is ApiError -> e
        is UnknownHostException -> IOException("Can't find that server. Check the address and your connection.", e)
        is ConnectException -> IOException("The server refused the connection. Check the address and port.", e)
        is SocketTimeoutException -> IOException("The server took too long to answer.", e)
        is SSLException -> IOException("Secure connection failed. Check the https:// address and certificate.", e)
        is MalformedURLException -> IOException("That doesn't look like a server address.", e)
        else -> e
    }

    fun call(method: String, path: String, body: Any? = null, server: String = prefs.server, token: String = prefs.token): String {
        try {
            return request(method, path, body, server, token)
        } catch (e: IOException) {
            throw friendly(e)
        }
    }

    private fun request(method: String, path: String, body: Any?, server: String, token: String): String {
        val c = URL(server + path).openConnection() as HttpURLConnection
        try {
            c.requestMethod = method
            c.connectTimeout = 15_000
            c.readTimeout = 60_000
            c.setRequestProperty("Accept", "application/json")
            if (token.isNotEmpty()) c.setRequestProperty("Authorization", "Bearer $token")
            if (body != null) {
                c.doOutput = true
                c.setRequestProperty("Content-Type", "application/json")
                c.outputStream.use { it.write(body.toString().toByteArray()) }
            }
            val code = c.responseCode
            // HttpURLConnection never follows http→https redirects; say so instead of failing on an empty body.
            if (code in 300..399) {
                throw ApiError(code, "The server redirected the request. Check the address, usually it should start with https://")
            }
            val text = (if (code < 400) c.inputStream else c.errorStream)?.bufferedReader()?.use { it.readText() } ?: ""
            if (code >= 400) {
                val msg = runCatching { JSONObject(text).getString("title") }.getOrNull()
                throw ApiError(code, msg ?: when (code) {
                    401 -> "Signed out: sign in again"
                    404 -> "That address isn't a GeoTracker server (404)"
                    else -> "Server error $code"
                })
            }
            return text
        } finally {
            c.disconnect()
        }
    }

    private fun q(vararg kv: Pair<String, Any?>) = kv.filter { it.second != null }
        .joinToString("&", "?") { "${it.first}=" + URLEncoder.encode(it.second.toString(), "UTF-8") }

    fun login(server: String, email: String, password: String, deviceName: String): JSONObject {
        val body = JSONObject().put("email", email).put("password", password).put("device_name", deviceName)
        return JSONObject(call("POST", "/api/v1/auth/token", body, server, ""))
    }

    /** True when the server confirmed that this phone's token was revoked. */
    fun signOut(): Boolean =
        prefs.deviceId <= 0 || runCatching { call("DELETE", "/api/v1/devices/${prefs.deviceId}") }.isSuccess

    fun upload(points: List<PointRow>): Int {
        val arr = JSONArray()
        points.forEach { p ->
            arr.put(JSONObject().put("ts", p.ts).put("lat", p.lat).put("lon", p.lon)
                .putOpt("acc", p.acc).putOpt("alt", p.alt).putOpt("speed", p.speed)
                .putOpt("bearing", p.bearing).putOpt("batt", p.batt))
        }
        return JSONObject(call("POST", "/ingest/json", arr)).optInt("added")
    }

    fun firstPoint(): Long? = JSONObject(call("GET", "/api/v1/stats")).let { if (it.isNull("first")) null else it.getLong("first") }

    /** Raw points, all accuracies, unthinned up to 200k. */
    fun points(from: Long, to: Long, user: Long? = null): Pair<List<PointRow>, Int> {
        val o = JSONObject(call("GET", "/api/v1/points" + q("from" to from, "to" to to, "raw" to 1, "max" to 200_000, "user" to user)))
        val a = o.getJSONArray("points")
        val out = ArrayList<PointRow>(a.length())
        for (i in 0 until a.length()) {
            val r = a.getJSONArray(i)
            out += PointRow(r.getLong(0), r.getDouble(1), r.getDouble(2), r.optD(3), r.optD(5), r.optD(4), null,
                if (r.isNull(6)) null else r.getInt(6), synced = true)
        }
        return out to o.optInt("step", 1)
    }

    fun timeline(from: Long, to: Long, user: Long? = null): Pair<List<Visit>, List<Trip>> {
        val o = JSONObject(call("GET", "/api/v1/timeline" + q("from" to from, "to" to to, "user" to user)))
        val visits = o.getJSONArray("visits").objects().map { v ->
            Visit(v.getLong("start"), v.getLong("end"), v.getDouble("lat"), v.getDouble("lon"), v.optDouble("radius", 50.0),
                v.optInt("points"), v.str("custom_name") ?: v.str("place_name") ?: v.str("name"),
                v.str("address"), v.str("city"), v.str("country"))
        }
        val trips = o.getJSONArray("trips").objects().map { t ->
            Trip(t.getLong("start"), t.getLong("end"), t.getDouble("distance"), t.optString("mode", "unknown"), t.optDouble("confidence", 0.0))
        }
        return visits to trips
    }

    fun setTripMode(start: Long, mode: String) = call("PUT", "/api/v1/trips/mode", JSONObject().put("start", start).put("mode", mode))
    fun renameVisit(start: Long, name: String) = call("PATCH", "/api/v1/visits", JSONObject().put("start", start).put("name", name))
    fun hideVisit(start: Long, hidden: Boolean) = call("PATCH", "/api/v1/visits", JSONObject().put("start", start).put("hidden", hidden))

    fun insights(from: Long, to: Long, tz: String): Insights {
        val o = JSONObject(call("GET", "/api/v1/insights" + q("from" to from, "to" to to, "tz" to tz)))
        fun areas(k: String) = o.optJSONArray(k).objects().map { AreaStat(it.getString("name"), it.optInt("visits"), it.optLong("ms")) }
        return Insights(
            distance = o.optDouble("distance"), movingMs = o.optLong("moving_ms"), visits = o.optInt("visits"),
            trips = o.optInt("trips"), places = o.optInt("places"), daysTracked = o.optInt("days_tracked"),
            modes = o.optJSONArray("modes").objects().map { ModeStat(it.getString("mode"), it.optDouble("distance"), it.optLong("ms"), it.optInt("trips")) },
            topPlaces = o.optJSONArray("top_places").objects().map {
                PlaceStat(it.optString("name").ifEmpty { "Unnamed place" }, it.getDouble("lat"), it.getDouble("lon"), it.optInt("visits"), it.optLong("ms"))
            },
            countries = areas("countries"), cities = areas("cities"),
            daily = o.optJSONArray("daily").objects().map { DayStat(it.getString("day"), it.optDouble("distance"), it.optLong("moving_ms"), it.optInt("visits"), it.optInt("points")) },
            longest = o.optJSONObject("longest_trip")?.let { Trip(it.getLong("start"), it.getLong("end"), it.getDouble("distance"), it.optString("mode"), 0.0) },
        )
    }

    fun places(): List<PlaceRow> = JSONArray(call("GET", "/api/v1/places")).objects().map {
        PlaceRow(remoteId = it.getLong("id"), name = it.getString("name"), lat = it.getDouble("lat"), lon = it.getDouble("lon"),
            radius = it.optDouble("radius", 75.0), private = it.optBoolean("private"), visits = it.optInt("visits"), totalMs = it.optLong("total_ms"))
    }

    fun savePlace(p: PlaceRow) {
        val body = JSONObject().put("name", p.name).put("lat", p.lat).put("lon", p.lon).put("radius", p.radius).put("private", p.private)
        if (p.remoteId != null) call("PUT", "/api/v1/places/${p.remoteId}", body) else call("POST", "/api/v1/places", body)
    }

    fun deletePlace(remoteId: Long) = call("DELETE", "/api/v1/places/$remoteId")

    fun family(): List<Person> = JSONArray(call("GET", "/api/v1/family")).objects().map { o ->
        val p = o.optJSONObject("point")?.let { Fix(it.getLong("ts"), it.getDouble("lat"), it.getDouble("lon"), acc = it.optD("acc"), batt = it.optI("batt")) }
        Person(o.getLong("user_id"), o.getString("name"), o.optString("color", "#0d9488"), p, o.str("place"), o.optBoolean("paused"))
    }

    fun shares(): List<Share> = JSONArray(call("GET", "/api/v1/shares")).objects().map {
        Share(it.getLong("id"), it.getString("name"), it.getString("kind"), it.optString("precision"), it.getLong("expires_at"), it.optInt("views"))
    }

    /** Returns the public link. */
    fun createShare(name: String, hours: Int, approx: Boolean, from: Long? = null, to: Long? = null): String {
        val body = JSONObject().put("name", name).put("kind", if (from != null) "range" else "live")
            .put("precision", if (approx) "approx" else "exact").put("expires_in_hours", hours).putOpt("from", from).putOpt("to", to)
        return JSONObject(call("POST", "/api/v1/shares", body)).getString("url")
    }

    fun deleteShare(id: Long) = call("DELETE", "/api/v1/shares/$id")

    /** Address search through the server's geocoder: (label, lat, lon). */
    fun search(text: String): List<Triple<String, Double, Double>> =
        JSONArray(call("GET", "/api/v1/geocode/search" + q("q" to text))).objects().map {
            Triple(it.optString("name").ifEmpty { it.optString("address") }, it.getDouble("lat"), it.getDouble("lon"))
        }
}

fun JSONArray?.objects(): List<JSONObject> = if (this == null) emptyList() else (0 until length()).map { getJSONObject(it) }
private fun JSONObject.str(k: String) = optString(k).takeIf { it.isNotEmpty() }
private fun JSONObject.optD(k: String) = if (isNull(k)) null else optDouble(k)
private fun JSONObject.optI(k: String) = if (isNull(k)) null else optInt(k)
private fun JSONArray.optD(i: Int) = if (isNull(i)) null else getDouble(i)
