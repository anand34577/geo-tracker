package app.geotracker

import android.content.Context
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import java.util.concurrent.TimeUnit

/**
 * Uploads queued fixes to /ingest/json, then mirrors the server's history (imports, other
 * phones) into the local database so every screen also works offline. Both directions are
 * idempotent: the server ignores duplicate timestamps and so does the local table.
 */
class SyncWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {
    override suspend fun doWork(): Result {
        val prefs = App.prefs
        if (!prefs.connected) return Result.success()
        return try {
            Sync.run()
            prefs.syncError = ""
            Result.success()
        } catch (e: ApiError) {
            val revoked = e.status == 401 || e.status == 403
            prefs.syncError = if (revoked) "The server no longer accepts this phone. Sign out and sign in again." else e.message ?: "Sync failed"
            if (revoked) Result.failure() else Result.retry()
        } catch (e: Exception) {
            prefs.syncError = e.message ?: "Server unreachable"
            Result.retry()
        }
    }
}

object Sync {
    private const val WEEK = 7 * 86_400_000L

    suspend fun run() {
        val dao = App.db.points()
        while (true) {
            val batch = dao.unsynced(500) // markSynced binds one variable per row; SQLite before Android 11 allows 999
            if (batch.isEmpty()) break
            Api.upload(batch)
            dao.markSynced(batch.map { it.ts })
        }
        App.prefs.lastSync = System.currentTimeMillis()

        val now = System.currentTimeMillis()
        // "Upload every fix" runs this per fix; mirroring the server's history can wait.
        if (now - App.prefs.pulledTo < 10 * 60_000L) return
        // Overlap a few hours: other phones may upload late.
        var from = App.prefs.pulledTo.takeIf { it > 0 }?.minus(6 * 3_600_000L) ?: (Api.firstPoint() ?: now)
        var span = WEEK
        while (from < now) {
            val to = minOf(from + span, now)
            val (pts, step) = Api.points(from, to)
            if (step > 1 && span > 3_600_000L) { // the server thinned it: fetch a shorter window instead
                span /= 4; continue
            }
            dao.insertAll(pts)
            App.prefs.pulledTo = to
            from = to
            span = WEEK
        }
        Repo.refreshPlaces()
    }

    private fun constraints() = Constraints.Builder()
        .setRequiredNetworkType(if (App.prefs.wifiOnly) NetworkType.UNMETERED else NetworkType.CONNECTED)
        .build()

    fun schedule(ctx: Context) {
        val wm = WorkManager.getInstance(ctx)
        if (!App.prefs.connected) {
            wm.cancelUniqueWork("periodic"); return
        }
        val every = maxOf(15, App.prefs.syncEvery).toLong()
        wm.enqueueUniquePeriodicWork(
            "periodic", ExistingPeriodicWorkPolicy.UPDATE,
            PeriodicWorkRequestBuilder<SyncWorker>(every, TimeUnit.MINUTES).setConstraints(constraints()).build(),
        )
    }

    fun now(ctx: Context) {
        WorkManager.getInstance(ctx).enqueueUniqueWork(
            // APPEND_OR_REPLACE: a request made while a run is in flight must run after it. KEEP dropped
            // it, so fixes that arrived mid-upload waited for the next periodic run.
            "now", ExistingWorkPolicy.APPEND_OR_REPLACE,
            OneTimeWorkRequestBuilder<SyncWorker>()
                .setConstraints(constraints())
                .build(),
        )
    }
}
