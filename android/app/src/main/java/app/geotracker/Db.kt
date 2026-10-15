package app.geotracker

import android.content.Context
import androidx.room.Dao
import androidx.room.Database
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.room.Transaction
import androidx.room.Upsert
import kotlinx.coroutines.flow.Flow

@Entity(tableName = "points")
data class PointRow(
    @PrimaryKey val ts: Long,
    val lat: Double, val lon: Double,
    val acc: Double? = null, val alt: Double? = null, val speed: Double? = null,
    val bearing: Double? = null, val batt: Int? = null,
    val synced: Boolean = false,
) {
    fun fix() = Fix(ts, lat, lon, acc, alt, speed, bearing, batt)
}

/** A saved place. remoteId is set for places that live on the server. */
@Entity(tableName = "places")
data class PlaceRow(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    val remoteId: Long? = null,
    val name: String,
    val lat: Double, val lon: Double,
    val radius: Double = 75.0,
    val private: Boolean = false, // server privacy zone: hidden from family and shares
    val pause: Boolean = false,   // device-only: don't record while inside (Colota-style pause zone)
    val visits: Int = 0,
    val totalMs: Long = 0,
)

/** Corrections made on the device when there is no server: visit names and trip modes, keyed by start. */
@Entity(tableName = "edits", primaryKeys = ["start", "kind"])
data class EditRow(val start: Long, val kind: String, val value: String)

@Dao
interface PointDao {
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insert(p: PointRow): Long

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insertAll(p: List<PointRow>)

    @Query("SELECT * FROM points WHERE synced = 0 ORDER BY ts LIMIT :limit")
    suspend fun unsynced(limit: Int): List<PointRow>

    @Query("UPDATE points SET synced = 1 WHERE ts IN (:ts)")
    suspend fun markSynced(ts: List<Long>)

    @Query("SELECT COUNT(*) FROM points WHERE synced = 0")
    fun unsyncedCount(): Flow<Int>

    @Query("SELECT COUNT(*) FROM points")
    fun count(): Flow<Int>

    @Query("SELECT * FROM points ORDER BY ts DESC LIMIT 1")
    fun latest(): Flow<PointRow?>

    @Query("SELECT * FROM points WHERE ts >= :from AND ts < :to ORDER BY ts")
    suspend fun range(from: Long, to: Long): List<PointRow>

    @Query("SELECT * FROM points WHERE ts > :after AND ts < :to ORDER BY ts LIMIT :limit")
    suspend fun page(after: Long, to: Long, limit: Int): List<PointRow>

    @Query("SELECT MIN(ts) FROM points")
    suspend fun first(): Long?

    /** 15-minute buckets that have data (≤ ~3k a month), for calendar dots. */
    @Query("SELECT DISTINCT ts / 900000 FROM points WHERE ts >= :from AND ts < :to")
    suspend fun quarterHours(from: Long, to: Long): List<Long>

    @Query("SELECT ts, batt FROM points WHERE ts >= :from AND batt IS NOT NULL ORDER BY ts")
    suspend fun battery(from: Long): List<BattSample>

    @Query("DELETE FROM points WHERE ts >= :from AND ts < :to")
    suspend fun delete(from: Long, to: Long): Int
}

data class BattSample(val ts: Long, val batt: Int)

@Dao
interface PlaceDao {
    @Query("SELECT * FROM places ORDER BY name COLLATE NOCASE")
    fun all(): Flow<List<PlaceRow>>

    @Query("SELECT * FROM places")
    suspend fun list(): List<PlaceRow>

    @Upsert
    suspend fun upsert(p: PlaceRow): Long

    @Query("DELETE FROM places WHERE id = :id")
    suspend fun delete(id: Long)

    @Query("DELETE FROM places WHERE remoteId IS NOT NULL")
    suspend fun clearRemote()

    @Transaction
    suspend fun replaceRemote(places: List<PlaceRow>) {
        clearRemote()
        places.forEach { upsert(it) }
    }
}

@Dao
interface EditDao {
    @Query("SELECT * FROM edits")
    suspend fun all(): List<EditRow>

    @Upsert
    suspend fun put(e: EditRow)

    @Query("DELETE FROM edits WHERE start = :start AND kind = :kind")
    suspend fun delete(start: Long, kind: String)
}

@Database(entities = [PointRow::class, PlaceRow::class, EditRow::class], version = 1, exportSchema = true)
abstract class Db : RoomDatabase() {
    abstract fun points(): PointDao
    abstract fun places(): PlaceDao
    abstract fun edits(): EditDao

    companion object {
        fun open(ctx: Context) = Room.databaseBuilder(ctx, Db::class.java, "geotracker.db").build()
    }
}
