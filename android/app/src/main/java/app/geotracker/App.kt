package app.geotracker

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.SharedPreferences
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.setValue
import androidx.core.content.edit
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import kotlinx.coroutines.flow.MutableStateFlow
import org.maplibre.android.MapLibre
import org.maplibre.android.offline.OfflineManager

class App : Application() {
    override fun onCreate() {
        super.onCreate()
        app = this
        db = Db.open(this)
        prefs = Prefs(this)
        MapLibre.getInstance(this)
        // Every tile you look at is kept, so places you've been stay viewable offline.
        OfflineManager.getInstance(this).setMaximumAmbientCacheSize(300L * 1024 * 1024, null)
        getSystemService(NotificationManager::class.java).createNotificationChannel(
            NotificationChannel(CHANNEL, "Location tracking", NotificationManager.IMPORTANCE_LOW)
        )
        Sync.schedule(this)
        // The process can be revived (sync, boot, update) after the OS killed the recorder; bring it back.
        if (prefs.tracking && !TrackingService.state.value.running) runCatching { TrackingService.start(this) }
    }

    companion object {
        const val CHANNEL = "tracking"
        lateinit var app: App
        lateinit var db: Db
        lateinit var prefs: Prefs
    }
}

class Prefs(ctx: Context) {
    private val p = ctx.getSharedPreferences("settings", Context.MODE_PRIVATE)
    private val secret: SharedPreferences = openSecret(ctx)

    /** Bumped on every change so Compose screens can react to settings. */
    val version = MutableStateFlow(0)

    /**
     * Compose state mirror of [version]. Every getter reads it, so any composable that reads a
     * setting is recomposed when one changes, even a parameterless composable Compose would skip.
     */
    private var tick by mutableIntStateOf(0)

    private fun bump() {
        version.value++; tick++
    }

    private inline fun <T> read(block: () -> T): T {
        tick // subscribe the calling composition
        return block()
    }

    private fun put(block: SharedPreferences.Editor.() -> Unit) {
        p.edit(action = block); bump()
    }

    var server: String
        get() = read { p.getString("server", "")!! }
        set(v) = put { putString("server", v.trimEnd('/')) }
    var token: String
        get() = read { secret.getString("token", "")!! }
        set(v) { secret.edit { putString("token", v) }; bump() }
    var deviceId: Long
        get() = read { p.getLong("deviceId", 0) }
        set(v) = put { putLong("deviceId", v) }
    var userName: String
        get() = read { p.getString("userName", "")!! }
        set(v) = put { putString("userName", v) }
    val connected get() = read { server.isNotEmpty() && token.isNotEmpty() }

    var tracking: Boolean
        get() = read { p.getBoolean("tracking", false) }
        set(v) = put { putBoolean("tracking", v) }
    /** Seconds between fixes while moving. */
    var interval: Int
        get() = read { p.getInt("interval", 10) }
        set(v) = put { putInt("interval", v) }
    /** Minimum meters between recorded fixes (0 = time only). */
    var minDistance: Int
        get() = read { p.getInt("minDistance", 0) }
        set(v) = put { putInt("minDistance", v) }
    /** Fixes less accurate than this are dropped. */
    var maxAccuracy: Int
        get() = read { p.getInt("maxAccuracy", 50) }
        set(v) = put { putInt("maxAccuracy", v) }
    /** Slow down to [stillInterval] while stationary to save battery. */
    var adaptive: Boolean
        get() = read { p.getBoolean("adaptive", true) }
        set(v) = put { putBoolean("adaptive", v) }
    var stillInterval: Int
        get() = read { p.getInt("stillInterval", 120) }
        set(v) = put { putInt("stillInterval", v) }

    /** Minutes between uploads; 0 = upload every fix as it arrives. */
    var syncEvery: Int
        get() = read { p.getInt("syncEvery", 15) }
        set(v) = put { putInt("syncEvery", v) }
    var wifiOnly: Boolean
        get() = read { p.getBoolean("wifiOnly", false) }
        set(v) = put { putBoolean("wifiOnly", v) }
    var lastSync: Long
        get() = read { p.getLong("lastSync", 0) }
        set(v) = put { putLong("lastSync", v) }
    var syncError: String
        get() = read { p.getString("syncError", "")!! }
        set(v) = put { putString("syncError", v) }
    /** Server points are mirrored locally up to here. */
    var pulledTo: Long
        get() = read { p.getLong("pulledTo", 0) }
        set(v) = put { putLong("pulledTo", v) }

    /** auto | light | dark */
    var theme: String
        get() = read { p.getString("theme", "auto")!! }
        set(v) = put { putString("theme", v) }
    var units: String // metric | imperial
        get() = read { p.getString("units", "metric")!! }
        set(v) = put { putString("units", v) }

    /** Whose history the points table holds (server + email), so another account never inherits it. */
    var dataOwner: String
        get() = read { p.getString("dataOwner", "")!! }
        set(v) = put { putString("dataOwner", v) }

    /** Public http server hosts the user explicitly allowed; LAN addresses never need approval. */
    var insecureHosts: Set<String>
        get() = read { p.getStringSet("insecureHosts", emptySet())!! }
        set(v) = put { putStringSet("insecureHosts", v) }

    fun signOut() {
        secret.edit { clear() }
        put { remove("deviceId"); remove("userName"); remove("pulledTo"); remove("lastSync"); remove("syncError") }
    }

    private companion object {
        /**
         * The Keystore key can be lost (restore to another phone, OS update, lock-screen reset);
         * then decrypting throws and the app would crash on every launch. Start over instead:
         * the user only has to sign in again, recorded points are in the database.
         */
        fun openSecret(ctx: Context): SharedPreferences {
            fun create() = EncryptedSharedPreferences.create(
                ctx, "secret", MasterKey.Builder(ctx).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build(),
                EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
            )
            return try {
                create().also { it.getString("token", "") }
            } catch (e: Exception) {
                ctx.deleteSharedPreferences("secret")
                create()
            }
        }
    }
}
