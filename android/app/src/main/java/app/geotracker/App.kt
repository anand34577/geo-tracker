package app.geotracker

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.SharedPreferences
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
    private val secret: SharedPreferences = EncryptedSharedPreferences.create(
        ctx, "secret", MasterKey.Builder(ctx).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build(),
        EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
        EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
    )

    /** Bumped on every change so Compose screens can react to settings. */
    val version = MutableStateFlow(0)

    private fun put(block: SharedPreferences.Editor.() -> Unit) {
        p.edit(action = block); version.value++
    }

    var server: String
        get() = p.getString("server", "")!!
        set(v) = put { putString("server", v.trimEnd('/')) }
    var token: String
        get() = secret.getString("token", "")!!
        set(v) { secret.edit { putString("token", v) }; version.value++ }
    var deviceId: Long
        get() = p.getLong("deviceId", 0)
        set(v) = put { putLong("deviceId", v) }
    var userName: String
        get() = p.getString("userName", "")!!
        set(v) = put { putString("userName", v) }
    val connected get() = server.isNotEmpty() && token.isNotEmpty()

    var tracking: Boolean
        get() = p.getBoolean("tracking", false)
        set(v) = put { putBoolean("tracking", v) }
    /** Seconds between fixes while moving. */
    var interval: Int
        get() = p.getInt("interval", 10)
        set(v) = put { putInt("interval", v) }
    /** Minimum meters between recorded fixes (0 = time only). */
    var minDistance: Int
        get() = p.getInt("minDistance", 0)
        set(v) = put { putInt("minDistance", v) }
    /** Fixes less accurate than this are dropped. */
    var maxAccuracy: Int
        get() = p.getInt("maxAccuracy", 50)
        set(v) = put { putInt("maxAccuracy", v) }
    /** Slow down to [stillInterval] while stationary to save battery. */
    var adaptive: Boolean
        get() = p.getBoolean("adaptive", true)
        set(v) = put { putBoolean("adaptive", v) }
    var stillInterval: Int
        get() = p.getInt("stillInterval", 120)
        set(v) = put { putInt("stillInterval", v) }

    /** Minutes between uploads; 0 = upload every fix as it arrives. */
    var syncEvery: Int
        get() = p.getInt("syncEvery", 15)
        set(v) = put { putInt("syncEvery", v) }
    var wifiOnly: Boolean
        get() = p.getBoolean("wifiOnly", false)
        set(v) = put { putBoolean("wifiOnly", v) }
    var lastSync: Long
        get() = p.getLong("lastSync", 0)
        set(v) = put { putLong("lastSync", v) }
    var syncError: String
        get() = p.getString("syncError", "")!!
        set(v) = put { putString("syncError", v) }
    /** Server points are mirrored locally up to here. */
    var pulledTo: Long
        get() = p.getLong("pulledTo", 0)
        set(v) = put { putLong("pulledTo", v) }

    /** auto | light | dark */
    var theme: String
        get() = p.getString("theme", "auto")!!
        set(v) = put { putString("theme", v) }
    var units: String // metric | imperial
        get() = p.getString("units", "metric")!!
        set(v) = put { putString("units", v) }

    fun signOut() {
        secret.edit { clear() }
        put { remove("deviceId"); remove("userName"); remove("pulledTo"); remove("lastSync"); remove("syncError") }
    }
}
