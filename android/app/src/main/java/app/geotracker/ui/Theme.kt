package app.geotracker.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.DirectionsWalk
import androidx.compose.material.icons.rounded.DirectionsBike
import androidx.compose.material.icons.rounded.DirectionsCar
import androidx.compose.material.icons.rounded.Flight
import androidx.compose.material.icons.rounded.HelpOutline
import androidx.compose.material.icons.rounded.Train
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.geotracker.App
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

// Same palette as the web app (web/src/index.css): warm stone neutrals, teal accent.
private val Light = lightColorScheme(
    primary = Color(0xFF0D9488), onPrimary = Color.White,
    primaryContainer = Color(0xFFCCFBF1), onPrimaryContainer = Color(0xFF042F2E),
    secondary = Color(0xFFF97316), onSecondary = Color.White,
    secondaryContainer = Color(0xFFCCFBF1), onSecondaryContainer = Color(0xFF042F2E),
    background = Color(0xFFFAFAF9), onBackground = Color(0xFF1C1917),
    surface = Color(0xFFFFFFFF), onSurface = Color(0xFF1C1917),
    surfaceVariant = Color(0xFFF5F5F4), onSurfaceVariant = Color(0xFF57534E),
    surfaceContainer = Color(0xFFF5F5F4), surfaceContainerHigh = Color(0xFFEEEDEC),
    surfaceContainerLow = Color(0xFFFAFAF9), surfaceContainerHighest = Color(0xFFE7E5E4),
    outline = Color(0xFFD6D3D1), outlineVariant = Color(0xFFE7E5E4),
    error = Color(0xFFB91C1C),
)

private val Dark = darkColorScheme(
    primary = Color(0xFF2DD4BF), onPrimary = Color(0xFF042F2E),
    primaryContainer = Color(0xFF0D2A27), onPrimaryContainer = Color(0xFF99F6E4),
    secondary = Color(0xFFFB923C), onSecondary = Color(0xFF2A1004),
    secondaryContainer = Color(0xFF0D2A27), onSecondaryContainer = Color(0xFF99F6E4),
    background = Color(0xFF0C0A09), onBackground = Color(0xFFFAFAF9),
    surface = Color(0xFF171412), onSurface = Color(0xFFFAFAF9),
    surfaceVariant = Color(0xFF211E1C), onSurfaceVariant = Color(0xFFB8B2AD),
    surfaceContainer = Color(0xFF1C1917), surfaceContainerHigh = Color(0xFF211E1C),
    surfaceContainerLow = Color(0xFF151210), surfaceContainerHighest = Color(0xFF2E2A27),
    outline = Color(0xFF3D3835), outlineVariant = Color(0xFF2A2624),
    error = Color(0xFFF87171),
)

private val Type = Typography().run {
    copy(
        displaySmall = displaySmall.copy(fontWeight = FontWeight.SemiBold, letterSpacing = (-0.5).sp),
        headlineMedium = headlineMedium.copy(fontWeight = FontWeight.SemiBold, letterSpacing = (-0.5).sp),
        headlineSmall = headlineSmall.copy(fontWeight = FontWeight.SemiBold, letterSpacing = (-0.25).sp),
        titleLarge = titleLarge.copy(fontWeight = FontWeight.SemiBold),
        titleMedium = titleMedium.copy(fontWeight = FontWeight.SemiBold),
        labelLarge = labelLarge.copy(fontWeight = FontWeight.SemiBold),
    )
}

@Composable
fun isDark(): Boolean = when (App.prefs.theme) {
    "light" -> false
    "dark" -> true
    else -> isSystemInDarkTheme()
}

@Composable
fun GeoTheme(dark: Boolean, content: @Composable () -> Unit) =
    MaterialTheme(colorScheme = if (dark) Dark else Light, typography = Type, content = content)

// ── Formatting ───────────────────────────────────────────────

object Fmt {
    private val imperial get() = App.prefs.units == "imperial"
    private val time = DateTimeFormatter.ofLocalizedTime(FormatStyle.SHORT)
    private val dateTime = DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT)

    fun dist(m: Double): String = if (imperial) {
        val mi = m / 1609.344
        if (mi < 0.1) "${(m * 3.281).toInt()} ft" else if (mi < 10) "%.1f mi".format(mi) else "%,d mi".format(mi.toLong())
    } else {
        if (m < 1000) "${m.toInt()} m" else if (m < 10_000) "%.1f km".format(m / 1000) else "%,d km".format((m / 1000).toLong())
    }

    fun speed(ms: Double): String = if (imperial) "%.0f mph".format(ms * 2.23694) else "%.0f km/h".format(ms * 3.6)

    fun dur(ms: Long): String {
        val d = Duration.ofMillis(ms.coerceAtLeast(0))
        return when {
            d.toDays() >= 2 -> "${d.toDays()} d"
            d.toHours() >= 1 -> "${d.toHours()} h ${d.toMinutes() % 60} min" // toMinutesPart() is API 31+
            d.toMinutes() >= 1 -> "${d.toMinutes()} min"
            else -> "${d.seconds} s"
        }
    }

    fun clock(ts: Long): String = time.format(Instant.ofEpochMilli(ts).atZone(ZoneId.systemDefault()))
    fun dateTime(ts: Long): String = dateTime.format(Instant.ofEpochMilli(ts).atZone(ZoneId.systemDefault()))

    fun ago(ts: Long): String {
        val s = (System.currentTimeMillis() - ts) / 1000
        return when {
            ts <= 0 -> "never"
            s < 60 -> "just now"
            s < 3600 -> "${s / 60} min ago"
            s < 86_400 -> "${s / 3600} h ago"
            else -> "${s / 86_400} d ago"
        }
    }
}

val modes = listOf("walk", "cycle", "drive", "train", "flight", "unknown")

fun modeIcon(m: String): ImageVector = when (m) {
    "walk" -> Icons.AutoMirrored.Rounded.DirectionsWalk
    "cycle" -> Icons.Rounded.DirectionsBike
    "drive" -> Icons.Rounded.DirectionsCar
    "train" -> Icons.Rounded.Train
    "flight" -> Icons.Rounded.Flight
    else -> Icons.Rounded.HelpOutline
}

fun modeLabel(m: String) = when (m) {
    "walk" -> "Walking"; "cycle" -> "Cycling"; "drive" -> "Driving"
    "train" -> "Train"; "flight" -> "Flight"; else -> "Moving"
}

fun modeColor(m: String) = when (m) {
    "walk" -> Color(0xFF16A34A); "cycle" -> Color(0xFF0891B2); "drive" -> Color(0xFF2563EB)
    "train" -> Color(0xFF9333EA); "flight" -> Color(0xFFDB2777); else -> Color(0xFF78716C)
}

// ── Building blocks ──────────────────────────────────────────

@Composable
fun Section(title: String? = null, modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Column(modifier.fillMaxWidth()) {
        if (title != null) Text(
            title.uppercase(), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(start = 4.dp, bottom = 8.dp, top = 8.dp), letterSpacing = 1.sp,
        )
        Card(
            shape = RoundedCornerShape(20.dp),
            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
            border = androidx.compose.foundation.BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
        ) { Column(Modifier.padding(vertical = 4.dp), content = content) }
    }
}

@Composable
fun Stat(label: String, value: String, modifier: Modifier = Modifier, accent: Color = MaterialTheme.colorScheme.onSurface) {
    Column(modifier) {
        Text(value, style = MaterialTheme.typography.titleLarge, color = accent, maxLines = 1)
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
    }
}

@Composable
fun ModeBadge(mode: String, size: Int = 36) {
    Box(
        Modifier.size(size.dp).background(modeColor(mode).copy(alpha = 0.15f), CircleShape),
        contentAlignment = Alignment.Center,
    ) { Icon(modeIcon(mode), null, tint = modeColor(mode), modifier = Modifier.size((size * 0.55).dp)) }
}

@Composable
fun Dot(color: Color, size: Int = 8) = Box(Modifier.size(size.dp).background(color, CircleShape))

@Composable
fun EmptyState(icon: ImageVector, title: String, body: String, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth().padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Box(Modifier.size(64.dp).background(MaterialTheme.colorScheme.primaryContainer, CircleShape), contentAlignment = Alignment.Center) {
            Icon(icon, null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(32.dp))
        }
        Spacer(Modifier.size(4.dp))
        Text(title, style = MaterialTheme.typography.titleMedium)
        Text(body, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center)
    }
}

@Composable
fun Row2(label: String, value: String) = Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
    Text(label, Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    Spacer(Modifier.width(12.dp))
    Text(value, style = MaterialTheme.typography.bodyMedium)
}

val screenPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp)
val mono = TextStyle(fontFeatureSettings = "tnum")
