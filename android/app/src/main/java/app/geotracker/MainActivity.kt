package app.geotracker

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.BarChart
import androidx.compose.material.icons.outlined.Map
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Timeline
import androidx.compose.material.icons.rounded.BarChart
import androidx.compose.material.icons.rounded.Map
import androidx.compose.material.icons.rounded.Place
import androidx.compose.material.icons.rounded.Settings
import androidx.compose.material.icons.rounded.Timeline
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import app.geotracker.ui.GeoTheme
import app.geotracker.ui.InsightsScreen
import app.geotracker.ui.MapScreen
import app.geotracker.ui.PlacesScreen
import app.geotracker.ui.SettingsScreen
import app.geotracker.ui.TimelineScreen
import java.time.LocalDate

enum class Tab(val label: String, val icon: ImageVector, val selected: ImageVector) {
    Map("Map", Icons.Outlined.Map, Icons.Rounded.Map),
    Timeline("Timeline", Icons.Outlined.Timeline, Icons.Rounded.Timeline),
    Insights("Insights", Icons.Outlined.BarChart, Icons.Rounded.BarChart),
    Places("Places", Icons.Outlined.Place, Icons.Rounded.Place),
    Settings("Settings", Icons.Outlined.Settings, Icons.Rounded.Settings),
}

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (App.prefs.tracking && !TrackingService.state.value.running) runCatching { TrackingService.start(this) }
        setContent {
            val version by App.prefs.version.collectAsState()
            val system = isSystemInDarkTheme()
            val dark = when (App.prefs.theme.also { version }) { "light" -> false; "dark" -> true; else -> system }
            enableEdgeToEdge(
                statusBarStyle = if (dark) SystemBarStyle.dark(0) else SystemBarStyle.light(0, 0),
                navigationBarStyle = if (dark) SystemBarStyle.dark(0) else SystemBarStyle.light(0, 0),
            )
            var tab by rememberSaveable { mutableStateOf(Tab.Map) }
            var day by rememberSaveable { mutableStateOf(LocalDate.now().toString()) }
            BackHandler(tab != Tab.Map) { tab = Tab.Map }

            GeoTheme(dark) {
                Scaffold(
                    containerColor = MaterialTheme.colorScheme.background,
                    bottomBar = {
                        NavigationBar(containerColor = MaterialTheme.colorScheme.surface, tonalElevation = 0.dp) {
                            Tab.entries.forEach { t ->
                                NavigationBarItem(
                                    selected = tab == t, onClick = { tab = t },
                                    icon = { Icon(if (tab == t) t.selected else t.icon, null) },
                                    label = { Text(t.label) },
                                    colors = NavigationBarItemDefaults.colors(
                                        indicatorColor = MaterialTheme.colorScheme.primaryContainer,
                                        selectedIconColor = MaterialTheme.colorScheme.primary,
                                        selectedTextColor = MaterialTheme.colorScheme.primary,
                                    ),
                                )
                            }
                        }
                    },
                ) { pad ->
                    Box(Modifier.fillMaxSize().padding(bottom = pad.calculateBottomPadding())) {
                        AnimatedContent(tab, transitionSpec = { fadeIn() togetherWith fadeOut() }, label = "tab") { t ->
                            val openDay: (LocalDate) -> Unit = { day = it.toString(); tab = Tab.Timeline }
                            when (t) {
                                Tab.Map -> MapScreen(dark, openDay)
                                Tab.Timeline -> TimelineScreen(dark, LocalDate.parse(day)) { day = it.toString() }
                                Tab.Insights -> InsightsScreen(openDay)
                                Tab.Places -> PlacesScreen(dark)
                                Tab.Settings -> SettingsScreen()
                            }
                        }
                    }
                }
            }
        }
    }
}
