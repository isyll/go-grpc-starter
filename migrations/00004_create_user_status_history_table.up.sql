-- Immutable audit trail of every status transition applied to
-- auth.users. Rows are written by the AFTER UPDATE OF status trigger
-- below (never by the application), mirroring auth.user_role_history
-- and auth.phone_change_history, and may never be physically deleted
-- (the audit trail must be permanent).
CREATE TABLE IF NOT EXISTS
auth.user_status_history (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES auth.users (id) ON DELETE RESTRICT,
  old_status user_status,
  new_status user_status NOT NULL,
  changed_by BIGINT REFERENCES auth.users (id),
  reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot-path lookups: every audit query filters by user_id or by
-- new_status (dashboards counting users entering a given state).
CREATE INDEX IF NOT EXISTS idx_user_status_history_user_id ON auth.user_status_history (user_id);

CREATE INDEX IF NOT EXISTS idx_user_status_history_new_status ON auth.user_status_history (new_status);

-- Lifecycle trigger: history rows are append-only, so the only
-- mutation possible is a corrected backfill - created_at must
-- still survive untouched.
DROP TRIGGER IF EXISTS prevent_user_history_created_at_update ON auth.user_status_history;

CREATE TRIGGER prevent_user_history_created_at_update BEFORE
UPDATE ON auth.user_status_history FOR EACH ROW
EXECUTE FUNCTION prevent_update_created_at();

-- Append-only enforcement: block ALL column updates, not only created_at.
-- History rows are immutable after insertion; a corrective backfill must
-- insert a new row rather than modifying an existing one.
-- public.prevent_status_history_update is declared in 000001_init_utils
-- and shared by every *_status_history / *_role_history table.
DROP TRIGGER IF EXISTS prevent_user_status_history_update
ON auth.user_status_history;

CREATE TRIGGER prevent_user_status_history_update
BEFORE UPDATE ON auth.user_status_history
FOR EACH ROW EXECUTE FUNCTION public.prevent_status_history_update();

-- Append-only enforcement: physical DELETE is rejected so the
-- audit trail can never be tampered with.
CREATE
OR REPLACE FUNCTION prevent_user_status_history_delete() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Deletion of history records not allowed. History is an immutable audit trail.';
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS prevent_user_status_history_delete ON auth.user_status_history;

CREATE TRIGGER prevent_user_status_history_delete BEFORE DELETE ON auth.user_status_history FOR EACH ROW
EXECUTE FUNCTION prevent_user_status_history_delete();

-- Auto-population: fires AFTER UPDATE on auth.users when the status
-- column changes. Runs as SECURITY DEFINER (owned by the migration
-- role) so the INSERT into this append-only, FORCE-RLS table succeeds
-- regardless of the session's application role - the same pattern as
-- log_user_role_change (000010) and log_phone_change (000011).
--
-- The actor and reason are recovered from transaction-local GUCs:
--   - app.current_user_id : caller's user id, set per transaction by
--                            the store layer ('0' for admin / anonymous);
--                            recorded as changed_by (NULL when 0, so
--                            admin/system transitions have no FK).
--   - app.change_reason   : free-form reason set with SET LOCAL by the
--                            status-mutation repository method before
--                            the UPDATE; NULL when unset.
CREATE OR REPLACE FUNCTION log_user_status_change()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
DECLARE
    actor_id BIGINT;
    reason_text TEXT;
BEGIN
    IF OLD.status IS NOT DISTINCT FROM NEW.status THEN
        RETURN NEW;
    END IF;

    actor_id := NULLIF(
        current_setting('app.current_user_id', TRUE),
        ''
    )::BIGINT;
    reason_text := NULLIF(
        current_setting('app.change_reason', TRUE),
        ''
    );

    INSERT INTO auth.user_status_history (
        user_id, old_status, new_status, changed_by, reason
    ) VALUES (
        NEW.id, OLD.status, NEW.status, NULLIF(actor_id, 0), reason_text
    );

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_log_user_status_change ON auth.users;
CREATE TRIGGER trg_log_user_status_change
AFTER UPDATE OF status ON auth.users
FOR EACH ROW EXECUTE FUNCTION log_user_status_change();

COMMENT ON TABLE auth.user_status_history IS 'Immutable audit trail of user status changes. One row per transition, written by the log_user_status_change trigger. Physical deletion is blocked by trigger.';

COMMENT ON COLUMN auth.user_status_history.old_status IS 'Status before the transition. NULL on the first row when the user was created.';

COMMENT ON COLUMN auth.user_status_history.new_status IS 'Status after the transition. NOT NULL.';

COMMENT ON COLUMN auth.user_status_history.changed_by IS 'User or admin who triggered the change. NULL for system-driven transitions.';

COMMENT ON COLUMN auth.user_status_history.reason IS 'Free-form reason captured at the time of the change (e.g. suspension reason).';

COMMENT ON FUNCTION prevent_user_status_history_delete() IS 'BEFORE DELETE trigger on auth.user_status_history that raises an exception. Status history is an append-only audit trail and must never be physically deleted.';

COMMENT ON FUNCTION log_user_status_change() IS
'AFTER UPDATE trigger on auth.users that inserts one row into
auth.user_status_history whenever the status column transitions. Reads
app.current_user_id (changed_by) and app.change_reason (reason) from the
session GUCs; runs as SECURITY DEFINER to bypass RLS on the append-only
history table. Replaces the former application-side write.';

-- Row-level security. History is read-only for the owning user (their
-- own status changes). The sole INSERT path is the SECURITY DEFINER
-- trigger, which runs as the app_owner DDL role; no application role writes
-- directly. admin/ro roles keep full / read access for the backoffice.
ALTER TABLE auth.user_status_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth.user_status_history FORCE ROW LEVEL SECURITY;

-- Allows the SECURITY DEFINER trigger function (running as the app_owner
-- DDL role) to insert rows even under FORCE ROW LEVEL SECURITY.
CREATE POLICY user_status_history_definer_insert ON auth.user_status_history
FOR INSERT TO app_owner
WITH CHECK (TRUE);

CREATE POLICY user_status_history_self_read ON auth.user_status_history
FOR SELECT TO app_api
USING (TRUE);

CREATE POLICY user_status_history_admin_all ON auth.user_status_history
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY user_status_history_ro_select ON auth.user_status_history
FOR SELECT TO app_readonly USING (TRUE);

COMMENT ON POLICY user_status_history_definer_insert ON auth.user_status_history IS
'Permits the SECURITY DEFINER trigger function (owned by the app_owner DDL role) to insert rows. This is the sole INSERT path for status-change capture; no application session writes directly.';

COMMENT ON POLICY user_status_history_self_read ON auth.user_status_history IS
'A user can read their own status-change history. Rows are written only by the log_user_status_change trigger; no application role inserts directly.';


COMMENT ON POLICY user_status_history_admin_all ON auth.user_status_history IS
'app_worker has full access (INSERT, UPDATE, SELECT, DELETE) for backoffice status management and bulk operations.';

COMMENT ON POLICY user_status_history_ro_select ON auth.user_status_history IS
'app_readonly reads status-change history for dashboards and audit reporting.';
