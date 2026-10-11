package app.geotracker.ui

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.ChevronLeft
import androidx.compose.material.icons.rounded.ChevronRight
import androidx.compose.material.icons.rounded.Insights
import androidx.compose.material.icons.rounded.Place
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.geotracker.Insights
import app.geotracker.Repo
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.time.temporal.TemporalAdjusters

private enum class Period(val label: String) { Week("Week"), Month("Month"), Year("Year"), All("All time") }

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun InsightsScreen(openDay: (LocalDate) -> Unit) {
    var period by rememberSaveable { mutableStateOf(Period.Month) }
    var offset by rememberSaveable { mutableIntStateOf(0) } // 0 = current period, -1 = previous…
    var result by remember { mutableStateOf<Pair<Insights, Boolean>?>(null) }

    val today = LocalDate.now()
    val (from, to) = remember(period, offset) {
        when (period) {
            Period.Week -> today.with(TemporalAdjusters.previousOrSame(java.time.DayOfWeek.MONDAY)).plusWeeks(offset.toLong()).let { it to it.plusDays(6) }
            Period.Month -> today.withDayOfMonth(1).plusMonths(offset.toLong()).let { it to it.plusMonths(1).minusDays(1) }
            Period.Year -> today.withDayOfYear(1).plusYears(offset.toLong()).let { it to it.plusYears(1).minusDays(1) }
            Period.All -> LocalDate.of(1990, 1, 1) to today
        }
    }
    LaunchedEffect(from, to) { result = null; result = Repo.insights(from, minOf(to, today)) }

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).statusBarsPadding().padding(screenPadding)) {
        Text("Insights", style = MaterialTheme.typography.headlineMedium, modifier = Modifier.padding(vertical = 8.dp))
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
            Period.entries.forEachIndexed { i, p ->
                SegmentedButton(period == p, { period = p; offset = 0 }, SegmentedButtonDefaults.itemShape(i, Period.entries.size)) {
                    Text(p.label, maxLines = 1)
                }
            }
        }
        if (period != Period.All) Row(Modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            IconButton({ offset-- }) { Icon(Icons.Rounded.ChevronLeft, "Earlier") }
            Text(
                when (period) {
                    Period.Week -> "${from.format(DateTimeFormatter.ofPattern("d MMM"))} – ${to.format(DateTimeFormatter.ofPattern("d MMM yyyy"))}"
                    Period.Month -> from.format(DateTimeFormatter.ofPattern("MMMM yyyy"))
                    else -> from.year.toString()
                }, Modifier.weight(1f), style = MaterialTheme.typography.titleMedium, textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
            IconButton({ offset++ }, enabled = offset < 0) { Icon(Icons.Rounded.ChevronRight, "Later") }
        }

        val r = result
        if (r == null) {
            Box(Modifier.fillMaxWidth().height(300.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            return@Column
        }
        val (ins, server) = r
        if (!server) Note("Computed on this phone" + if (app.geotracker.App.prefs.connected) " · server unreachable" else "")
        if (ins.daysTracked == 0 && ins.visits == 0) {
            EmptyState(Icons.Rounded.Insights, "No data for this period", "Start recording or pick another period.")
            return@Column
        }

        Section {
            Row(Modifier.padding(16.dp)) {
                Stat("Distance", Fmt.dist(ins.distance), Modifier.weight(1f), MaterialTheme.colorScheme.primary)
                Stat("Moving", Fmt.dur(ins.movingMs), Modifier.weight(1f))
            }
            Row(Modifier.padding(start = 16.dp, end = 16.dp, bottom = 16.dp)) {
                Stat("Places", "${ins.places}", Modifier.weight(1f))
                Stat("Trips", "${ins.trips}", Modifier.weight(1f))
                Stat("Days", "${ins.daysTracked}", Modifier.weight(1f))
            }
        }

        if (ins.daily.isNotEmpty() && period != Period.All) Section("Distance per day", Modifier.padding(top = 12.dp)) {
            DailyBars(ins, from, to, openDay)
        }

        if (ins.modes.isNotEmpty()) Section("How you moved", Modifier.padding(top = 12.dp)) {
            val max = ins.modes.maxOf { it.distance }.coerceAtLeast(1.0)
            ins.modes.forEach { m ->
                Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                    ModeBadge(m.mode, 32)
                    Spacer(Modifier.width(12.dp))
                    Column(Modifier.weight(1f)) {
                        Row {
                            Text(modeLabel(m.mode), Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium)
                            Text(Fmt.dist(m.distance), style = MaterialTheme.typography.bodyMedium)
                        }
                        Spacer(Modifier.height(6.dp))
                        Box(Modifier.fillMaxWidth().height(6.dp).background(MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(3.dp))) {
                            Box(Modifier.fillMaxWidth((m.distance / max).toFloat()).fillMaxHeight().background(modeColor(m.mode), RoundedCornerShape(3.dp)))
                        }
                        Text("${m.trips} trips · ${Fmt.dur(m.ms)}", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }

        if (ins.topPlaces.isNotEmpty()) Section("Where you spent time", Modifier.padding(top = 12.dp)) {
            ins.topPlaces.take(8).forEachIndexed { i, p ->
                ListItem(
                    headlineContent = { Text(p.name, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    supportingContent = { Text("${p.visits} visits") },
                    leadingContent = {
                        Box(Modifier.size(36.dp).background(MaterialTheme.colorScheme.primaryContainer, RoundedCornerShape(10.dp)), contentAlignment = Alignment.Center) {
                            Text("${i + 1}", color = MaterialTheme.colorScheme.onPrimaryContainer, style = MaterialTheme.typography.labelLarge)
                        }
                    },
                    trailingContent = { Text(Fmt.dur(p.ms), style = MaterialTheme.typography.labelLarge) },
                    colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surface),
                )
            }
        }

        if (ins.countries.isNotEmpty() || ins.cities.isNotEmpty()) Section("Countries & cities", Modifier.padding(top = 12.dp)) {
            FlowRow(Modifier.padding(12.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                (ins.countries + ins.cities).forEach { a ->
                    AssistChip({}, { Text("${a.name} · ${a.visits}") }, leadingIcon = { Icon(Icons.Rounded.Place, null, Modifier.size(16.dp)) })
                }
            }
        }

        ins.longest?.let { t ->
            Section("Longest trip", Modifier.padding(top = 12.dp)) {
                ListItem(
                    headlineContent = { Text("${modeLabel(t.mode)} · ${Fmt.dist(t.distance)}") },
                    supportingContent = { Text("${Fmt.dateTime(t.start)} · ${Fmt.dur(t.end - t.start)}") },
                    leadingContent = { ModeBadge(t.mode) },
                    modifier = Modifier.clickable { openDay(java.time.Instant.ofEpochMilli(t.start).atZone(Repo.zone).toLocalDate()) },
                    colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surface),
                )
            }
        }
        Spacer(Modifier.height(24.dp))
    }
}

@Composable
private fun DailyBars(ins: Insights, from: LocalDate, to: LocalDate, openDay: (LocalDate) -> Unit) {
    val byDay = ins.daily.associateBy { it.day }
    val days = generateSequence(from) { it.plusDays(1) }.takeWhile { it <= to }.toList()
    val values = days.map { byDay[it.toString()]?.distance ?: 0.0 }
    val max = values.maxOrNull()?.coerceAtLeast(1.0) ?: 1.0
    val bar = MaterialTheme.colorScheme.primary
    val empty = MaterialTheme.colorScheme.surfaceVariant
    var picked by remember(from) { mutableStateOf<Int?>(null) }
    Column(Modifier.padding(16.dp)) {
        val i = picked
        Text(
            if (i != null) "${days[i].format(DateTimeFormatter.ofPattern("EEE d MMM"))} · ${Fmt.dist(values[i])}" else "Best day: ${Fmt.dist(max)}",
            style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(12.dp))
        Canvas(Modifier.fillMaxWidth().height(120.dp).pointerInput(days) {
            detectTapGestures { o ->
                val idx = (o.x / size.width * days.size).toInt().coerceIn(0, days.size - 1)
                if (picked == idx) openDay(days[idx]) else picked = idx
            }
        }) {
                val slot = size.width / days.size
                val bw = (slot * 0.7f).coerceAtLeast(1f)
                values.forEachIndexed { idx, v ->
                    val h = if (v > 0) (v / max * size.height).toFloat().coerceAtLeast(4f) else 3f
                    drawRoundRect(
                        if (v > 0) (if (idx == picked) bar.copy(alpha = 1f) else bar.copy(alpha = 0.75f)) else empty,
                    Offset(idx * slot + (slot - bw) / 2, size.height - h), Size(bw, h), CornerRadius(bw / 3),
                )
            }
        }
        Text("Tap a bar for details, tap again to open that day.", style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 8.dp))
    }
}
