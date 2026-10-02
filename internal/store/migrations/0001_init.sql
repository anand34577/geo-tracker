-- All timestamps are UTC unix milliseconds; coordinates are WGS84 degrees.

CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE COLLATE NOCASE,
  name          TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin','user')),
  prefs         TEXT NOT NULL DEFAULT '{}',
  disabled_at   INTEGER,
  timeline_dirty_from INTEGER,        -- earliest ts whose derived timeline is stale
  created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
  id_hash    BLOB PRIMARY KEY,         -- SHA-256 of the cookie value
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL,
  user_agent TEXT,
  ip         TEXT,
  created_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE devices (
  id           INTEGER PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  client       TEXT NOT NULL,
  last_seen_at INTEGER,
  last_battery INTEGER,
  created_at   INTEGER NOT NULL
);

CREATE TABLE tokens (
  id           INTEGER PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id    INTEGER REFERENCES devices(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  hash         BLOB NOT NULL UNIQUE,   -- SHA-256; plaintext is shown once
  scopes       TEXT NOT NULL,
  last_used_at INTEGER,
  created_at   INTEGER NOT NULL
);

CREATE TABLE imports (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  filename    TEXT NOT NULL,
  format      TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL,            -- queued|running|done|failed
  added       INTEGER NOT NULL DEFAULT 0,
  duplicates  INTEGER NOT NULL DEFAULT 0,
  rejected    INTEGER NOT NULL DEFAULT 0,
  error       TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  finished_at INTEGER
);

-- Raw data: never modified by processing (ADR-009).
CREATE TABLE points (
  id        INTEGER PRIMARY KEY,
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id INTEGER REFERENCES devices(id) ON DELETE SET NULL,
  import_id INTEGER REFERENCES imports(id) ON DELETE CASCADE,
  ts        INTEGER NOT NULL,
  lat       REAL NOT NULL,
  lon       REAL NOT NULL,
  accuracy  REAL,
  altitude  REAL,
  speed     REAL,
  bearing   REAL,
  battery   INTEGER,
  UNIQUE (user_id, ts)                  -- dedupe; also serves every time-range query
);
CREATE INDEX points_import ON points(import_id) WHERE import_id IS NOT NULL;

CREATE TABLE places (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  icon       TEXT NOT NULL DEFAULT 'map-pin',
  lat        REAL NOT NULL,
  lon        REAL NOT NULL,
  radius_m   REAL NOT NULL DEFAULT 75,
  created_at INTEGER NOT NULL
);
CREATE INDEX places_user ON places(user_id);

CREATE TABLE geocode_cache (
  id           INTEGER PRIMARY KEY,
  lat          REAL NOT NULL,
  lon          REAL NOT NULL,
  provider     TEXT NOT NULL,
  name         TEXT NOT NULL DEFAULT '',
  address      TEXT NOT NULL DEFAULT '',
  city         TEXT NOT NULL DEFAULT '',
  country_code TEXT NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL
);
CREATE INDEX geocode_lat ON geocode_cache(lat);

-- Derived data: recomputed from points at any time.
CREATE TABLE visits (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  start_ts   INTEGER NOT NULL,
  end_ts     INTEGER NOT NULL,
  lat        REAL NOT NULL,
  lon        REAL NOT NULL,
  radius_m   REAL NOT NULL,
  points     INTEGER NOT NULL,
  geocode_id INTEGER REFERENCES geocode_cache(id) ON DELETE SET NULL
);
CREATE INDEX visits_user_time ON visits(user_id, start_ts);
CREATE INDEX visits_ungeocoded ON visits(start_ts) WHERE geocode_id IS NULL;

CREATE TABLE trips (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  start_ts   INTEGER NOT NULL,
  end_ts     INTEGER NOT NULL,
  distance_m REAL NOT NULL,
  mode       TEXT NOT NULL,
  confidence REAL NOT NULL
);
CREATE INDEX trips_user_time ON trips(user_id, start_ts);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
