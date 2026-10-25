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
import android.hardware.Sensor
import android.hardware.SensorManager
import android.hardware.TriggerEvent
import android.hardware.TriggerEventListener
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.location.LocationRequest
import android.os.BatteryManager
import android.os.Build
import android.os.Looper
import android.os.SystemClock
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import androidx.core.location.LocationManagerCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

data class TrackState(
    val running: Boolean = false,
    val moving: Boolean = true,
    val last: Fix? = null,
    val paused: String? = null, // name of the pause zone we're in
    val accepted: Int = 0,
    val dropped: Int = 0,
    /** Running, but the phone's location switch is off so nothing can be recorded. */
    val locationOff: Boolean = false,
    /**
     * Tracking is on, but Android refused to start the service from the background (the process
     * was revived by a sync, not by a boot or an update). It resumes when the app opens.
     */
    val waiting: Boolean = false,
    /** Why the most recent dropped fix was dropped, for diagnosing odd tracks. */
    val lastDrop: Verdict? = null,
)

/**
 * Foreground GPS recorder. One location source only (the OS fused provider where there is one,
 * else GPS; network only on a phone with no GPS), because mixing sources is what makes a track
 * zig-zag when GPS degrades. The accept/reject rules live in [FixGate]. While stationary it slows
 * down to save battery and speeds up again as soon as the motion sensor or a fix says you moved.
 */
class TrackingService : Service(), LocationListener {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private lateinit var lm: LocationManager
    private val prefs get() = App.prefs

    private val gate = FixGate({ prefs.maxAccuracy.toDouble() }, { prefs.minDistance.toDouble() }, { prefs.stillInterval })
    private var lastFixAt = 0L // elapsedRealtime of the last delivery from the provider
    private var watchdog: Job? = null
    private var motionListener: TriggerEventListener? = null
    private var anchor: Fix? = null // where we've been sitting still since
    private var moving = true
    @Volatile private var places: List<PlaceRow> = emptyList() // written by a DB flow on IO, read on main

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
        // Reaching here without the permission means it was revoked while we were running. The
        // foreground call must still be honoured within seconds or Android kills the whole app.
        val allowed = ContextCompat.checkSelfPermission(this, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
        val started = runCatching {
            if (allowed) ServiceCompat.startForeground(this, 1, notification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
            else startForeground(1, notification())
        }.isSuccess
        if (!allowed) {
            prefs.tracking = false
            state.value = state.value.copy(waiting = false)
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
            return START_NOT_STICKY
        }
        if (!started) {
            // Android 12+ refuses a location foreground service started from the background (the
            // process was revived by a sync, not by boot/update). Recording is still wanted: keep
            // the setting and resume in MainActivity.onResume instead of silently turning it off.
            state.value = state.value.copy(waiting = true)
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
            return START_NOT_STICKY
        }
        state.value = state.value.copy(running = true, waiting = false)
        request()
        startWatchdog()
        notify(notification())
        return START_STICKY
    }

    /** The one provider to listen to: fused (Android 12+) > GPS > network (phones without GPS). */
    private fun pickProvider(): String? {
        val on = lm.getProviders(true)
        return when {
            Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && LocationManager.FUSED_PROVIDER in on -> LocationManager.FUSED_PROVIDER
            LocationManager.GPS_PROVIDER in on -> LocationManager.GPS_PROVIDER
            LocationManager.NETWORK_PROVIDER in on -> LocationManager.NETWORK_PROVIDER
            else -> null
        }
    }

    @Suppress("MissingPermission")
    private fun request() {
        lm.removeUpdates(this)
        val provider = pickProvider()
        val off = provider == null
        if (off != state.value.locationOff) state.value = state.value.copy(locationOff = off)
        armMotionWake()
        if (provider == null) return
        val ms = effectiveInterval() * 1000L
        // The distance filter is applied in FixGate, not by the provider: a provider-side filter
        // goes completely silent while you stand still, so the quiet-period heartbeat never ran.
        runCatching {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                // The plain overload runs at balanced power on Android 12+ and may never engage GNSS.
                val req = LocationRequest.Builder(ms).setQuality(LocationRequest.QUALITY_HIGH_ACCURACY).setMinUpdateDistanceMeters(0f).build()
                lm.requestLocationUpdates(provider, req, mainExecutor, this)
            } else {
                lm.requestLocationUpdates(provider, ms, 0f, this, Looper.getMainLooper())
            }
        }.onFailure {
            // The provider vanished or the permission went mid-call; the watchdog retries.
            state.value = state.value.copy(locationOff = true)
        }
        lastFixAt = SystemClock.elapsedRealtime()
    }

    /** While stationary, wake on the accelerometer's significant-motion trigger instead of waiting out a slow interval. */
    private fun armMotionWake() {
        val sm = getSystemService(SensorManager::class.java) ?: return
        val sensor = sm.getDefaultSensor(Sensor.TYPE_SIGNIFICANT_MOTION) ?: return
        motionListener?.let { sm.cancelTriggerSensor(it, sensor) }
        motionListener = null
        if (moving || !prefs.adaptive) return
        val l = object : TriggerEventListener() {
            override fun onTrigger(event: TriggerEvent?) {
                motionListener = null
                if (!moving) {
                    moving = true
                    anchor = null
                    state.value = state.value.copy(moving = true)
                    request()
                }
            }
        }
        if (sm.requestTriggerSensor(l, sensor)) motionListener = l
    }

    /**
     * Phones kill or silence location in the background more than they admit. If the provider has
     * gone quiet for much longer than the interval, ask again; if the location switch came back on, resume.
     */
    private fun startWatchdog() {
        watchdog?.cancel()
        watchdog = scope.launch {
            while (isActive) {
                delay(60_000)
                val quiet = SystemClock.elapsedRealtime() - lastFixAt
                if (quiet > 3L * effectiveInterval() * 1000 + 60_000 || state.value.locationOff) {
                    request()
                    notify(notification())
                }
            }
        }
    }

    override fun onLocationChanged(l: Location) {
        lastFixAt = SystemClock.elapsedRealtime()
        // Fixes can be delivered late (a queue after the screen was off). Judge their real age, and
        // stamp them with when they were taken, not with the possibly wrong provider clock.
        val ageMs = (SystemClock.elapsedRealtimeNanos() - l.elapsedRealtimeNanos) / 1_000_000
        if (ageMs > STALE_MS) return
        val now = System.currentTimeMillis() - maxOf(0L, ageMs)

        val fix = Fix(
            ts = now, lat = l.latitude, lon = l.longitude,
            acc = if (l.hasAccuracy()) l.accuracy.toDouble() else null,
            alt = if (l.hasAltitude()) l.altitude else null,
            speed = if (l.hasSpeed()) l.speed.toDouble() else null,
            bearing = if (l.hasBearing()) l.bearing.toDouble() else null,
            batt = getSystemService(BatteryManager::class.java).getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY).takeIf { it in 0..100 },
        )
        val verdict = gate.check(fix)
        if (verdict == Verdict.KEEP) {
            gate.commit(fix)
            val zone = places.firstOrNull { it.pause && distance(it.lat, it.lon, fix.lat, fix.lon) <= it.radius }
            state.value = state.value.copy(last = fix, paused = zone?.name, accepted = state.value.accepted + if (zone == null) 1 else 0)
            if (zone == null) scope.launch {
                runCatching {
                    App.db.points().insert(PointRow(fix.ts, fix.lat, fix.lon, fix.acc, fix.alt, fix.speed, fix.bearing, fix.batt))
                    if (prefs.connected && prefs.syncEvery == 0) Sync.now(this@TrackingService)
                }
            }
        } else {
            state.value = state.value.copy(dropped = state.value.dropped + 1, lastDrop = verdict)
        }
        if (verdict != Verdict.ACCURACY && verdict != Verdict.JUMP) adapt(fix)
        notify(notification())
    }

    /** Seconds between fixes right now: slower while stationary, when saving battery. */
    private fun effectiveInterval(): Int {
        if (moving || !prefs.adaptive) return prefs.interval
        // Without a motion sensor to wake us, a long stationary interval would hide the start of a trip.
        val canWake = motionListener != null
        return if (canWake) prefs.stillInterval else minOf(prefs.stillInterval, 30)
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
            s.locationOff -> "Location is turned off. Turn it on to keep recording."
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
            // The text names places and accuracy; the lock screen only shows that we record.
            .setVisibility(NotificationCompat.VISIBILITY_SECRET)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .build()
    }

    @android.annotation.SuppressLint("MissingPermission") // POST_NOTIFICATIONS may be denied; runCatching covers it
    private fun notify(n: Notification) {
        runCatching { androidx.core.app.NotificationManagerCompat.from(this).notify(1, n) }
    }

    override fun onDestroy() {
        lm.removeUpdates(this)
        getSystemService(SensorManager::class.java)?.let { sm ->
            val s = sm.getDefaultSensor(Sensor.TYPE_SIGNIFICANT_MOTION)
            motionListener?.let { if (s != null) sm.cancelTriggerSensor(it, s) }
        }
        scope.cancel()
        state.value = state.value.copy(running = false, locationOff = false)
        super.onDestroy()
    }

    // Re-evaluates which provider to use and refreshes the notification (location switched off/on).
    override fun onProviderEnabled(provider: String) { request(); notify(notification()) }
    override fun onProviderDisabled(provider: String) { request(); notify(notification()) }

    companion object {
        private const val ACTION_STOP = "stop"
        /** A fix older than this was queued by the OS and no longer describes where we are. */
        private const val STALE_MS = 5 * 60_000L
        val state = MutableStateFlow(TrackState())

        /** True when the phone's location switch is on (any provider). */
        fun locationOn(ctx: Context): Boolean =
            ctx.getSystemService(LocationManager::class.java)?.let { LocationManagerCompat.isLocationEnabled(it) } ?: false

        /** False (and nothing started) when location permission is missing. */
        fun start(ctx: Context): Boolean {
            if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.ACCESS_FINE_LOCATION) != PackageManager.PERMISSION_GRANTED) {
                App.prefs.tracking = false
                return false
            }
            App.prefs.tracking = true
            ContextCompat.startForegroundService(ctx, Intent(ctx, TrackingService::class.java))
            return true
        }

        fun stop(ctx: Context) {
            App.prefs.tracking = false
            state.value = state.value.copy(waiting = false)
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
