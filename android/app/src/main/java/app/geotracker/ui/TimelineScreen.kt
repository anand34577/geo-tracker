package app.geotracker.ui

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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.geotracker.App
import app.geotracker.Day
import app.geotracker.Fix
import app.geotracker.PlaceRow
import app.geotracker.Repo
import app.geotracker.Trip
import app.geotracker.Visit
import kotlinx.coroutines.launch
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter

private sealed interface Item {
    val start: Long
    data class V(val v: Visit) : Item { override val start get() = v.start }
    data class T(val t: Trip) : Item { override val start get() = t.start }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TimelineScreen(dark: Boolean, day: LocalDate, setDay: (LocalDate) -> Unit) {
    val scope = rememberCoroutineScope()
    var data by remember { mutableStateOf<Day?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    var selected by remember { mutableStateOf<Item?>(null) }
    var editing by remember { mutableStateOf<Item?>(null) }
    var picking by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val snack = remember { SnackbarHostState() }

    LaunchedEffect(day, reload) {
        data = null; selected = null
        data = Repo.day(day)
    }

    val d = data
    val items = remember(d) { d?.let { (it.visits.map { v -> Item.V(v) } + it.trips.map { t -> Item.T(t) }).sortedBy { i -> i.start } }.orEmpty() }
    val highlight = remember(selected, d) {
        when (val s = selected) {
            is Item.T -> d?.path?.filter { it.ts in s.t.start..s.t.end }.orEmpty()
            else -> emptyList()
        }
    }
    val fit = remember(selected, d) {
        when (val s = selected) {
            is Item.V -> listOf(org.maplibre.android.geometry.LatLng(s.v.lat, s.v.lon))
            is Item.T -> highlight.map { it.latLng() }
            null -> (d?.path?.map { it.latLng() }.orEmpty() + d?.visits?.map { org.maplibre.android.geometry.LatLng(it.lat, it.lon) }.orEmpty())
        }.ifEmpty { null }
    }

    Column(Modifier.fillMaxSize()) {
        Box(Modifier.fillMaxWidth().weight(0.42f)) {
            GeoMap(MapData(path = d?.path.orEmpty(), highlight = highlight, visits = d?.visits.orEmpty()), Modifier.fillMaxSize(), dark = dark, fit = fit)
            DayBar(day, setDay, { picking = true }, Modifier.statusBarsPadding().padding(12.dp))
        }
        Box(Modifier.fillMaxWidth().weight(0.58f).background(MaterialTheme.colorScheme.background)) {
            when {
                d == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                items.isEmpty() -> Column {
                    d.note?.let { Note(it) }
                    EmptyState(Icons.Rounded.Timeline, "Nothing recorded",
                        if (d.path.isEmpty()) "No locations on this day." else "Only ${d.path.size} fixes, not enough for visits or trips yet.")
                }
                else -> LazyColumn(contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 24.dp)) {
                    item { DaySummary(d) }
                    d.note?.let { item { Note(it) } }
                    error?.let { item { Note(it, error = true) } }
                    items(items, key = { (if (it is Item.V) "v" else "t") + it.start }) { item ->
                        val sel = selected == item
                        when (item) {
                            is Item.V -> VisitRow(item.v, sel, { selected = if (sel) null else item }, { editing = item })
                            is Item.T -> TripRow(item.t, sel, { selected = if (sel) null else item }, { editing = item })
                        }
                    }
                }
            }
            SnackbarHost(snack, Modifier.align(Alignment.BottomCenter))
        }
    }

    if (picking) {
        val state = rememberDatePickerState(
            initialSelectedDateMillis = day.atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli(),
            selectableDates = object : SelectableDates {
                override fun isSelectableDate(utcTimeMillis: Long) = utcTimeMillis <= System.currentTimeMillis()
            },
        )
        DatePickerDialog(
            onDismissRequest = { picking = false },
            confirmButton = {
                TextButton({
                    state.selectedDateMillis?.let { setDay(java.time.Instant.ofEpochMilli(it).atZone(ZoneOffset.UTC).toLocalDate()) }
                    picking = false
                }) { Text("Open") }
            },
        ) { DatePicker(state) }
    }

    when (val e = editing) {
        is Item.V -> VisitSheet(e.v, { editing = null }, hide = {
            editing = null
            scope.launch {
                error = runCatching { Repo.hideVisit(e.v); reload++ }.exceptionOrNull()?.message
                if (error == null && snack.showSnackbar("Visit hidden", "Undo", duration = SnackbarDuration.Long) == SnackbarResult.ActionPerformed)
                    error = runCatching { Repo.hideVisit(e.v, hidden = false); reload++ }.exceptionOrNull()?.message
            }
        }) { action ->
            editing = null
            scope.launch {
                error = runCatching { action(); reload++ }.exceptionOrNull()?.message
            }
        }
        is Item.T -> ModeSheet(e.t, { editing = null }) { mode ->
            editing = null
            scope.launch { error = runCatching { Repo.setTripMode(e.t, mode); reload++ }.exceptionOrNull()?.message }
        }
        null -> {}
    }
}

@Composable
private fun DayBar(day: LocalDate, setDay: (LocalDate) -> Unit, pick: () -> Unit, modifier: Modifier) {
    val today = LocalDate.now()
    Surface(modifier.fillMaxWidth(), shape = RoundedCornerShape(20.dp), color = MaterialTheme.colorScheme.surface.copy(alpha = 0.96f), shadowElevation = 6.dp) {
        Row(Modifier.padding(4.dp), verticalAlignment = Alignment.CenterVertically) {
            IconButton({ setDay(day.minusDays(1)) }) { Icon(Icons.Rounded.ChevronLeft, "Previous day") }
            Column(Modifier.weight(1f).clip(RoundedCornerShape(12.dp)).clickable(onClick = pick).padding(vertical = 6.dp),
                horizontalAlignment = Alignment.CenterHorizontally) {
                Text(when (day) {
                    today -> "Today"
                    today.minusDays(1) -> "Yesterday"
                    else -> day.format(DateTimeFormatter.ofPattern("EEEE"))
                }, style = MaterialTheme.typography.titleMedium)
                Text(day.format(DateTimeFormatter.ofPattern("d MMMM yyyy")), style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            IconButton({ setDay(day.plusDays(1)) }, enabled = day < today) { Icon(Icons.Rounded.ChevronRight, "Next day") }
        }
    }
}

@Composable
private fun DaySummary(d: Day) {
    val dist = d.trips.sumOf { it.distance }
    val moving = d.trips.sumOf { it.end - it.start }
    Row(Modifier.fillMaxWidth().padding(bottom = 12.dp)) {
        Stat("Distance", Fmt.dist(dist), Modifier.weight(1f), MaterialTheme.colorScheme.primary)
        Stat("Moving", Fmt.dur(moving), Modifier.weight(1f))
        Stat("Places", "${d.visits.size}", Modifier.weight(1f))
        Stat("Fixes", "%,d".format(d.path.size), Modifier.weight(1f))
    }
}

@Composable
fun Note(text: String, error: Boolean = false) {
    Row(
        Modifier.fillMaxWidth().padding(bottom = 12.dp)
            .background(if (error) MaterialTheme.colorScheme.errorContainer else MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(12.dp))
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(if (error) Icons.Rounded.ErrorOutline else Icons.Rounded.CloudOff, null, Modifier.size(18.dp),
            tint = if (error) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.width(10.dp))
        Text(text, style = MaterialTheme.typography.bodySmall)
    }
}

/** A row on the vertical timeline: a rail on the left, content on the right. */
@Composable
private fun Rail(selected: Boolean, onClick: () -> Unit, node: @Composable () -> Unit, lineColor: Color, content: @Composable ColumnScope.() -> Unit) {
    Row(
        Modifier.fillMaxWidth().height(IntrinsicSize.Min).clip(RoundedCornerShape(16.dp))
            .background(if (selected) MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.5f) else Color.Transparent)
            .clickable(onClick = onClick).padding(horizontal = 8.dp),
    ) {
        Box(Modifier.width(40.dp).fillMaxHeight(), contentAlignment = Alignment.Center) {
            Box(Modifier.width(3.dp).fillMaxHeight().background(lineColor))
            node()
        }
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f).padding(vertical = 12.dp), content = content)
    }
}

@Composable
private fun VisitRow(v: Visit, selected: Boolean, onClick: () -> Unit, onEdit: () -> Unit) {
    Rail(selected, onClick, {
        Box(Modifier.size(28.dp).background(MaterialTheme.colorScheme.primary, CircleShape), contentAlignment = Alignment.Center) {
            Icon(Icons.Rounded.Place, null, tint = MaterialTheme.colorScheme.onPrimary, modifier = Modifier.size(16.dp))
        }
    }, MaterialTheme.colorScheme.outlineVariant) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(v.name ?: "Unnamed place", style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(
                    listOfNotNull("${Fmt.clock(v.start)} – ${Fmt.clock(v.end)}", Fmt.dur(v.end - v.start), v.address ?: v.city).joinToString(" · "),
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2,
                )
            }
            IconButton(onEdit) { Icon(Icons.Rounded.MoreVert, "Edit visit") }
        }
    }
}

@Composable
private fun TripRow(t: Trip, selected: Boolean, onClick: () -> Unit, onEdit: () -> Unit) {
    Rail(selected, onClick, { ModeBadge(t.mode, 30) }, modeColor(t.mode).copy(alpha = 0.5f)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("${modeLabel(t.mode)} · ${Fmt.dist(t.distance)}", style = MaterialTheme.typography.bodyLarge)
                val avg = t.distance / ((t.end - t.start).coerceAtLeast(1) / 1000.0)
                Text("${Fmt.dur(t.end - t.start)} · avg ${Fmt.speed(avg)}${if (t.confidence in 0.01..0.6) " · uncertain" else ""}",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            TextButton(onEdit) { Text("Change") }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ModeSheet(t: Trip, close: () -> Unit, pick: (String) -> Unit) {
    ModalBottomSheet(close) {
        Text("How did you travel?", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(horizontal = 24.dp))
        Text("Corrections are remembered and used in your insights.", style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 24.dp, vertical = 4.dp))
        Column(Modifier.padding(12.dp).navigationBarsPadding()) {
            modes.forEach { m ->
                ListItem(
                    headlineContent = { Text(modeLabel(m)) },
                    leadingContent = { ModeBadge(m) },
                    trailingContent = { if (m == t.mode) Icon(Icons.Rounded.Check, null, tint = MaterialTheme.colorScheme.primary) },
                    modifier = Modifier.clip(RoundedCornerShape(12.dp)).clickable { pick(m) },
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun VisitSheet(v: Visit, close: () -> Unit, hide: () -> Unit, run: (suspend () -> Unit) -> Unit) {
    var name by remember { mutableStateOf(v.name.orEmpty()) }
    ModalBottomSheet(close) {
        Column(Modifier.padding(horizontal = 24.dp).navigationBarsPadding().padding(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("Edit visit", style = MaterialTheme.typography.titleLarge)
            Text("${Fmt.dateTime(v.start)} · ${Fmt.dur(v.end - v.start)}", style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedTextField(name, { name = it }, label = { Text("Name") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            Button({ run { Repo.renameVisit(v, name.trim()) } }, Modifier.fillMaxWidth(), enabled = name.isNotBlank()) { Text("Rename") }
            OutlinedButton({
                run { Repo.savePlace(PlaceRow(name = name.trim().ifEmpty { "New place" }, lat = v.lat, lon = v.lon, radius = maxOf(50.0, v.radius))) }
            }, Modifier.fillMaxWidth()) {
                Icon(Icons.Rounded.BookmarkAdd, null); Spacer(Modifier.width(8.dp)); Text("Save as a place")
            }
            TextButton(hide, Modifier.fillMaxWidth(),
                colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)) {
                Icon(Icons.Rounded.VisibilityOff, null); Spacer(Modifier.width(8.dp)); Text("Hide this visit")
            }
        }
    }
}
