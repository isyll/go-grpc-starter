-- Reverses 000012_create_user_settings_table.up.sql by dropping
-- RLS policies, lifecycle triggers, the GIN index, and the table.

-- Drop policies before disabling RLS.
DROP POLICY IF EXISTS user_settings_owner_all ON auth.user_settings;
DROP POLICY IF EXISTS user_settings_admin_all ON auth.user_settings;
DROP POLICY IF EXISTS user_settings_ro_select ON auth.user_settings;
ALTER TABLE auth.user_settings DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS update_user_settings_updated_at ON auth.user_settings;

DROP TRIGGER IF EXISTS prevent_user_settings_created_at_update ON auth.user_settings;

DROP INDEX IF EXISTS idx_user_settings_settings_gin;

DROP TABLE IF EXISTS auth.user_settings;
