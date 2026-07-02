-- Reverses 000013_create_account_suspensions_table.up.sql. drops the account_suspensions table and its
-- suspension_reason enum.

-- Drop policies before disabling RLS.
DROP POLICY IF EXISTS account_suspensions_self_read ON auth.account_suspensions;
DROP POLICY IF EXISTS account_suspensions_admin_all ON auth.account_suspensions;
DROP POLICY IF EXISTS account_suspensions_ro_select ON auth.account_suspensions;
ALTER TABLE auth.account_suspensions DISABLE ROW LEVEL SECURITY;

DROP TABLE IF EXISTS auth.account_suspensions;

DROP TYPE IF EXISTS auth.suspension_reason;
