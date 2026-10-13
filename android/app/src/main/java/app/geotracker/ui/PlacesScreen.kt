package app.geotracker.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import app.geotracker.Api
import app.geotracker.App
import app.geotracker.PlaceRow
import app.geotracker.Repo
import app.geotracker.TrackingService
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import kotlin.math.roundToInt
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.maplibre.android.geometry.LatLng

@Composable
fun PlacesScreen(dark: Boolean) {
    val places by App.db.places().all().collectAsState(emptyList())
    var editing by remember { mutableStateOf<PlaceRow?>(null) }
    var query by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(Unit) { Repo.refreshPlaces() }

    Box(Modifier.fillMaxSize()) {
        LazyColumn(Modifier.fillMaxSize().statusBarsPadding(), contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 96.dp)) {
            item {
                Text("Places", style = MaterialTheme.typography.headlineMedium, modifier = Modifier.padding(vertical = 8.dp))
                Text("Saved places name your visits. Pause zones stop recording on this phone; privacy zones hide you from family and share links.",
                    style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (places.size > 6) OutlinedTextField(query, { query = it }, Modifier.fillMaxWidth().padding(top = 12.dp),
                    placeholder = { Text("Search places") }, leadingIcon = { Icon(Icons.Rounded.Search, null) }, singleLine = true,
                    shape = RoundedCornerShape(16.dp))
                Spacer(Modifier.height(12.dp))
            }
            val shown = places.filter { query.isBlank() || it.name.contains(query, true) }
            if (shown.isEmpty()) item { EmptyState(Icons.Rounded.Place, "No places yet", "Add home, work and the places you visit often.") }
            items(shown, key = { it.id }) { p ->
                Surface(
                    Modifier.fillMaxWidth().padding(vertical = 4.dp).clickable { editing = p },
                    shape = RoundedCornerShape(16.dp), color = MaterialTheme.colorScheme.surface,
                    border = androidx.compose.foundation.BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
                ) {
                    Row(Modifier.padding(14.dp), verticalAlignment = Alignment.CenterVertically) {
                        Box(Modifier.size(42.dp).background(MaterialTheme.colorScheme.primaryContainer, RoundedCornerShape(12.dp)), contentAlignment = Alignment.Center) {
                            Icon(if (p.pause) Icons.Rounded.PauseCircle else Icons.Rounded.Place, null, tint = MaterialTheme.colorScheme.primary)
                        }
                        Spacer(Modifier.width(14.dp))
                        Column(Modifier.weight(1f)) {
                            Text(p.name, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(
                                listOfNotNull(
                                    "${p.radius.toInt()} m",
                                    p.visits.takeIf { it > 0 }?.let { "$it visits · ${Fmt.dur(p.totalMs)}" },
                                    if (p.pause) "pause zone" else null,
                                    if (p.private) "privacy zone" else null,
                                    if (p.remoteId == null && App.prefs.connected) "this phone only" else null,
                                ).joinToString(" · "),
                                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        Icon(Icons.Rounded.ChevronRight, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
        ExtendedFloatingActionButton(
            onClick = { editing = PlaceRow(name = "", lat = 0.0, lon = 0.0) },
            modifier = Modifier.align(Alignment.BottomEnd).padding(16.dp),
            icon = { Icon(Icons.Rounded.Add, null) }, text = { Text("Add place") },
            containerColor = MaterialTheme.colorScheme.primary, contentColor = MaterialTheme.colorScheme.onPrimary,
        )
    }

    editing?.let { PlaceEditor(it, dark, places) { editing = null } }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun PlaceEditor(initial: PlaceRow, dark: Boolean, others: List<PlaceRow>, close: () -> Unit) {
    val scope = rememberCoroutineScope()
    val here = TrackingService.state.collectAsState().value.last ?: App.db.points().latest().collectAsState(null).value?.fix()
    val isNew = initial.lat == 0.0 && initial.lon == 0.0
    var p by remember { mutableStateOf(if (isNew && here != null) initial.copy(lat = here.lat, lon = here.lon) else initial) }
    var fit by remember { mutableStateOf(if (p.lat != 0.0 || p.lon != 0.0) CameraTarget(LatLng(p.lat, p.lon)) else null) }
    var search by remember { mutableStateOf("") }
    var hits by remember { mutableStateOf<List<Triple<String, Double, Double>>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val connected = App.prefs.connected

    fun save(block: suspend () -> Unit) {
        busy = true
        scope.launch {
            error = runCatching { block() }.exceptionOrNull()?.message
            busy = false
            if (error == null) close()
        }
    }

    fun runSearch() {
        if (search.trim().length < 3) return
        scope.launch {
            error = null
            hits = withContext(Dispatchers.IO) { runCatching { Api.search(search.trim()) }.onFailure { error = it.message }.getOrDefault(emptyList()) }
            if (hits.isEmpty() && error == null) error = "No matches. Try a fuller address."
        }
    }

    Dialog(close, DialogProperties(usePlatformDefaultWidth = false)) {
        Scaffold(
            topBar = {
                TopAppBar(
                    title = { Text(if (isNew) "New place" else "Edit place") },
                    navigationIcon = { IconButton(close) { Icon(Icons.Rounded.Close, "Close") } },
                    actions = {
                        TextButton({ save { Repo.savePlace(p.copy(name = p.name.trim())) } }, enabled = !busy && p.name.isNotBlank() && (p.lat != 0.0 || p.lon != 0.0)) {
                            Text("Save")
                        }
                    },
                )
            },
        ) { pad ->
            Column(Modifier.padding(pad).fillMaxSize()) {
                Box(Modifier.fillMaxWidth().weight(1f)) {
                    GeoMap(
                        MapData(places = others.filter { it.id != p.id }, pick = LatLng(p.lat, p.lon).takeIf { p.lat != 0.0 || p.lon != 0.0 }, pickRadius = p.radius, me = here),
                        Modifier.fillMaxSize(), dark = dark, fit = fit,
                        onClick = { p = p.copy(lat = it.latitude, lon = it.longitude) },
                    )
                    Text("Tap the map to move the place", style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.align(Alignment.BottomCenter).padding(8.dp)
                            .background(MaterialTheme.colorScheme.surface.copy(alpha = 0.9f), RoundedCornerShape(50)).padding(horizontal = 12.dp, vertical = 6.dp))
                    if (here != null) Box(Modifier.align(Alignment.TopEnd).padding(12.dp)) {
                        MapButton(Icons.Rounded.MyLocation, "Use my location") {
                            p = p.copy(lat = here.lat, lon = here.lon); fit = CameraTarget(here.latLng())
                        }
                    }
                }
                Column(Modifier.padding(16.dp).imePadding().navigationBarsPadding(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (connected) {
                        OutlinedTextField(search, { search = it }, Modifier.fillMaxWidth(), placeholder = { Text("Search an address") },
                            leadingIcon = { Icon(Icons.Rounded.Search, null) }, singleLine = true, shape = RoundedCornerShape(14.dp),
                            keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false, imeAction = ImeAction.Search),
                            keyboardActions = KeyboardActions(onSearch = { runSearch() }),
                            trailingIcon = {
                                if (search.trim().length >= 3) IconButton({ runSearch() }) { Icon(Icons.Rounded.ArrowForward, "Search") }
                            })
                        hits.take(4).forEach { (label, lat, lon) ->
                            Text(label, Modifier.fillMaxWidth().clickable {
                                p = p.copy(lat = lat, lon = lon, name = p.name.ifBlank { label.substringBefore(",") })
                                fit = CameraTarget(LatLng(lat, lon)); hits = emptyList()
                            }.padding(vertical = 6.dp), style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                    }
                    OutlinedTextField(p.name, { p = p.copy(name = it.take(80)) }, Modifier.fillMaxWidth(), label = { Text("Name") }, singleLine = true,
                        shape = RoundedCornerShape(14.dp))
                    Text("Radius · ${p.radius.toInt()} m", style = MaterialTheme.typography.labelLarge)
                    Slider(p.radius.toFloat(), { p = p.copy(radius = (it / 5).roundToInt() * 5.0) }, valueRange = 25f..500f)
                    Toggle("Pause recording here", "Nothing is recorded on this phone while you're inside.", p.pause) { p = p.copy(pause = it) }
                    if (connected) Toggle("Privacy zone", "Family and share links never see you here.", p.private) { p = p.copy(private = it) }
                    error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                    if (!isNew) TextButton({ save { Repo.deletePlace(p) } }, colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)) {
                        Icon(Icons.Rounded.Delete, null); Spacer(Modifier.width(8.dp)); Text("Delete place")
                    }
                }
            }
        }
    }
}

@Composable
fun Toggle(title: String, body: String?, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().clickable { onChange(!checked) }.padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.bodyLarge)
            body?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
        Spacer(Modifier.width(12.dp))
        Switch(checked, onChange)
    }
}
