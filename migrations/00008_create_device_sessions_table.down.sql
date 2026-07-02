-- Reverse the device_sessions table, RLS policies, and immutability
-- triggers (field-level guard and delete-protection).

-- Drop policies before disabling RLS to avoid leaving orphaned policies.
DROP POLICY IF EXISTS device_sessions_owner_all ON auth.device_sessions;
DROP POLICY IF EXISTS device_sessions_admin_all ON auth.device_sessions;
DROP POLICY IF EXISTS device_sessions_ro_select ON auth.device_sessions;
ALTER TABLE auth.device_sessions DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS protect_device_sessions_update_trigger ON auth.device_sessions;

DROP TRIGGER IF EXISTS prevent_device_sessions_delete ON auth.device_sessions;

DROP FUNCTION IF EXISTS protect_device_sessions_update();

DROP FUNCTION IF EXISTS prevent_sessions_delete();

DROP INDEX IF EXISTS idx_unique_active_device_id;

DROP INDEX IF EXISTS idx_device_sessions_user_id;

DROP INDEX IF EXISTS idx_device_sessions_device_id;

DROP INDEX IF EXISTS idx_device_sessions_last_activity;

DROP INDEX IF EXISTS idx_device_sessions_revoked_at;

DROP INDEX IF EXISTS idx_device_sessions_platform;

DROP TABLE IF EXISTS auth.device_sessions;
