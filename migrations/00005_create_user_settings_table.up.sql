-- One-to-one extension of auth.users that holds all mutable user
-- preferences as a single JSONB blob, so the preference schema can
-- evolve without migrations whenever a new toggle is added.
CREATE TABLE IF NOT EXISTS
auth.user_settings (
  user_id BIGINT PRIMARY KEY REFERENCES auth.users (id) ON DELETE CASCADE,
  settings JSONB NOT NULL DEFAULT jsonb_build_object(
    'phone_visibility',
    'masked',
    'auto_accept_bookings',
    FALSE,
    'contact_methods',
    jsonb_build_object('sms', TRUE, 'call', TRUE, 'inapp', TRUE),
    'trip_auto_archive_policy',
    'after_completed_days',
    'trip_archive_days_completed',
    30,
    'trip_archive_days_canceled',
    30,
    'preferred_language',
    'en',
    'push_notification_settings',
    jsonb_build_object(
      'trip_updates',
      TRUE,
      'messages',
      TRUE,
      'promotions',
      FALSE,
      'emergency',
      TRUE
    ),
    'timezone',
    'UTC',
    'distance_unit',
    'metric',
    'data_sharing_with_third_parties',
    'analytics_only',
    'email_trip_recap',
    TRUE
  ),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- GIN on the JSONB blob powers `settings @> '{"key":...}'` queries
-- (e.g. fan-out by preferred_language, push opt-in audiences).
CREATE INDEX IF NOT EXISTS idx_user_settings_settings_gin ON auth.user_settings USING GIN (settings);

-- Lifecycle triggers: updated_at maintenance and created_at
-- immutability.
DROP TRIGGER IF EXISTS update_user_settings_updated_at ON auth.user_settings;

CREATE TRIGGER update_user_settings_updated_at BEFORE
UPDATE ON auth.user_settings FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS prevent_user_settings_created_at_update ON auth.user_settings;

CREATE TRIGGER prevent_user_settings_created_at_update BEFORE
UPDATE ON auth.user_settings FOR EACH ROW
EXECUTE FUNCTION prevent_update_created_at();

COMMENT ON TABLE auth.user_settings IS 'One-to-one extension of auth.users holding all mutable user preferences. Stored as JSONB so the schema can evolve without migrations for new preference keys.';

COMMENT ON COLUMN auth.user_settings.user_id IS 'Primary key and FK to auth.users. CASCADE delete removes settings when the user is deleted.';

COMMENT ON COLUMN auth.user_settings.settings IS 'JSONB preference bag. Stable keys: phone_visibility (masked|full|hidden), auto_accept_bookings (bool), contact_methods ({sms,call,inapp}: bool), trip_auto_archive_policy (enum), trip_archive_days_completed (int), trip_archive_days_canceled (int), preferred_language (ISO 639-1 code), push_notification_settings ({trip_updates,messages,promotions,emergency}: bool), timezone (IANA name), distance_unit (metric|imperial), data_sharing_with_third_parties (enum), email_trip_recap (bool). The GIN index on this column enables efficient jsonb operator queries (e.g. @> for filtering by a specific preference value).';

-- Row-level security. user_settings is a 1:1 child of auth.users
-- keyed by user_id (also the primary key). The owner predicate
-- mirrors the parent table: the authenticated user reads and
-- mutates only their own settings row.
ALTER TABLE auth.user_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth.user_settings FORCE ROW LEVEL SECURITY;

CREATE POLICY user_settings_owner_all ON auth.user_settings
FOR ALL TO app_api
USING (TRUE)
WITH CHECK (TRUE);

CREATE POLICY user_settings_admin_all ON auth.user_settings
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY user_settings_ro_select ON auth.user_settings
FOR SELECT TO app_readonly USING (TRUE);

COMMENT ON POLICY user_settings_owner_all ON auth.user_settings IS
'Settings row is owned 1:1 by user_id. Owner reads and writes; nobody else touches it.';

COMMENT ON POLICY user_settings_admin_all ON auth.user_settings IS
'app_worker has full access for backoffice settings management and bulk operations.';

COMMENT ON POLICY user_settings_ro_select ON auth.user_settings IS
'app_readonly reads settings for analytics and reporting dashboards.';
