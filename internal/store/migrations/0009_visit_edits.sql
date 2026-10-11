-- User corrections of detected visits: rename, pin to the right saved place, delete (hide),
-- or merge with the following visits. Visits are recomputed, so like trip_modes these are
-- matched to visits by start time at read time.
CREATE TABLE visit_edits (
  user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  start_ts INTEGER NOT NULL,
  name     TEXT NOT NULL DEFAULT '',                         -- '' = keep the detected name
  place_id INTEGER REFERENCES places(id) ON DELETE SET NULL, -- "it was actually here"
  no_place INTEGER NOT NULL DEFAULT 0,                       -- "not any of my saved places"
  hidden   INTEGER NOT NULL DEFAULT 0,                       -- deleted from the timeline
  merge_to INTEGER NOT NULL DEFAULT 0,                       -- absorbs visits/trips up to this time
  PRIMARY KEY (user_id, start_ts)
);

-- Device health reads one device's fixes over a few weeks.
CREATE INDEX points_device_ts ON points(device_id, ts) WHERE device_id IS NOT NULL;
