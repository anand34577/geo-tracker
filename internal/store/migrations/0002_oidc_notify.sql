-- OIDC: users are linked to their identity provider by the stable "sub" claim.
ALTER TABLE users ADD COLUMN oidc_sub TEXT;
CREATE UNIQUE INDEX users_oidc_sub ON users(oidc_sub) WHERE oidc_sub IS NOT NULL;

-- Per-user notification channels and event choices (JSON, see server/notify.go).
ALTER TABLE users ADD COLUMN notify TEXT NOT NULL DEFAULT '{}';
