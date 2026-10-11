package app.geotracker

import android.Manifest
import android.app.Notification
import android.app.PendingIntent
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.BatteryManager
import android.os.Looper
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch

data class TrackState(
    val running: Boolean = false,
    val moving: Boolean = true,
    val last: Fix? = null,
    val paused: String? = null, // name of the pause zone we're in
    val accepted: Int = 0,
    val dropped: Int = 0,
)

/**
 * Foreground GPS recorder. Precision rules, in order: drop fixes worse than the accuracy limit,
 * drop impossible jumps (> 350 m/s, same rule as the server), prefer GPS over network fixes,
 * then thin by distance. While stationary it slows down to save battery and speeds up again
 * as soon as you move.
 */
class TrackingService : Service(), LocationListener {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private lateinit var lm: LocationManager
    private val prefs get() = App.prefs

    private var lastSaved: Fix? = null
    private var lastGpsAt = 0L
    private var spikes = 0
    private var anchor: Fix? = null // where we've been sitting still since
    private var moving = true
    private var places: List<PlaceRow> = emptyList()

    override fun onBind(intent: Intent?) = null

    override fun onCreate() {
        super.onCreate()
        lm = getSystemService(LocationManager::class.java)
        scope.launch { App.db.places().all().collect { places = it } }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stop(this); return START_NOT_STICKY
        }
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.ACCESS_FINE_LOCATION) != PackageManager.PERMISSION_GRANTED) {
            stopSelf(); return START_NOT_STICKY
        }
        ServiceCompat.startForeground(this, 1, notification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
        state.value = state.value.copy(running = true)
        request()
        return START_STICKY
    }

    @Suppress("MissingPermission")
    private fun request() {
        lm.removeUpdates(this)
        val secs = if (moving || !prefs.adaptive) prefs.interval else prefs.stillInterval
        val ms = secs * 1000L
        val providers = lm.getProviders(true)
        if (LocationManager.GPS_PROVIDER in providers)
            lm.requestLocationUpdates(LocationManager.GPS_PROVIDER, ms, prefs.minDistance.toFloat(), this, Looper.getMainLooper())
        // Network fixes keep stays alive indoors where GPS can't see the sky.
        if (LocationManager.NETWORK_PROVIDER in providers)
            lm.requestLocationUpdates(LocationManager.NETWORK_PROVIDER, maxOf(ms, 30_000L), 0f, this, Looper.getMainLooper())
    }

    override fun onLocationChanged(l: Location) {
        val now = l.time
        val isGps = l.provider == LocationManager.GPS_PROVIDER
        if (isGps) lastGpsAt = now
        else if (now - lastGpsAt < 3L * prefs.interval * 1000) return // GPS is working; ignore coarser fixes

        val fix = Fix(
            ts = now, lat = l.latitude, lon = l.longitude,
            acc = if (l.hasAccuracy()) l.accuracy.toDouble() else null,
            alt = if (l.hasAltitude()) l.altitude else null,
            speed = if (l.hasSpeed()) l.speed.toDouble() else null,
            bearing = if (l.hasBearing()) l.bearing.toDouble() else null,
            batt = getSystemService(BatteryManager::class.java).getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY).takeIf { it in 0..100 },
        )
        if (accept(fix)) {
            lastSaved = fix
            val zone = places.firstOrNull { it.pause && distance(it.lat, it.lon, fix.lat, fix.lon) <= it.radius }
            state.value = state.value.copy(last = fix, paused = zone?.name, accepted = state.value.accepted + if (zone == null) 1 else 0)
            if (zone == null) scope.launch {
                App.db.points().insert(PointRow(fix.ts, fix.lat, fix.lon, fix.acc, fix.alt, fix.speed, fix.bearing, fix.batt))
                if (prefs.connected && prefs.syncEvery == 0) Sync.now(this@TrackingService)
            }
        } else {
            state.value = state.value.copy(dropped = state.value.dropped + 1)
        }
        if (fix.acc == null || fix.acc <= prefs.maxAccuracy) adapt(fix)
        notify(notification())
    }

    private fun accept(f: Fix): Boolean {
        if (f.acc != null && f.acc > prefs.maxAccuracy) return false
        val prev = lastSaved ?: return true
        val dt = (f.ts - prev.ts) / 1000.0
        if (dt <= 0) return false
        val d = distance(prev.lat, prev.lon, f.lat, f.lon)
        if (d / dt > TimelineBuilder.MAX_SPEED && spikes < 3) {
            spikes++; return false
        }
        spikes = 0
        // Distance filter, but never stay silent longer than the stationary interval.
        return d >= prefs.minDistance || dt >= prefs.stillInterval
    }

    /** Stationary = within 30 m (or the fix accuracy) of one spot for 3 minutes without speed. */
    private fun adapt(f: Fix) {
        if (!prefs.adaptive) return
        val a = anchor
        val radius = maxOf(30.0, f.acc ?: 0.0)
        val wasMoving = moving
        if (a == null || distance(a.lat, a.lon, f.lat, f.lon) > radius || (f.speed ?: 0.0) > 2.0) {
            anchor = f
            moving = true
        } else if (f.ts - a.ts >= 3 * 60_000) {
            moving = false
        }
        if (moving != wasMoving) {
            state.value = state.value.copy(moving = moving)
            request()
        }
    }

    private fun notification(): Notification {
        val s = state.value
        val text = when {
            s.paused != null -> "Paused in ${s.paused}"
            s.last == null -> "Waiting for GPS…"
            else -> "±${s.last.acc?.toInt() ?: "?"} m · ${if (s.moving) "moving" else "stationary"} · ${s.accepted} fixes"
        }
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val stop = PendingIntent.getService(this, 1, Intent(this, TrackingService::class.java).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE)
        return NotificationCompat.Builder(this, App.CHANNEL)
            .setSmallIcon(R.drawable.ic_stat)
            .setContentTitle("Recording your timeline")
            .setContentText(text)
            .setContentIntent(open)
            .addAction(0, "Stop", stop)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .build()
    }

    @android.annotation.SuppressLint("MissingPermission") // POST_NOTIFICATIONS may be denied; runCatching covers it
    private fun notify(n: Notification) {
        runCatching { androidx.core.app.NotificationManagerCompat.from(this).notify(1, n) }
    }

    override fun onDestroy() {
        lm.removeUpdates(this)
        scope.cancel()
        state.value = state.value.copy(running = false)
        super.onDestroy()
    }

    override fun onProviderEnabled(provider: String) = request()
    override fun onProviderDisabled(provider: String) {}

    companion object {
        private const val ACTION_STOP = "stop"
        val state = MutableStateFlow(TrackState())

        fun start(ctx: Context) {
            App.prefs.tracking = true
            ContextCompat.startForegroundService(ctx, Intent(ctx, TrackingService::class.java))
        }

        fun stop(ctx: Context) {
            App.prefs.tracking = false
            ctx.stopService(Intent(ctx, TrackingService::class.java))
            if (App.prefs.connected) Sync.now(ctx) // flush what's queued
        }

        /** Re-applies changed settings to a running service. */
        fun restartIfRunning(ctx: Context) {
            if (state.value.running) ContextCompat.startForegroundService(ctx, Intent(ctx, TrackingService::class.java))
        }
    }
}

class BootReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val ok = intent.action == Intent.ACTION_BOOT_COMPLETED || intent.action == Intent.ACTION_MY_PACKAGE_REPLACED
        if (ok && App.prefs.tracking) runCatching { TrackingService.start(ctx) }
    }
}
