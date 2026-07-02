-- Append-only record of every modification to auth.account_suspensions rows.
-- The trigger fires AFTER UPDATE when any of the mutable suspension
-- fields (reason, details, suspended_until, is_permanent) change.
-- A revoked or amended ban leaves a complete audit trail here.
CREATE TABLE IF NOT EXISTS auth.account_suspension_history (
  id BIGSERIAL PRIMARY KEY,
  suspension_id BIGINT NOT NULL
  REFERENCES auth.account_suspensions (id) ON DELETE RESTRICT,
  user_id BIGINT NOT NULL
  REFERENCES auth.users (id) ON DELETE RESTRICT,
  old_reason auth.suspension_reason NOT NULL,
  new_reason auth.suspension_reason NOT NULL,
  old_details TEXT,
  new_details TEXT,
  old_suspended_until TIMESTAMPTZ,
  new_suspended_until TIMESTAMPTZ,
  old_is_permanent BOOLEAN NOT NULL,
  new_is_permanent BOOLEAN NOT NULL,
  change_actor auth.change_actor NOT NULL DEFAULT 'admin',
  change_actor_id BIGINT,
  change_actor_label TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Lookup by suspended user: full amendment history for a subject.
CREATE INDEX IF NOT EXISTS idx_account_suspension_history_user_id
ON auth.account_suspension_history (user_id, created_at DESC);

-- Lookup by suspension record: all modifications to one ban entry.
CREATE INDEX IF NOT EXISTS idx_account_suspension_history_suspension_id
ON auth.account_suspension_history (suspension_id, created_at DESC);

-- Deny all UPDATE and DELETE - rows are immutable once written.
DROP TRIGGER IF EXISTS deny_account_suspension_history_update
ON auth.account_suspension_history;
CREATE TRIGGER deny_account_suspension_history_update
BEFORE UPDATE ON auth.account_suspension_history
FOR EACH ROW EXECUTE FUNCTION public.prevent_status_history_update();

CREATE OR REPLACE FUNCTION prevent_account_suspension_history_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
BEGIN
    RAISE EXCEPTION
        'Deletion of auth.account_suspension_history records is not '
        'allowed. Suspension history is an immutable audit trail.'
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

DROP TRIGGER IF EXISTS deny_account_suspension_history_delete
ON auth.account_suspension_history;
CREATE TRIGGER deny_account_suspension_history_delete
BEFORE DELETE ON auth.account_suspension_history
FOR EACH ROW EXECUTE FUNCTION prevent_account_suspension_history_delete();

-- Trigger function: fires AFTER UPDATE on auth.account_suspensions when
-- any of the four mutable fields change. The old and new values of all
-- four fields are captured together so a single row describes the full
-- diff of one amendment event. Runs as SECURITY DEFINER.
CREATE OR REPLACE FUNCTION log_account_suspension_change()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
BEGIN
    IF OLD.reason IS DISTINCT FROM NEW.reason
        OR OLD.details IS DISTINCT FROM NEW.details
        OR OLD.suspended_until IS DISTINCT FROM NEW.suspended_until
        OR OLD.is_permanent IS DISTINCT FROM NEW.is_permanent
    THEN
        INSERT INTO auth.account_suspension_history (
            suspension_id,
            user_id,
            old_reason,
            new_reason,
            old_details,
            new_details,
            old_suspended_until,
            new_suspended_until,
            old_is_permanent,
            new_is_permanent,
            change_actor,
            change_actor_id,
            change_actor_label
        ) VALUES (
            NEW.id,
            NEW.user_id,
            OLD.reason,
            NEW.reason,
            OLD.details,
            NEW.details,
            OLD.suspended_until,
            NEW.suspended_until,
            OLD.is_permanent,
            NEW.is_permanent,
            'admin',
            NULL,
            NULL
        );
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_log_account_suspension_change
ON auth.account_suspensions;
CREATE TRIGGER trg_log_account_suspension_change
AFTER UPDATE OF reason, details, suspended_until, is_permanent
ON auth.account_suspensions
FOR EACH ROW EXECUTE FUNCTION log_account_suspension_change();

COMMENT ON TABLE auth.account_suspension_history IS
'Append-only log of every field-level amendment to
auth.account_suspensions rows. Allows trust-and-safety teams to
reconstruct the full lifecycle of a ban: who changed what, from which
value to which, and when.';

COMMENT ON COLUMN auth.account_suspension_history.suspension_id IS
'FK to the suspension record that was amended.';

COMMENT ON COLUMN auth.account_suspension_history.user_id IS
'The suspended user (denormalized from the parent row for efficient
per-user queries without a join).';

COMMENT ON COLUMN auth.account_suspension_history.old_reason IS
'Suspension reason code before the amendment.';

COMMENT ON COLUMN auth.account_suspension_history.new_reason IS
'Suspension reason code after the amendment.';

COMMENT ON COLUMN auth.account_suspension_history.old_suspended_until IS
'Expiry timestamp before the amendment. NULL for permanent bans.';

COMMENT ON COLUMN auth.account_suspension_history.new_suspended_until IS
'Expiry timestamp after the amendment. NULL for permanent bans.';

COMMENT ON COLUMN auth.account_suspension_history.old_is_permanent IS
'Permanence flag before the amendment.';

COMMENT ON COLUMN auth.account_suspension_history.new_is_permanent IS
'Permanence flag after the amendment. A TRUE→FALSE transition means
the ban was downgraded from permanent to temporary.';

COMMENT ON COLUMN auth.account_suspension_history.change_actor IS
'Who initiated the amendment. Almost always admin; system for automated
expiry workflows.';

COMMENT ON COLUMN auth.account_suspension_history.change_actor_id IS
'Internal ID of the admin who made the change. NULL for system actors.';

COMMENT ON COLUMN auth.account_suspension_history.change_actor_label IS
'Human-readable label for non-admin actors (e.g. "cron:expiry_sweep").';

COMMENT ON FUNCTION prevent_account_suspension_history_delete() IS
'BEFORE DELETE trigger on auth.account_suspension_history that raises
an unconditional exception. Suspension-history rows are immutable audit
records and must never be physically deleted.';

COMMENT ON FUNCTION log_account_suspension_change() IS
'AFTER UPDATE trigger on auth.account_suspensions that inserts one row
into auth.account_suspension_history whenever any mutable field
(reason, details, suspended_until, is_permanent) changes. Captures the
full old/new diff in one row. Runs as SECURITY DEFINER.';

ALTER TABLE auth.account_suspension_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth.account_suspension_history FORCE ROW LEVEL SECURITY;

CREATE POLICY account_suspension_history_definer_insert
ON auth.account_suspension_history
FOR INSERT TO app_owner
WITH CHECK (TRUE);

CREATE POLICY account_suspension_history_admin_all
ON auth.account_suspension_history
FOR ALL TO app_worker
USING (TRUE)
WITH CHECK (TRUE);

CREATE POLICY account_suspension_history_ro_select
ON auth.account_suspension_history
FOR SELECT TO app_readonly
USING (TRUE);

COMMENT ON POLICY account_suspension_history_definer_insert
ON auth.account_suspension_history IS
'Permits the SECURITY DEFINER trigger function (owned by the app_owner DDL
role) to insert rows. This is the sole automated INSERT path.';

COMMENT ON POLICY account_suspension_history_admin_all
ON auth.account_suspension_history IS
'app_worker (backoffice and trust-and-safety workers) has full access.
UPDATE and DELETE are blocked by the deny triggers.';

COMMENT ON POLICY account_suspension_history_ro_select
ON auth.account_suspension_history IS
'Read-only reporting role may SELECT for analytics and audit reporting.';
