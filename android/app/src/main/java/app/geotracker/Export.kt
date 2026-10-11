package app.geotracker

import java.io.OutputStream
import java.time.Instant

/** Streams local points as GPX, GeoJSON or CSV, a page at a time so years of data fit in memory. */
object Export {
    suspend fun write(format: String, out: OutputStream, from: Long = 0, to: Long = Long.MAX_VALUE): Int {
        val w = out.bufferedWriter()
        var n = 0
        when (format) {
            "gpx" -> w.write("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<gpx version=\"1.1\" creator=\"GeoTracker Android\" xmlns=\"http://www.topografix.com/GPX/1/1\">\n<trk><name>GeoTracker</name><trkseg>\n")
            "geojson" -> w.write("{\"type\":\"FeatureCollection\",\"features\":[\n")
            else -> w.write("time,lat,lon,accuracy,altitude,speed,bearing,battery\n")
        }
        var after = from - 1
        while (true) {
            val page = App.db.points().page(after, to, 5000)
            if (page.isEmpty()) break
            for (p in page) {
                val time = Instant.ofEpochMilli(p.ts).toString()
                when (format) {
                    "gpx" -> w.write("<trkpt lat=\"${p.lat}\" lon=\"${p.lon}\">${p.alt?.let { "<ele>$it</ele>" } ?: ""}<time>$time</time>" +
                        "${p.acc?.let { "<hdop>${"%.1f".format(java.util.Locale.ROOT, it / 5)}</hdop>" } ?: ""}</trkpt>\n")
                    "geojson" -> w.write((if (n > 0) ",\n" else "") +
                        "{\"type\":\"Feature\",\"geometry\":{\"type\":\"Point\",\"coordinates\":[${p.lon},${p.lat}${p.alt?.let { ",$it" } ?: ""}]}," +
                        "\"properties\":{\"time\":\"$time\",\"accuracy\":${p.acc},\"speed\":${p.speed},\"bearing\":${p.bearing},\"battery\":${p.batt}}}")
                    else -> w.write("$time,${p.lat},${p.lon},${p.acc ?: ""},${p.alt ?: ""},${p.speed ?: ""},${p.bearing ?: ""},${p.batt ?: ""}\n")
                }
                n++
            }
            after = page.last().ts
        }
        when (format) {
            "gpx" -> w.write("</trkseg></trk>\n</gpx>\n")
            "geojson" -> w.write("\n]}\n")
        }
        w.flush()
        return n
    }
}
