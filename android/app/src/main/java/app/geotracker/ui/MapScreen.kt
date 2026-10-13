package app.geotracker.ui

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.animation.core.animateFloat
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import app.geotracker.App
import app.geotracker.Api
import app.geotracker.Fix
import app.geotracker.Person
import app.geotracker.Repo
import app.geotracker.TimelineBuilder
import app.geotracker.TrackingService
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.maps.MapLibreMap
import java.time.LocalDate

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MapScreen(dark: Boolean, openDay: (LocalDate) -> Unit) {
    val ctx = LocalContext.current
    val track by TrackingService.state.collectAsState()
    val latest by App.db.points().latest().collectAsState(null)
    val queued by App.db.points().unsyncedCount().collectAsState(0)
    val places by App.db.places().all().collectAsState(emptyList())
    val version by App.prefs.version.collectAsState()
    val connected = App.prefs.connected.also { version }

    var today by remember { mutableStateOf<List<Fix>>(emptyList()) }
    var todayDist by remember { mutableDoubleStateOf(0.0) }
    var people by remember { mutableStateOf<List<Person>>(emptyList()) }
    var follow by remember { mutableStateOf<CameraTarget?>(null) }
    var fit by remember { mutableStateOf<CameraTarget?>(null) }
    var map by remember { mutableStateOf<MapLibreMap?>(null) }
    var showPeople by remember { mutableStateOf(false) }
    var download by remember { mutableStateOf(false) }

    // Today's path from the local database; re-read as fixes arrive.
    LaunchedEffect(latest?.ts) {
        val (from, to) = Repo.dayRange(LocalDate.now())
        val pts = withContext(Dispatchers.IO) { App.db.points().range(from, to).map { it.fix() } }
        today = pts
        todayDist = TimelineBuilder().apply { pts.forEach(::add); finish() }.trips.sumOf { it.distance }
        if (fit == null) latest?.let { fit = CameraTarget(it.fix().latLng()) } // first open: zoom to where we are
    }
    // Family members' live positions, refreshed while the map is open.
    LaunchedEffect(connected) {
        while (connected) {
            people = withContext(Dispatchers.IO) { runCatching { Api.family() }.getOrDefault(people) }
            delay(30_000)
        }
    }

    val me = track.last ?: latest?.fix()
    Box(Modifier.fillMaxSize()) {
        GeoMap(
            MapData(path = today, places = places, people = people, me = me),
            Modifier.fillMaxSize(), dark = dark, fit = fit, follow = follow, fitTopDp = 230, fitBottomDp = 130, onMap = { map = it },
        )

        // Status card
        Column(Modifier.fillMaxWidth().statusBarsPadding().padding(12.dp)) {
            StatusCard(track.running, track.paused, track.moving, me, queued, connected, todayDist, today.size) { openDay(LocalDate.now()) }
        }

        // Map controls
        Column(Modifier.align(Alignment.CenterEnd).padding(end = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            MapButton(Icons.Rounded.MyLocation, "My location") { me?.let { follow = CameraTarget(it.latLng()) } }
            MapButton(Icons.Rounded.Route, "Fit today") { today.map { it.latLng() }.takeIf { it.isNotEmpty() }?.let { fit = CameraTarget(it) } }
            MapButton(Icons.Rounded.DownloadForOffline, "Download area for offline") { download = true }
            if (connected && people.isNotEmpty()) MapButton(Icons.Rounded.Groups, "People") { showPeople = true }
        }

        TrackButton(track.running, Modifier.align(Alignment.BottomCenter).padding(bottom = 20.dp))
    }

    if (showPeople) ModalBottomSheet({ showPeople = false }) {
        Text("People", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(horizontal = 20.dp))
        LazyColumn(contentPadding = PaddingValues(12.dp)) {
            items(people) { p ->
                ListItem(
                    headlineContent = { Text(p.name) },
                    supportingContent = {
                        Text(when {
                            p.paused -> "Sharing paused"
                            p.point == null -> "Not sharing live location"
                            else -> listOfNotNull(p.place, Fmt.ago(p.point.ts), p.point.batt?.let { "$it% battery" }).joinToString(" · ")
                        })
                    },
                    leadingContent = {
                        Box(Modifier.size(40.dp).background(parseColor(p.color), CircleShape), contentAlignment = Alignment.Center) {
                            Text(p.name.take(1).uppercase(), color = Color.White, fontWeight = FontWeight.Bold)
                        }
                    },
                    modifier = Modifier.clickable(enabled = p.point != null) { showPeople = false; follow = CameraTarget(p.point!!.latLng()) },
                )
            }
        }
        Spacer(Modifier.navigationBarsPadding())
    }

    if (download) DownloadDialog(dark, map) { download = false }
}

@Composable
private fun StatusCard(
    running: Boolean, paused: String?, moving: Boolean, me: Fix?, queued: Int, connected: Boolean,
    todayDist: Double, todayPoints: Int, onToday: () -> Unit,
) {
    val color = when {
        !running -> MaterialTheme.colorScheme.outline
        paused != null -> Color(0xFFF59E0B)
        else -> Color(0xFF22C55E)
    }
    Surface(
        shape = RoundedCornerShape(24.dp), color = MaterialTheme.colorScheme.surface.copy(alpha = 0.96f),
        modifier = Modifier.fillMaxWidth().shadow(10.dp, RoundedCornerShape(24.dp)),
    ) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                PulseDot(color, running && paused == null)
                Spacer(Modifier.width(10.dp))
                Column(Modifier.weight(1f)) {
                    Text(
                        when {
                            !running -> "Not recording"
                            paused != null -> "Paused in $paused"
                            moving -> "Recording · moving"
                            else -> "Recording · stationary"
                        }, style = MaterialTheme.typography.titleMedium,
                    )
                    Text(
                        me?.let {
                            listOfNotNull(
                                it.acc?.let { a -> "±${a.toInt()} m" }, it.speed?.takeIf { s -> s > 0.5 }?.let(Fmt::speed),
                                it.alt?.let { a -> "${a.toInt()} m alt" }, Fmt.ago(it.ts),
                            ).joinToString(" · ")
                        } ?: "No location yet",
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                SyncChip(connected, queued)
            }
            HorizontalDivider(Modifier.padding(vertical = 12.dp), color = MaterialTheme.colorScheme.outlineVariant)
            Row(Modifier.fillMaxWidth().clickable(onClick = onToday), verticalAlignment = Alignment.CenterVertically) {
                Stat("Today", Fmt.dist(todayDist), Modifier.weight(1f))
                Stat("Fixes", "%,d".format(todayPoints), Modifier.weight(1f))
                Stat("Battery", me?.batt?.let { "$it%" } ?: "–", Modifier.weight(1f))
                Icon(Icons.Rounded.ChevronRight, "Open today's timeline", tint = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
private fun SyncChip(connected: Boolean, queued: Int) {
    val version by App.prefs.version.collectAsState()
    val err = App.prefs.syncError.also { version }
    val (icon, text, tint) = when {
        !connected -> Triple(Icons.Rounded.PhoneAndroid, "On device", MaterialTheme.colorScheme.onSurfaceVariant)
        err.isNotEmpty() -> Triple(Icons.Rounded.CloudOff, "$queued queued", MaterialTheme.colorScheme.error)
        queued > 0 -> Triple(Icons.Rounded.CloudUpload, "$queued queued", MaterialTheme.colorScheme.primary)
        else -> Triple(Icons.Rounded.CloudDone, "Synced", MaterialTheme.colorScheme.primary)
    }
    Row(
        Modifier.background(MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(50)).padding(horizontal = 10.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, null, tint = tint, modifier = Modifier.size(16.dp))
        Spacer(Modifier.width(6.dp))
        Text(text, style = MaterialTheme.typography.labelMedium, color = tint)
    }
}

@Composable
private fun PulseDot(color: Color, pulse: Boolean) {
    val t = rememberInfiniteTransitionCompat(pulse)
    Box(Modifier.size(22.dp), contentAlignment = Alignment.Center) {
        if (pulse) Box(Modifier.size((10 + 12 * t).dp).background(color.copy(alpha = 0.35f * (1 - t)), CircleShape))
        Box(Modifier.size(10.dp).background(color, CircleShape))
    }
}

@Composable
private fun rememberInfiniteTransitionCompat(on: Boolean): Float {
    if (!on) return 0f
    val tr = androidx.compose.animation.core.rememberInfiniteTransition(label = "pulse")
    val v by tr.animateFloat(
        0f, 1f, androidx.compose.animation.core.infiniteRepeatable(androidx.compose.animation.core.tween(1600)), label = "pulse",
    )
    return v
}

@Composable
fun MapButton(icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, onClick: () -> Unit) {
    SmallFloatingActionButton(
        onClick, containerColor = MaterialTheme.colorScheme.surface, contentColor = MaterialTheme.colorScheme.onSurface,
        shape = RoundedCornerShape(14.dp),
    ) { Icon(icon, label) }
}

@Composable
private fun TrackButton(running: Boolean, modifier: Modifier) {
    val ctx = LocalContext.current
    var askBackground by remember { mutableStateOf(false) }
    val background = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {}
    var denied by remember { mutableStateOf(false) }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { res ->
        // Fine location can be missing when only "approximate" was chosen; recording needs precise.
        if (granted(ctx, Manifest.permission.ACCESS_FINE_LOCATION) && TrackingService.start(ctx)) {
            if (!granted(ctx, Manifest.permission.ACCESS_BACKGROUND_LOCATION)) askBackground = true
        } else {
            denied = true
        }
    }
    ExtendedFloatingActionButton(
        onClick = {
            if (running) TrackingService.stop(ctx)
            else launcher.launch(buildList {
                add(Manifest.permission.ACCESS_FINE_LOCATION); add(Manifest.permission.ACCESS_COARSE_LOCATION)
                if (Build.VERSION.SDK_INT >= 33) add(Manifest.permission.POST_NOTIFICATIONS)
            }.toTypedArray())
        },
        icon = { Icon(if (running) Icons.Rounded.Stop else Icons.Rounded.FiberManualRecord, null) },
        text = { Text(if (running) "Stop recording" else "Start recording") },
        containerColor = if (running) MaterialTheme.colorScheme.surface else MaterialTheme.colorScheme.primary,
        contentColor = if (running) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onPrimary,
        shape = RoundedCornerShape(28.dp),
        modifier = modifier,
    )
    if (denied) AlertDialog(
        onDismissRequest = { denied = false },
        icon = { Icon(Icons.Rounded.LocationOff, null) },
        title = { Text("Precise location needed") },
        text = { Text("GeoTracker can't record without precise location. Open the app's permissions and allow \"Precise\" location.") },
        confirmButton = {
            TextButton({
                denied = false
                ctx.startActivity(android.content.Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
                    android.net.Uri.parse("package:${ctx.packageName}")))
            }) { Text("Open settings") }
        },
        dismissButton = { TextButton({ denied = false }) { Text("Not now") } },
    )
    if (askBackground) AlertDialog(
        onDismissRequest = { askBackground = false },
        icon = { Icon(Icons.Rounded.LocationOn, null) },
        title = { Text("Keep recording in the background") },
        text = { Text("To record your timeline while the screen is off or after a restart, choose \"Allow all the time\" on the next screen. Your locations stay on this phone and your own server.") },
        confirmButton = { TextButton({ askBackground = false; background.launch(Manifest.permission.ACCESS_BACKGROUND_LOCATION) }) { Text("Continue") } },
        dismissButton = { TextButton({ askBackground = false }) { Text("Not now") } },
    )
}

fun granted(ctx: Context, p: String) = ContextCompat.checkSelfPermission(ctx, p) == PackageManager.PERMISSION_GRANTED

fun parseColor(hex: String) = runCatching { Color(android.graphics.Color.parseColor(hex)) }.getOrDefault(Color(0xFF0D9488))

@Composable
private fun DownloadDialog(dark: Boolean, map: MapLibreMap?, close: () -> Unit) {
    val ctx = LocalContext.current
    var name by remember { mutableStateOf("Area ${LocalDate.now()}") }
    var progress by remember { mutableStateOf<Int?>(null) }
    var done by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    AlertDialog(
        onDismissRequest = { if (progress == null || done) close() },
        icon = { Icon(Icons.Rounded.DownloadForOffline, null) },
        title = { Text("Save this area offline") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text("Downloads the map you see now, down to street level, so it works without a connection.",
                    style = MaterialTheme.typography.bodyMedium)
                OutlinedTextField(name, { name = it }, label = { Text("Name") }, singleLine = true, enabled = progress == null)
                progress?.let {
                    LinearProgressIndicator({ it / 100f }, Modifier.fillMaxWidth())
                    Text(if (done) "Saved. Manage saved areas in Settings." else "$it%", style = MaterialTheme.typography.bodySmall)
                }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            if (progress == null) TextButton({
                val m = map ?: return@TextButton
                progress = 0
                Offline.download(ctx, m.projection.visibleRegion.latLngBounds, m.cameraPosition.zoom, dark, name.trim()) { pct, complete, err ->
                    progress = pct; done = complete && err == null; error = err
                }
            }) { Text("Download") }
            else TextButton(close) { Text(if (done || error != null) "Close" else "Hide") }
        },
        dismissButton = { if (progress == null) TextButton(close) { Text("Cancel") } },
    )
}
