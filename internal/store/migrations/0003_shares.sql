-- Public, time-limited links to a live location or a recorded time range.
CREATE TABLE share_links (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash BLOB NOT NULL UNIQUE,     -- SHA-256; the link itself is shown once
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL CHECK (kind IN ('live','range')),
  from_ts    INTEGER,                  -- range links only
  to_ts      INTEGER,
  precision  TEXT NOT NULL DEFAULT 'exact' CHECK (precision IN ('exact','approx')),
  expires_at INTEGER NOT NULL,
  views      INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX share_links_user ON share_links(user_id);
