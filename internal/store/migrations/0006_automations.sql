-- Geofence automations: when someone arrives at or leaves a saved place, run actions
-- (webhook, notify family, ntfy, Telegram, ...). Actions are a JSON list; the secrets inside
-- stay on the server and the API masks them.
CREATE TABLE automations (
  id            INTEGER PRIMARY KEY,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  place_id      INTEGER NOT NULL REFERENCES places(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  on_arrive     INTEGER NOT NULL DEFAULT 1,
  on_leave      INTEGER NOT NULL DEFAULT 0,
  enabled       INTEGER NOT NULL DEFAULT 1,
  cooldown_min  INTEGER NOT NULL DEFAULT 5,
  actions       TEXT NOT NULL DEFAULT '[]',
  last_fired_at INTEGER,
  last_event    TEXT NOT NULL DEFAULT '',
  last_result   TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL
);
CREATE INDEX automations_user ON automations(user_id);
CREATE INDEX automations_place ON automations(place_id);
