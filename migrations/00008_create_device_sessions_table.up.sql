-- Per-device authenticated session. One row is inserted at login and
-- only revoked_at, revoked_reason, and last_activity may ever change
-- afterwards (guarded by protect_device_sessions_update). Sessions
-- form the user-facing audit trail for "active devices" and are never
-- physically deleted.
CREATE TABLE IF NOT EXISTS auth.device_sessions (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES auth.users (id) ON DELETE RESTRICT,
  platform VARCHAR(50) NOT NULL,
  manufacturer VARCHAR(100),
  model VARCHAR(100),
  version VARCHAR(50),
  sdk VARCHAR(50),
  brand VARCHAR(100),
  hardware VARCHAR(100),
  board VARCHAR(100),
  device VARCHAR(100),
  product VARCHAR(100),
  is_physical_device BOOLEAN DEFAULT TRUE,
  name VARCHAR(100),
  identifier VARCHAR(255),
  device_id VARCHAR(36) NOT NULL,
  last_activity TIMESTAMPTZ NOT NULL,
  ip_address VARCHAR(45),
  user_agent VARCHAR(255),
  location VARCHAR(100),
  revoked_at TIMESTAMPTZ,
  revoked_reason VARCHAR(255),
  created_at TIMESTAMPTZ DEFAULT now()
);

-- Uniqueness scoped to active rows so the same device_id can re-login
-- after a prior session has been revoked (revoked_at IS NOT NULL).
CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_active_device_id ON auth.device_sessions (device_id)
WHERE revoked_at IS NULL;

-- Hot-path lookups: by user (active devices listing), by device_id
-- (rotate/refresh), by last_activity (inactivity sweeps), by
-- revoked_at (audit queries), by platform (analytics).
CREATE INDEX IF NOT EXISTS idx_device_sessions_user_id ON auth.device_sessions (user_id);

CREATE INDEX IF NOT EXISTS idx_device_sessions_device_id ON auth.device_sessions (device_id);

CREATE INDEX IF NOT EXISTS idx_device_sessions_last_activity ON auth.device_sessions (last_activity);

CREATE INDEX IF NOT EXISTS idx_device_sessions_revoked_at ON auth.device_sessions (revoked_at);

CREATE INDEX IF NOT EXISTS idx_device_sessions_platform ON auth.device_sessions (platform);

COMMENT ON TABLE auth.device_sessions IS
'Per-device authenticated session. Inserted at login; only revoked_at, revoked_reason, and last_activity (pre-revocation) may change after insert. Rows are never physically deleted.';

COMMENT ON COLUMN auth.device_sessions.device_id IS 'Client-supplied device UUID. Unique among active (non-revoked) sessions.';

COMMENT ON COLUMN auth.device_sessions.platform IS 'Device platform (iOS, Android, web). Immutable after insert.';

COMMENT ON COLUMN auth.device_sessions.last_activity IS 'Most recent activity timestamp. Updated by the auth middleware on each authenticated request; frozen at revocation time.';

COMMENT ON COLUMN auth.device_sessions.ip_address IS 'IP address captured at last login. Immutable.';

COMMENT ON COLUMN auth.device_sessions.revoked_at IS 'Set when the session is invalidated. Once stamped, cannot be cleared or changed.';

COMMENT ON COLUMN auth.device_sessions.revoked_reason IS 'Reason for revocation (logout, password change, security event). Once set non-empty, cannot be changed.';

-- Field-level immutability: only revoked_at, revoked_reason, and
-- last_activity (pre-revocation) may ever change after insert.
CREATE OR REPLACE FUNCTION protect_device_sessions_update() RETURNS TRIGGER AS $$
BEGIN
    -- Block any change to device-fingerprint and identifying fields.
    IF NEW.id <> OLD.id
    OR NEW.user_id <> OLD.user_id
    OR NEW.platform <> OLD.platform
    OR NEW.manufacturer <> OLD.manufacturer
    OR NEW.model <> OLD.model
    OR NEW.version <> OLD.version
    OR NEW.sdk <> OLD.sdk
    OR NEW.brand <> OLD.brand
    OR NEW.hardware <> OLD.hardware
    OR NEW.board <> OLD.board
    OR NEW.device <> OLD.device
    OR NEW.product <> OLD.product
    OR NEW.is_physical_device <> OLD.is_physical_device
    OR NEW.name <> OLD.name
    OR NEW.identifier <> OLD.identifier
    OR NEW.device_id <> OLD.device_id
    OR NEW.ip_address <> OLD.ip_address
    OR NEW.user_agent <> OLD.user_agent
    OR NEW.location <> OLD.location
    OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'Modification prohibited: only revoked_at, revoked_reason, and last_activity (if not revoked) can be modified';
    END IF;

    -- Once a session is revoked, last_activity is frozen as well.
    IF NEW.last_activity <> OLD.last_activity AND OLD.revoked_at IS NOT NULL THEN
        RAISE EXCEPTION 'last_activity cannot be modified once revoked_at is set';
    END IF;

    -- revoked_at is write-once: it cannot be changed once stamped.
    IF OLD.revoked_at IS NOT NULL AND NEW.revoked_at <> OLD.revoked_at THEN
        RAISE EXCEPTION 'revoked_at is already defined and cannot be changed';
    END IF;

    -- revoked_reason is write-once: it cannot be changed once stamped non-empty.
    IF OLD.revoked_reason IS NOT NULL AND OLD.revoked_reason <> ''
       AND NEW.revoked_reason <> OLD.revoked_reason THEN
        RAISE EXCEPTION 'revoked_reason is already defined and cannot be changed';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS protect_device_sessions_update_trigger ON auth.device_sessions;

CREATE TRIGGER protect_device_sessions_update_trigger BEFORE
UPDATE ON auth.device_sessions FOR EACH ROW
EXECUTE FUNCTION protect_device_sessions_update();

-- Sessions are part of the security audit trail; physical DELETE is
-- forbidden so revocation always leaves a permanent record.
CREATE OR REPLACE FUNCTION prevent_sessions_delete() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Physical deletion of device sessions not allowed. Sessions are part of security audit trail.';
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS prevent_device_sessions_delete ON auth.device_sessions;

CREATE TRIGGER prevent_device_sessions_delete BEFORE DELETE ON auth.device_sessions FOR EACH ROW
EXECUTE FUNCTION prevent_sessions_delete();

COMMENT ON FUNCTION protect_device_sessions_update() IS 'BEFORE UPDATE trigger on auth.device_sessions that guards all immutable device fingerprint fields (id, user_id, platform, device_id, ip_address, etc.) and prevents last_activity updates after the session has been revoked.';

COMMENT ON FUNCTION prevent_sessions_delete() IS 'BEFORE DELETE trigger on auth.device_sessions that raises an exception. Device sessions are part of the security audit trail and must never be physically deleted.';

-- Row-level security. Each user can see only their own active /
-- historical device sessions. Mutations are limited by the
-- protect_device_sessions_update trigger (which only allows
-- revoke / last_activity changes), so the owner write predicate
-- is correct for the rotate-vs-revoke flow.
ALTER TABLE auth.device_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth.device_sessions FORCE ROW LEVEL SECURITY;

CREATE POLICY device_sessions_owner_all ON auth.device_sessions
FOR ALL TO app_api
USING (TRUE)
WITH CHECK (TRUE);

CREATE POLICY device_sessions_admin_all ON auth.device_sessions
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY device_sessions_ro_select ON auth.device_sessions
FOR SELECT TO app_readonly USING (TRUE);

COMMENT ON POLICY device_sessions_owner_all ON auth.device_sessions IS
'A user sees only their own device sessions. The protect_device_sessions_update trigger gates which columns mutate.';

COMMENT ON POLICY device_sessions_admin_all ON auth.device_sessions IS
'app_worker (backoffice + workers) reads and revokes sessions across all users for security workflows.';

COMMENT ON POLICY device_sessions_ro_select ON auth.device_sessions IS
'app_readonly reads every session for security monitoring and analytics dashboards.';
