-- Firebase Cloud Messaging registration tokens. One row per
-- (user_id, device_id) tracking a device's current FCM token, platform,
-- and activity. The uq_fcm_tokens_user_device constraint keeps the table
-- compact: re-registering a device updates the existing row instead
-- of accumulating stale tokens.

DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_type t JOIN pg_namespace n ON t.typnamespace = n.oid
        WHERE t.typname = 'notification_platform' AND n.nspname = 'auth'
    ) THEN
        CREATE TYPE auth.notification_platform AS ENUM('android', 'ios', 'web');
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS
auth.fcm_tokens (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
  device_id VARCHAR(255) NOT NULL,
  token TEXT NOT NULL,
  platform auth.notification_platform NOT NULL,
  app_version VARCHAR(50),
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  last_used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT uq_fcm_tokens_user_device UNIQUE (user_id, device_id)
);

-- Fan-out path: the push worker fetches every active token for a user.
CREATE INDEX IF NOT EXISTS idx_fcm_tokens_user_id ON auth.fcm_tokens (user_id);

-- Device-level lookups (logout from a single device, debug tooling).
CREATE INDEX IF NOT EXISTS idx_fcm_tokens_device_id ON auth.fcm_tokens (device_id);

-- Hot path partial index: the push worker only iterates active tokens.
CREATE INDEX IF NOT EXISTS idx_fcm_tokens_user_active ON auth.fcm_tokens (user_id)
WHERE
is_active = TRUE;

-- Platform-segmented analytics: count active tokens per platform.
CREATE INDEX IF NOT EXISTS idx_fcm_tokens_platform ON auth.fcm_tokens (platform);

-- Pruning: rank tokens by recency to expire stale ones.
CREATE INDEX IF NOT EXISTS idx_fcm_tokens_last_used ON auth.fcm_tokens (last_used_at DESC);

DROP TRIGGER IF EXISTS update_fcm_tokens_updated_at ON auth.fcm_tokens;

CREATE TRIGGER update_fcm_tokens_updated_at BEFORE
UPDATE ON auth.fcm_tokens FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS prevent_fcm_tokens_created_at_update ON auth.fcm_tokens;

CREATE TRIGGER prevent_fcm_tokens_created_at_update BEFORE
UPDATE ON auth.fcm_tokens FOR EACH ROW
EXECUTE FUNCTION prevent_update_created_at();

-- Throttle: bump last_used_at at most once per hour to avoid hammering
-- the row on every notification fan-out. The WHEN clause on the trigger
-- skips inactive tokens entirely.
CREATE OR REPLACE FUNCTION update_fcm_token_last_used() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.is_active = TRUE AND (OLD.last_used_at IS NULL OR OLD.last_used_at < NOW() - INTERVAL '1 hour') THEN
        NEW.last_used_at = NOW();
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_update_fcm_token_last_used ON auth.fcm_tokens;

CREATE TRIGGER trigger_update_fcm_token_last_used BEFORE
UPDATE ON auth.fcm_tokens FOR EACH ROW WHEN (NEW.is_active = TRUE)
EXECUTE FUNCTION update_fcm_token_last_used();

COMMENT ON TABLE auth.fcm_tokens IS
'Firebase Cloud Messaging registration tokens for push delivery. One row per (user_id, device_id). Updated by the mobile clients on app launch and by the push worker on successful delivery.';

COMMENT ON COLUMN auth.fcm_tokens.device_id IS 'Stable per-device identifier reported by the mobile client. Combined with user_id forms the unique key.';

COMMENT ON COLUMN auth.fcm_tokens.token IS 'Current FCM registration token. Rotates whenever the platform invalidates it.';

COMMENT ON COLUMN auth.fcm_tokens.platform IS 'Originating platform: android, ios, or web.';

COMMENT ON COLUMN auth.fcm_tokens.app_version IS 'Client app version reported at registration time; used for diagnostics and feature gating.';

COMMENT ON COLUMN auth.fcm_tokens.is_active IS 'Set to FALSE when FCM reports the token as invalid; rows remain for forensics rather than being deleted.';

COMMENT ON COLUMN auth.fcm_tokens.last_used_at IS 'Timestamp of the most recent successful delivery to this token. Bumped at most once per hour by trigger.';

COMMENT ON TYPE auth.notification_platform IS 'Client platform that registered the FCM token.';

COMMENT ON FUNCTION update_fcm_token_last_used() IS 'BEFORE UPDATE trigger on auth.fcm_tokens. Stamps last_used_at when an active token is touched, throttled to one bump per hour so the row is not rewritten on every notification fan-out.';

-- Row-level security. FCM tokens are device-bound and registered per
-- user; app_api reads / writes only its own rows. The
-- push-notification worker connects as app_worker and needs full
-- access for fan-out and token invalidation.
ALTER TABLE auth.fcm_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth.fcm_tokens FORCE ROW LEVEL SECURITY;

CREATE POLICY fcm_tokens_owner_all ON auth.fcm_tokens
FOR ALL TO app_api
USING (TRUE)
WITH CHECK (TRUE);

CREATE POLICY fcm_tokens_admin_all ON auth.fcm_tokens
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY fcm_tokens_ro_select ON auth.fcm_tokens
FOR SELECT TO app_readonly USING (TRUE);

COMMENT ON POLICY fcm_tokens_owner_all ON auth.fcm_tokens IS
'A user registers and reads only their own device tokens. The push-notification worker uses app_worker to fan out.';

COMMENT ON POLICY fcm_tokens_admin_all ON auth.fcm_tokens IS
'app_worker (push worker + backoffice) reads, writes, and invalidates tokens across all users.';

COMMENT ON POLICY fcm_tokens_ro_select ON auth.fcm_tokens IS
'app_readonly reads tokens for delivery-rate dashboards and per-platform analytics.';
