-- Exports are built in the background and downloaded when ready, so a big export never
-- ties up a browser request. The file lives in DATA_DIR/exports and expires after a week.
CREATE TABLE exports (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  format      TEXT NOT NULL,
  from_ts     INTEGER,
  to_ts       INTEGER,
  status      TEXT NOT NULL,            -- queued|running|done|failed
  size        INTEGER NOT NULL DEFAULT 0,
  error       TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  finished_at INTEGER
);
CREATE INDEX exports_user ON exports(user_id);
