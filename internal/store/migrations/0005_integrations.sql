-- Per-user connections to other self-hosted apps (Immich photos). Secrets live here,
-- not in prefs, because prefs are sent to the browser.
ALTER TABLE users ADD COLUMN integrations TEXT NOT NULL DEFAULT '{}';
