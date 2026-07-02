-- Refresh tokens issued for JWT authentication with simple rotation.
-- Each row is write-once after insert: rotation creates a new row in
-- the same token_family and marks the previous one revoked via
-- revoked_at, so the family doubles as a replay-detection lineage.
CREATE TABLE IF NOT EXISTS auth.refresh_tokens (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id BIGINT NOT NULL REFERENCES auth.device_sessions (id) ON DELETE CASCADE,
  token_hash VARCHAR(64) UNIQUE NOT NULL,
  -- token_prefix is the first 8 hex characters of token_hash. The
  -- application queries WHERE token_prefix = ? to narrow the row set
  -- before doing a constant-time comparison on the full hash, preventing
  -- timing side-channels during token refresh.
  token_prefix VARCHAR(8) NOT NULL,
  token_family UUID NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  revoked_reason TEXT,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now(),
  CONSTRAINT fk_refresh_tokens_session FOREIGN KEY (session_id) REFERENCES auth.device_sessions (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_session_id ON auth.refresh_tokens (session_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON auth.refresh_tokens (token_hash);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token_family ON auth.refresh_tokens (token_family);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON auth.refresh_tokens (expires_at);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token_prefix ON auth.refresh_tokens (token_prefix);

-- Lifecycle trigger: updated_at maintenance.
DROP TRIGGER IF EXISTS refresh_tokens_updated_at ON auth.refresh_tokens;
CREATE TRIGGER refresh_tokens_updated_at BEFORE
UPDATE ON auth.refresh_tokens FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

COMMENT ON TABLE auth.refresh_tokens IS 'Refresh tokens for JWT authentication with simple rotation. Each row is write-once after insert; rotation creates a new row in the same family.';
COMMENT ON COLUMN auth.refresh_tokens.session_id IS 'Owning device session. CASCADE delete removes refresh tokens when the session is revoked.';
COMMENT ON COLUMN auth.refresh_tokens.token_hash IS 'SHA-256 hash of the refresh token. The clear token is never stored.';
COMMENT ON COLUMN auth.refresh_tokens.token_family IS 'Token family identifier for rotation tracking and replay detection. All rotations of the same logical credential share a token_family.';
COMMENT ON COLUMN auth.refresh_tokens.revoked_at IS 'Timestamp when the token was revoked. NULL means the token is still valid (subject to expires_at).';
COMMENT ON COLUMN auth.refresh_tokens.revoked_reason IS 'Free-form reason recorded at revocation (rotation, logout, replay-detected, admin-revoked).';
COMMENT ON COLUMN auth.refresh_tokens.token_prefix IS
'First 8 hex characters of token_hash, indexed for cheap prefix lookup. '
'The application queries WHERE token_prefix = ? to narrow the row set, '
'then uses constant-time comparison on the full hash to prevent timing '
'side-channels during token refresh.';

-- Immutable-field protection: only revoked_at and revoked_reason
-- may be updated post-insert. Everything else (id, session_id,
-- token_hash, token_family, created_at) is immutable.
CREATE OR REPLACE FUNCTION prevent_refresh_token_modification() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.id IS DISTINCT FROM NEW.id THEN
        RAISE EXCEPTION 'Cannot modify refresh token id';
    END IF;
    IF OLD.session_id IS DISTINCT FROM NEW.session_id THEN
        RAISE EXCEPTION 'Cannot modify refresh token session_id';
    END IF;
    IF OLD.token_hash IS DISTINCT FROM NEW.token_hash THEN
        RAISE EXCEPTION 'Cannot modify refresh token hash';
    END IF;
    IF OLD.token_prefix IS DISTINCT FROM NEW.token_prefix THEN
        RAISE EXCEPTION 'Cannot modify refresh token token_prefix';
    END IF;
    IF OLD.token_family IS DISTINCT FROM NEW.token_family THEN
        RAISE EXCEPTION 'Cannot modify refresh token family';
    END IF;
    IF OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Cannot modify refresh token created_at';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS prevent_refresh_token_modification_trigger ON auth.refresh_tokens;
CREATE TRIGGER prevent_refresh_token_modification_trigger BEFORE
UPDATE ON auth.refresh_tokens FOR EACH ROW EXECUTE FUNCTION prevent_refresh_token_modification();

COMMENT ON FUNCTION prevent_refresh_token_modification() IS 'BEFORE UPDATE trigger on auth.refresh_tokens that raises an exception. Refresh tokens are write-once; rotation always creates a new row in the same family and marks the old one revoked via revoked_at.';

-- Row-level security. Refresh tokens are owned by the user
-- through their device session - there is no user_id column on
-- the row, so the predicate joins up to auth.device_sessions.
-- EXISTS keeps the join cheap on the (session_id) index.
ALTER TABLE auth.refresh_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth.refresh_tokens FORCE ROW LEVEL SECURITY;

CREATE POLICY refresh_tokens_owner_all ON auth.refresh_tokens
FOR ALL TO app_api
USING (
  EXISTS (
    SELECT 1 FROM auth.device_sessions AS ds
    WHERE ds.id = refresh_tokens.session_id
      AND ds.TRUE
  )
)
WITH CHECK (
  EXISTS (
    SELECT 1 FROM auth.device_sessions AS ds
    WHERE ds.id = refresh_tokens.session_id
      AND ds.TRUE
  )
);

CREATE POLICY refresh_tokens_admin_all ON auth.refresh_tokens
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY refresh_tokens_ro_select ON auth.refresh_tokens
FOR SELECT TO app_readonly USING (TRUE);

COMMENT ON POLICY refresh_tokens_owner_all ON auth.refresh_tokens IS
'Refresh tokens have no user_id; ownership is reached via
auth.device_sessions.user_id. EXISTS uses
idx_refresh_tokens_session_id and the device_sessions
primary-key index.';

COMMENT ON POLICY refresh_tokens_admin_all ON auth.refresh_tokens IS
'app_worker has full access for session invalidation,
token rotation audits, and security operations.';

COMMENT ON POLICY refresh_tokens_ro_select ON auth.refresh_tokens IS
'app_readonly reads token metadata for security monitoring
and dashboards.';
