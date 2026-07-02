-- Reverses 000044_create_fcm_tokens_table.up.sql. Drops policies
-- before disabling RLS, then triggers and functions, then indexes,
-- then the table, and finally the enum type.

DROP POLICY IF EXISTS fcm_tokens_owner_all ON auth.fcm_tokens;
DROP POLICY IF EXISTS fcm_tokens_admin_all ON auth.fcm_tokens;
DROP POLICY IF EXISTS fcm_tokens_ro_select ON auth.fcm_tokens;
ALTER TABLE auth.fcm_tokens DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS trigger_update_fcm_token_last_used ON auth.fcm_tokens;

DROP TRIGGER IF EXISTS prevent_fcm_tokens_created_at_update ON auth.fcm_tokens;

DROP TRIGGER IF EXISTS update_fcm_tokens_updated_at ON auth.fcm_tokens;

DROP FUNCTION IF EXISTS update_fcm_token_last_used();

DROP INDEX IF EXISTS idx_fcm_tokens_last_used;

DROP INDEX IF EXISTS idx_fcm_tokens_platform;

DROP INDEX IF EXISTS idx_fcm_tokens_user_active;

DROP INDEX IF EXISTS idx_fcm_tokens_device_id;

DROP INDEX IF EXISTS idx_fcm_tokens_user_id;

DROP TABLE IF EXISTS auth.fcm_tokens;

DROP TYPE IF EXISTS auth.notification_platform;
