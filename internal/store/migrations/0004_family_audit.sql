-- Family groups: people who share locations with each other (spec §13).
CREATE TABLE groups (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL
);

-- Membership carries the member's own sharing choices for this group (consent-first:
-- invited members share nothing until they accept and choose).
CREATE TABLE group_members (
  group_id        INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role            TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner','member')),
  status          TEXT NOT NULL DEFAULT 'invited' CHECK (status IN ('invited','active')),
  share_live      INTEGER NOT NULL DEFAULT 0,
  share_history_d INTEGER NOT NULL DEFAULT 0,   -- days of history visible: 0 none, -1 all
  precision       TEXT NOT NULL DEFAULT 'exact' CHECK (precision IN ('exact','approx')),
  paused_until    INTEGER,                      -- ghost mode
  color           TEXT NOT NULL DEFAULT '',
  invited_by      INTEGER REFERENCES users(id) ON DELETE SET NULL,
  joined_at       INTEGER,
  created_at      INTEGER NOT NULL,
  PRIMARY KEY (group_id, user_id)
);
CREATE INDEX group_members_user ON group_members(user_id);

-- "Tell me when Sam arrives at / leaves School" (the place belongs to the watcher).
CREATE TABLE alert_rules (
  id         INTEGER PRIMARY KEY,
  watcher_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  subject_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  place_id   INTEGER NOT NULL REFERENCES places(id) ON DELETE CASCADE,
  on_arrive  INTEGER NOT NULL DEFAULT 1,
  on_leave   INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX alert_rules_subject ON alert_rules(subject_id);

-- User corrections of detected transport modes, matched to trips by start time at read time
-- (trips are recomputed, so they can't carry the correction themselves).
CREATE TABLE trip_modes (
  user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  start_ts INTEGER NOT NULL,
  mode     TEXT NOT NULL,
  PRIMARY KEY (user_id, start_ts)
);

CREATE TABLE audit_log (
  id       INTEGER PRIMARY KEY,
  ts       INTEGER NOT NULL,
  actor_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  action   TEXT NOT NULL,
  target   TEXT NOT NULL DEFAULT '',
  ip       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_ts ON audit_log(ts);

-- Raw points older than this many days are deleted automatically (0 = keep forever).
ALTER TABLE users ADD COLUMN retention_days INTEGER NOT NULL DEFAULT 0;
