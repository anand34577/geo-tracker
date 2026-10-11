package app.geotracker.ui

import android.annotation.SuppressLint
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import app.geotracker.Api
import app.geotracker.App
import app.geotracker.Export
import app.geotracker.Share
import app.geotracker.Sync
import app.geotracker.TrackingService
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@Composable
fun SettingsScreen() {
    val prefs = App.prefs
    val version by prefs.version.collectAsState()
    version.let { } // recompose on any settings change
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).statusBarsPadding().padding(screenPadding)) {
        Text("Settings", style = MaterialTheme.typography.headlineMedium, modifier = Modifier.padding(vertical = 8.dp))
        if (prefs.connected) Account() else Connect()
        Recording()
        if (prefs.connected) SyncSection()
        if (prefs.connected) Sharing()
        MapSection()
        Data()
        val ctx = LocalContext.current
        Text("GeoTracker for Android · " + ctx.packageManager.getPackageInfo(ctx.packageName, 0).versionName,style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(vertical = 24.dp))
    }
}

@Composable
private fun Connect() {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var server by rememberSaveable { mutableStateOf("https://") }
    var email by rememberSaveable { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val insecure = server.startsWith("http://") && !isPrivateHost(server)

    Section("Server") {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text("Works fully on this phone. Connect your GeoTracker server to back up, see imports and named places, and share with family.",
                style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedTextField(server, { server = it.trim() }, Modifier.fillMaxWidth(), label = { Text("Server address") }, singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri), shape = RoundedCornerShape(14.dp))
            if (insecure) Text("This address isn't encrypted. Use https:// for a server on the internet.", color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodySmall)
            OutlinedTextField(email, { email = it.trim() }, Modifier.fillMaxWidth(), label = { Text("Email") }, singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email), shape = RoundedCornerShape(14.dp))
            OutlinedTextField(password, { password = it }, Modifier.fillMaxWidth(), label = { Text("Password") }, singleLine = true,
                visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                shape = RoundedCornerShape(14.dp))
            error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
            Button({
                busy = true; error = null
                scope.launch {
                    val url = server.trimEnd('/')
                    runCatching {
                        withContext(Dispatchers.IO) { Api.login(url, email, password, "${Build.MANUFACTURER} ${Build.MODEL}".trim().take(60)) }
                    }.onSuccess { r ->
                        App.prefs.server = url
                        App.prefs.token = r.getString("token")
                        App.prefs.deviceId = r.getJSONObject("device").getLong("id")
                        App.prefs.userName = r.getJSONObject("user").optString("name")
                        Sync.schedule(ctx); Sync.now(ctx)
                    }.onFailure { error = it.message ?: "Could not reach the server" }
                    busy = false
                }
            }, Modifier.fillMaxWidth(), enabled = !busy && server.length > 8 && email.isNotBlank() && password.isNotBlank()) {
                if (busy) CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp) else Text("Sign in")
            }
        }
    }
}

private fun isPrivateHost(url: String): Boolean {
    val host = Uri.parse(url).host ?: return false
    return host == "localhost" || host.endsWith(".local") || host.endsWith(".lan") || host.startsWith("10.") ||
        host.startsWith("192.168.") || Regex("^172\\.(1[6-9]|2\\d|3[01])\\.").containsMatchIn(host) || host.startsWith("100.")
}

@Composable
private fun Account() {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val prefs = App.prefs
    var confirm by remember { mutableStateOf(false) }
    Section("Server") {
        ListItem(
            headlineContent = { Text(prefs.userName.ifEmpty { "Signed in" }) },
            supportingContent = { Text(prefs.server) },
            leadingContent = { Icon(Icons.Rounded.AccountCircle, null, Modifier.size(40.dp), tint = MaterialTheme.colorScheme.primary) },
        )
        Row2("Last upload", Fmt.ago(prefs.lastSync))
        if (prefs.syncError.isNotEmpty()) Text(prefs.syncError, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.padding(horizontal = 16.dp))
        Row(Modifier.padding(12.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilledTonalButton({ Sync.now(ctx) }) { Icon(Icons.Rounded.Sync, null, Modifier.size(18.dp)); Spacer(Modifier.width(6.dp)); Text("Sync now") }
            OutlinedButton({ ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(prefs.server))) }) { Text("Open web") }
            TextButton({ confirm = true }) { Text("Sign out") }
        }
    }
    if (confirm) AlertDialog(
        onDismissRequest = { confirm = false },
        title = { Text("Sign out?") },
        text = { Text("This phone stops uploading and its access token is revoked. Everything recorded stays on this phone and on your server.") },
        confirmButton = {
            TextButton({
                confirm = false
                scope.launch {
                    withContext(Dispatchers.IO) { Api.signOut() }
                    App.db.places().clearRemote()
                    App.prefs.signOut()
                    Sync.schedule(ctx)
                }
            }) { Text("Sign out") }
        },
        dismissButton = { TextButton({ confirm = false }) { Text("Cancel") } },
    )
}

@SuppressLint("BatteryLife")
@Composable
private fun Recording() {
    val ctx = LocalContext.current
    val prefs = App.prefs
    fun changed() = TrackingService.restartIfRunning(ctx)
    Section("Recording", Modifier.padding(top = 12.dp)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            SliderSetting("Fix every", "${prefs.interval} s", prefs.interval, 1..120) { prefs.interval = it; changed() }
            SliderSetting("Minimum movement", if (prefs.minDistance == 0) "off" else "${prefs.minDistance} m", prefs.minDistance, 0..200) { prefs.minDistance = it; changed() }
            SliderSetting("Accuracy limit", "±${prefs.maxAccuracy} m", prefs.maxAccuracy, 10..200) { prefs.maxAccuracy = it; changed() }
            Toggle("Save battery when still", "Slows down while you stay in one spot, speeds up as soon as you move.", prefs.adaptive) { prefs.adaptive = it; changed() }
            if (prefs.adaptive) SliderSetting("Fix every, when still", "${prefs.stillInterval} s", prefs.stillInterval, 30..600) { prefs.stillInterval = it; changed() }
        }
        val pm = ctx.getSystemService(PowerManager::class.java)
        if (!pm.isIgnoringBatteryOptimizations(ctx.packageName)) Link(Icons.Rounded.BatteryAlert, "Allow running in the background",
            "Some phones stop recording to save battery. Exempt GeoTracker.") {
            ctx.startActivity(Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:${ctx.packageName}")))
        }
    }
}

@Composable
private fun SyncSection() {
    val ctx = LocalContext.current
    val prefs = App.prefs
    Section("Upload", Modifier.padding(top = 12.dp)) {
        Column(Modifier.padding(16.dp)) {
            Text("Send to the server", style = MaterialTheme.typography.bodyLarge)
            Spacer(Modifier.height(8.dp))
            Choice(listOf(0 to "Instantly", 15 to "15 min", 60 to "Hourly", 360 to "6 h"), prefs.syncEvery) { prefs.syncEvery = it; Sync.schedule(ctx) }
            Spacer(Modifier.height(8.dp))
            Toggle("Only on Wi-Fi", "Queued fixes wait for Wi-Fi; nothing is lost.", prefs.wifiOnly) { prefs.wifiOnly = it; Sync.schedule(ctx) }
        }
    }
}

@Composable
private fun Sharing() {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var shares by remember { mutableStateOf<List<Share>>(emptyList()) }
    var hours by remember { mutableIntStateOf(1) }
    var approx by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    LaunchedEffect(reload) { shares = withContext(Dispatchers.IO) { runCatching { Api.shares() }.getOrDefault(emptyList()) } }

    Section("Share your location", Modifier.padding(top = 12.dp)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("A private link that shows where you are, until it expires.", style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant)
            Choice(listOf(1 to "1 hour", 8 to "8 hours", 24 to "1 day", 168 to "1 week"), hours) { hours = it }
            Toggle("Approximate", "About 1 km; no exact address.", approx) { approx = it }
            error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            Button({
                scope.launch {
                    runCatching { withContext(Dispatchers.IO) { Api.createShare("Live location", hours, approx) } }
                        .onSuccess { url ->
                            reload++
                            ctx.startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, url), "Share link"))
                        }.onFailure { error = it.message }
                }
            }, Modifier.fillMaxWidth()) { Icon(Icons.Rounded.Share, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp)); Text("Create link") }
        }
        shares.filter { it.expiresAt > System.currentTimeMillis() }.forEach { s ->
            ListItem(
                headlineContent = { Text(s.name) },
                supportingContent = { Text("${s.kind} · ${s.precision} · ${s.views} views · until ${Fmt.dateTime(s.expiresAt)}") },
                trailingContent = {
                    IconButton({ scope.launch { withContext(Dispatchers.IO) { runCatching { Api.deleteShare(s.id) } }; reload++ } }) {
                        Icon(Icons.Rounded.LinkOff, "Revoke link")
                    }
                },
            )
        }
    }
}

@Composable
private fun MapSection() {
    val ctx = LocalContext.current
    val prefs = App.prefs
    var regions by remember { mutableStateOf<List<Region>>(emptyList()) }
    var reload by remember { mutableIntStateOf(0) }
    LaunchedEffect(reload) { Offline.list(ctx) { regions = it } }
    Section("Appearance & maps", Modifier.padding(top = 12.dp)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text("Theme", style = MaterialTheme.typography.bodyLarge)
            Choice(listOf("auto" to "System", "light" to "Light", "dark" to "Dark"), prefs.theme) { prefs.theme = it }
            Text("Units", style = MaterialTheme.typography.bodyLarge)
            Choice(listOf("metric" to "Kilometers", "imperial" to "Miles"), prefs.units) { prefs.units = it }
        }
        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        Text("Offline areas", style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(start = 16.dp, top = 12.dp))
        Text("Save an area from the map's download button. Places you've looked at are also kept automatically.",
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp))
        regions.forEach { r ->
            ListItem(
                headlineContent = { Text(r.name) },
                supportingContent = { Text(if (r.styleDark) "Dark map" else "Light map") },
                leadingContent = { Icon(Icons.Rounded.Map, null) },
                trailingContent = { IconButton({ r.region.delete(object : org.maplibre.android.offline.OfflineRegion.OfflineRegionDeleteCallback {
                    override fun onDelete() { reload++ }
                    override fun onError(error: String) {}
                }) }) { Icon(Icons.Rounded.Delete, "Delete area") } },
            )
        }
        if (regions.isEmpty()) Spacer(Modifier.height(12.dp))
    }
}

@Composable
private fun Data() {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val count by App.db.points().count().collectAsState(0)
    val queued by App.db.points().unsyncedCount().collectAsState(0)
    var format by remember { mutableStateOf("gpx") }
    var message by remember { mutableStateOf<String?>(null) }
    var confirm by remember { mutableStateOf(false) }
    val export = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("application/octet-stream")) { uri ->
        uri ?: return@rememberLauncherForActivityResult
        scope.launch {
            message = runCatching {
                withContext(Dispatchers.IO) { ctx.contentResolver.openOutputStream(uri)!!.use { Export.write(format, it) } }
            }.fold({ "Exported %,d points".format(it) }, { "Export failed: ${it.message}" })
        }
    }
    Section("Your data", Modifier.padding(top = 12.dp)) {
        Row2("Points on this phone", "%,d".format(count))
        if (App.prefs.connected) Row2("Waiting to upload", "%,d".format(queued))
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Export", style = MaterialTheme.typography.bodyLarge)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf("gpx" to "GPX", "geojson" to "GeoJSON", "csv" to "CSV").forEach { (f, label) ->
                    OutlinedButton({ format = f; export.launch("geotracker-${java.time.LocalDate.now()}.$f") }) { Text(label) }
                }
            }
            message?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
            TextButton({ confirm = true }, colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)) {
                Icon(Icons.Rounded.DeleteForever, null); Spacer(Modifier.width(8.dp)); Text("Delete everything on this phone")
            }
        }
    }
    if (confirm) AlertDialog(
        onDismissRequest = { confirm = false },
        title = { Text("Delete all points on this phone?") },
        text = {
            Text(if (queued > 0 && App.prefs.connected) "%,d points have not been uploaded yet and will be lost for good.".format(queued)
            else "Your server keeps its copy. This can't be undone on the phone.")
        },
        confirmButton = {
            TextButton({
                confirm = false
                scope.launch { withContext(Dispatchers.IO) { App.db.points().delete(0, Long.MAX_VALUE); App.prefs.pulledTo = 0 } }
            }, colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error)) { Text("Delete") }
        },
        dismissButton = { TextButton({ confirm = false }) { Text("Cancel") } },
    )
}

@Composable
private fun SliderSetting(title: String, value: String, current: Int, range: IntRange, set: (Int) -> Unit) {
    var v by remember(current) { mutableFloatStateOf(current.toFloat()) }
    Row {
        Text(title, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Text(value, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.primary)
    }
    Slider(v, { v = it }, valueRange = range.first.toFloat()..range.last.toFloat(), onValueChangeFinished = { set(v.toInt()) })
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun <T> Choice(options: List<Pair<T, String>>, selected: T, pick: (T) -> Unit) {
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        options.forEachIndexed { i, (v, label) ->
            SegmentedButton(selected == v, { pick(v) }, SegmentedButtonDefaults.itemShape(i, options.size)) { Text(label, maxLines = 1) }
        }
    }
}

@Composable
private fun Link(icon: ImageVector, title: String, body: String, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(title) }, supportingContent = { Text(body) },
        leadingContent = { Icon(icon, null, tint = MaterialTheme.colorScheme.secondary) },
        modifier = Modifier.clickable(onClick = onClick),
    )
}
