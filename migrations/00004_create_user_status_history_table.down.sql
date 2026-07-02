-- Reverses 000009_create_user_status_history_table.up.sql by dropping
-- RLS policies, the anti-physical-delete trigger and its function,
-- the lifecycle trigger, indexes, and the audit-trail table.

-- Drop the status-change auto-population trigger (on auth.users) and
-- its function first.
DROP TRIGGER IF EXISTS trg_log_user_status_change ON auth.users;
DROP FUNCTION IF EXISTS log_user_status_change();

-- Drop policies before disabling RLS.
DROP POLICY IF EXISTS user_status_history_definer_insert ON auth.user_status_history;
DROP POLICY IF EXISTS user_status_history_self_read ON auth.user_status_history;
DROP POLICY IF EXISTS user_status_history_admin_all ON auth.user_status_history;
DROP POLICY IF EXISTS user_status_history_ro_select ON auth.user_status_history;
ALTER TABLE auth.user_status_history DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS prevent_user_status_history_update ON auth.user_status_history;

DROP TRIGGER IF EXISTS prevent_user_status_history_delete ON auth.user_status_history;

DROP TRIGGER IF EXISTS prevent_user_history_created_at_update ON auth.user_status_history;

DROP FUNCTION IF EXISTS prevent_user_status_history_delete();

DROP INDEX IF EXISTS idx_user_status_history_new_status;

DROP INDEX IF EXISTS idx_user_status_history_user_id;

DROP TABLE IF EXISTS auth.user_status_history;
