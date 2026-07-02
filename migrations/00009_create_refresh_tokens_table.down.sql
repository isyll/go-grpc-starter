-- Reverses 000035_create_refresh_tokens_table.up.sql by dropping RLS
-- policies, the immutable-field protection trigger and its function,
-- the lifecycle trigger, indexes, and the table.

-- Drop policies before disabling RLS.
DROP POLICY IF EXISTS refresh_tokens_owner_all ON auth.refresh_tokens;
DROP POLICY IF EXISTS refresh_tokens_admin_all ON auth.refresh_tokens;
DROP POLICY IF EXISTS refresh_tokens_ro_select ON auth.refresh_tokens;
ALTER TABLE auth.refresh_tokens DISABLE ROW LEVEL SECURITY;

DROP TRIGGER IF EXISTS prevent_refresh_token_modification_trigger ON auth.refresh_tokens;

DROP TRIGGER IF EXISTS refresh_tokens_updated_at ON auth.refresh_tokens;

DROP FUNCTION IF EXISTS prevent_refresh_token_modification();


DROP INDEX IF EXISTS idx_refresh_tokens_expires_at;

DROP INDEX IF EXISTS idx_refresh_tokens_token_prefix;

DROP INDEX IF EXISTS idx_refresh_tokens_token_family;

DROP INDEX IF EXISTS idx_refresh_tokens_token_hash;

DROP INDEX IF EXISTS idx_refresh_tokens_session_id;

DROP TABLE IF EXISTS auth.refresh_tokens CASCADE;
