package app.geotracker.ui

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import app.geotracker.Fix
import app.geotracker.Person
import app.geotracker.PlaceRow
import app.geotracker.Visit
import org.json.JSONObject
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.geometry.LatLngBounds
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style
import org.maplibre.android.offline.OfflineManager
import org.maplibre.android.offline.OfflineRegion
import org.maplibre.android.offline.OfflineRegionError
import org.maplibre.android.offline.OfflineRegionStatus
import org.maplibre.android.offline.OfflineTilePyramidRegionDefinition
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.CircleLayer
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory.*
import org.maplibre.android.style.layers.SymbolLayer
import org.maplibre.android.style.sources.GeoJsonSource
import org.maplibre.geojson.Feature
import org.maplibre.geojson.FeatureCollection
import org.maplibre.geojson.LineString
import org.maplibre.geojson.Point

// OpenFreeMap: the web app's default basemap. Free, no key, and cacheable for offline use.
fun styleUrl(dark: Boolean) = if (dark) "https://tiles.openfreemap.org/styles/dark" else "https://tiles.openfreemap.org/styles/liberty"

/** Everything a map can show; the composable diffs nothing, it just re-sets the sources. */
data class MapData(
    val path: List<Fix> = emptyList(),
    val highlight: List<Fix> = emptyList(),
    val visits: List<Visit> = emptyList(),
    val places: List<PlaceRow> = emptyList(),
    val people: List<Person> = emptyList(),
    val me: Fix? = null,
    val pick: LatLng? = null,
    val pickRadius: Double = 0.0,
)

@Composable
fun GeoMap(
    data: MapData,
    modifier: Modifier = Modifier,
    dark: Boolean,
    fit: List<LatLng>? = null,      // fit the camera to these whenever the list changes
    follow: LatLng? = null,          // or keep this point centred
    onClick: ((LatLng) -> Unit)? = null,
    onMap: (MapLibreMap) -> Unit = {},
) {
    val ctx = LocalContext.current
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val view = remember { MapView(ctx).apply { onCreate(null) } }
    var map by remember { mutableStateOf<MapLibreMap?>(null) }
    var style by remember { mutableStateOf<Style?>(null) }
    val primary = if (dark) Color(0xFF2DD4BF) else Color(0xFF0D9488)
    val accent = Color(0xFFF97316)

    DisposableEffect(lifecycle) {
        val obs = LifecycleEventObserver { _, e ->
            when (e) {
                Lifecycle.Event.ON_START -> view.onStart()
                Lifecycle.Event.ON_RESUME -> view.onResume()
                Lifecycle.Event.ON_PAUSE -> view.onPause()
                Lifecycle.Event.ON_STOP -> view.onStop()
                else -> {}
            }
        }
        lifecycle.addObserver(obs)
        if (lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) view.onStart()
        if (lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)) view.onResume()
        onDispose {
            lifecycle.removeObserver(obs)
            view.onPause(); view.onStop(); view.onDestroy()
        }
    }

    LaunchedEffect(dark) {
        style = null
        view.getMapAsync { m ->
            map = m
            m.uiSettings.isAttributionEnabled = true
            m.uiSettings.isLogoEnabled = false
            m.uiSettings.isCompassEnabled = true
            m.uiSettings.setCompassMargins(0, 220, 32, 0)
            m.uiSettings.isRotateGesturesEnabled = true
            m.addOnMapClickListener { onClick?.invoke(it); onClick != null }
            m.setStyle(Style.Builder().fromUri(styleUrl(dark))) { s ->
                addLayers(s, primary, accent, dark)
                style = s
            }
            onMap(m)
        }
    }

    LaunchedEffect(style, data) { style?.let { render(it, data) } }

    LaunchedEffect(map, fit) {
        val m = map ?: return@LaunchedEffect
        val pts = fit?.takeIf { it.isNotEmpty() } ?: return@LaunchedEffect
        if (pts.size == 1 || pts.all { it == pts[0] }) m.animateCamera(CameraUpdateFactory.newLatLngZoom(pts[0], 15.0), 600)
        else m.animateCamera(CameraUpdateFactory.newLatLngBounds(LatLngBounds.Builder().includes(pts).build(), 120, 260, 120, 160), 600)
    }

    LaunchedEffect(map, follow) {
        val m = map ?: return@LaunchedEffect
        follow?.let { m.animateCamera(CameraUpdateFactory.newLatLngZoom(it, maxOf(m.cameraPosition.zoom, 15.0)), 600) }
    }

    AndroidView({ view }, modifier)
}

private fun addLayers(s: Style, primary: Color, accent: Color, dark: Boolean) {
    val ink = android.graphics.Color.parseColor(if (dark) "#fafaf9" else "#1c1917")
    val halo = android.graphics.Color.parseColor(if (dark) "#0c0a09" else "#ffffff")
    listOf("path", "highlight", "visits", "places", "people", "me", "pick").forEach { s.addSource(GeoJsonSource(it)) }
    s.addLayer(CircleLayer("places", "places").withProperties(
        meterRadius(),
        circleColor(primary.toArgb()), circleOpacity(0.10f), circleStrokeColor(primary.toArgb()), circleStrokeWidth(1f), circleStrokeOpacity(0.5f),
    ))
    s.addLayer(LineLayer("path-casing", "path").withProperties(
        lineColor(android.graphics.Color.WHITE), lineWidth(7f), lineOpacity(0.7f), lineCap(Property.LINE_CAP_ROUND), lineJoin(Property.LINE_JOIN_ROUND)))
    s.addLayer(LineLayer("path", "path").withProperties(
        lineColor(primary.toArgb()), lineWidth(4f), lineCap(Property.LINE_CAP_ROUND), lineJoin(Property.LINE_JOIN_ROUND)))
    s.addLayer(LineLayer("highlight", "highlight").withProperties(
        lineColor(accent.toArgb()), lineWidth(6f), lineCap(Property.LINE_CAP_ROUND), lineJoin(Property.LINE_JOIN_ROUND)))
    s.addLayer(CircleLayer("visits", "visits").withProperties(
        circleRadius(7f), circleColor(android.graphics.Color.WHITE), circleStrokeColor(primary.toArgb()), circleStrokeWidth(3.5f)))
    s.addLayer(SymbolLayer("visit-labels", "visits").withProperties(
        textField(Expression.get("name")), textSize(12f), textOffset(arrayOf(0f, 1.4f)), textAnchor(Property.TEXT_ANCHOR_TOP),
        textColor(ink), textHaloColor(halo), textHaloWidth(1.5f), textOptional(true)))
    s.addLayer(CircleLayer("people", "people").withProperties(
        circleRadius(9f), circleColor(Expression.get("color")), circleStrokeColor(android.graphics.Color.WHITE), circleStrokeWidth(3f)))
    s.addLayer(SymbolLayer("people-labels", "people").withProperties(
        textField(Expression.get("name")), textSize(12f), textOffset(arrayOf(0f, 1.5f)), textAnchor(Property.TEXT_ANCHOR_TOP),
        textColor(ink), textHaloColor(halo), textHaloWidth(1.5f)))
    s.addLayer(CircleLayer("me-acc", "me").withProperties(
        meterRadius(),
        circleColor(primary.toArgb()), circleOpacity(0.15f)))
    s.addLayer(CircleLayer("me", "me").withProperties(
        circleRadius(8f), circleColor(primary.toArgb()), circleStrokeColor(android.graphics.Color.WHITE), circleStrokeWidth(3f)))
    s.addLayer(CircleLayer("pick-radius", "pick").withProperties(
        meterRadius(),
        circleColor(accent.toArgb()), circleOpacity(0.18f), circleStrokeColor(accent.toArgb()), circleStrokeWidth(1.5f)))
    s.addLayer(CircleLayer("pick", "pick").withProperties(
        circleRadius(7f), circleColor(accent.toArgb()), circleStrokeColor(android.graphics.Color.WHITE), circleStrokeWidth(3f)))
}

/**
 * Circles sized in meters: "px" is the radius in pixels at zoom 20 (MapLibre's 512 px tiles are
 * ~0.0746 m/px there at the equator), and base-2 interpolation scales it with zoom exactly.
 */
private fun metersAtZ20(m: Double, lat: Double) = m / (0.0746 * Math.cos(Math.toRadians(lat)))

private fun meterRadius() = circleRadius(Expression.interpolate(Expression.exponential(2), Expression.zoom(),
    Expression.stop(0, 0), Expression.stop(22, Expression.product(Expression.get("px"), Expression.literal(4)))))

private fun pt(lat: Double, lon: Double) = Point.fromLngLat(lon, lat)

private fun render(s: Style, d: MapData) {
    fun set(id: String, fc: FeatureCollection) = s.getSourceAs<GeoJsonSource>(id)?.setGeoJson(fc)
    fun line(pts: List<Fix>) = FeatureCollection.fromFeatures(
        // Break the line over long silent gaps so we don't draw straight lines across cities.
        splitGaps(pts).filter { it.size > 1 }.map { seg -> Feature.fromGeometry(LineString.fromLngLats(seg.map { pt(it.lat, it.lon) })) })
    set("path", line(d.path))
    set("highlight", line(d.highlight))
    set("visits", FeatureCollection.fromFeatures(d.visits.map { v ->
        Feature.fromGeometry(pt(v.lat, v.lon)).apply { addStringProperty("name", v.name ?: "") }
    }))
    set("places", FeatureCollection.fromFeatures(d.places.map { p ->
        Feature.fromGeometry(pt(p.lat, p.lon)).apply { addNumberProperty("px", metersAtZ20(p.radius, p.lat)) }
    }))
    set("people", FeatureCollection.fromFeatures(d.people.mapNotNull { p ->
        p.point?.let { f ->
            Feature.fromGeometry(pt(f.lat, f.lon)).apply {
                addStringProperty("name", p.name); addStringProperty("color", p.color.ifEmpty { "#0d9488" })
            }
        }
    }))
    set("me", FeatureCollection.fromFeatures(listOfNotNull(d.me?.let { f ->
        Feature.fromGeometry(pt(f.lat, f.lon)).apply { addNumberProperty("px", metersAtZ20(f.acc ?: 0.0, f.lat)) }
    })))
    set("pick", FeatureCollection.fromFeatures(listOfNotNull(d.pick?.let { p ->
        Feature.fromGeometry(pt(p.latitude, p.longitude)).apply { addNumberProperty("px", metersAtZ20(d.pickRadius, p.latitude)) }
    })))
}

fun splitGaps(pts: List<Fix>, gapMs: Long = 30 * 60_000): List<List<Fix>> {
    val out = ArrayList<List<Fix>>()
    var cur = ArrayList<Fix>()
    for (p in pts) {
        if (cur.isNotEmpty() && p.ts - cur.last().ts > gapMs && app.geotracker.distance(cur.last().lat, cur.last().lon, p.lat, p.lon) > 2000) {
            out += cur; cur = ArrayList()
        }
        cur += p
    }
    if (cur.isNotEmpty()) out += cur
    return out
}

fun Fix.latLng() = LatLng(lat, lon)

// ── Offline regions ──────────────────────────────────────────

data class Region(val region: OfflineRegion, val name: String, val styleDark: Boolean)

object Offline {
    fun list(ctx: Context, done: (List<Region>) -> Unit) {
        OfflineManager.getInstance(ctx).listOfflineRegions(object : OfflineManager.ListOfflineRegionsCallback {
            override fun onList(offlineRegions: Array<OfflineRegion>?) {
                done(offlineRegions.orEmpty().map { r ->
                    val meta = runCatching { JSONObject(String(r.metadata)) }.getOrNull()
                    Region(r, meta?.optString("name") ?: "Area ${r.id}", meta?.optBoolean("dark") ?: false)
                })
            }
            override fun onError(error: String) = done(emptyList())
        })
    }

    /** Downloads the visible area, zoom [current-1 .. 16], for the given style. */
    fun download(ctx: Context, bounds: LatLngBounds, zoom: Double, dark: Boolean, name: String, progress: (Int, Boolean, String?) -> Unit) {
        val mgr = OfflineManager.getInstance(ctx)
        mgr.setOfflineMapboxTileCountLimit(200_000)
        val def = OfflineTilePyramidRegionDefinition(styleUrl(dark), bounds, maxOf(0.0, zoom - 1), 16.0, ctx.resources.displayMetrics.density)
        val meta = JSONObject().put("name", name).put("dark", dark).toString().toByteArray()
        mgr.createOfflineRegion(def, meta, object : OfflineManager.CreateOfflineRegionCallback {
            override fun onCreate(offlineRegion: OfflineRegion) {
                offlineRegion.setObserver(object : OfflineRegion.OfflineRegionObserver {
                    override fun onStatusChanged(status: OfflineRegionStatus) {
                        val pct = if (status.requiredResourceCount > 0) (100 * status.completedResourceCount / status.requiredResourceCount).toInt() else 0
                        progress(pct, status.isComplete, null)
                        if (status.isComplete) offlineRegion.setDownloadState(OfflineRegion.STATE_INACTIVE)
                    }
                    override fun onError(error: OfflineRegionError) = progress(0, false, error.message)
                    override fun mapboxTileCountLimitExceeded(limit: Long) = progress(0, true, "Area too large: zoom in and try again")
                })
                offlineRegion.setDownloadState(OfflineRegion.STATE_ACTIVE)
            }
            override fun onError(error: String) = progress(0, true, error)
        })
    }
}
