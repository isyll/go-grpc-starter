-- Suspension records issued against a user account.
-- Captures both temporary (with suspended_until) and permanent bans
-- with a standardized reason code. Mutations are admin-only;
-- the affected user only reads their own row.

DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_type t
        JOIN pg_namespace n ON t.typnamespace = n.oid
        WHERE t.typname = 'suspension_reason' AND n.nspname = 'auth'
    ) THEN
        CREATE TYPE auth.suspension_reason AS ENUM (
            'terms_violation',
            'fraudulent_activity',
            'harassment',
            'spam',
            'security_breach',
            'legal_request',
            'other'
        );
    END IF;
END $$;

COMMENT ON TYPE auth.suspension_reason IS
'Standardized reason codes for account suspensions. Used by trust-and-safety to
classify bans for analytics and appeal workflows.
Values: terms_violation - generic ToS breach; fraudulent_activity - fake
listings or payments; payment_issues - unresolved disputes; identity_verification_failed
- did not pass KYC; harassment - towards passengers or drivers; inappropriate_behavior
- conduct violations; safety_concerns - risk to other users; spam - mass
messaging or fake listings; fake_account - identity impersonation; multiple_accounts
- operating more than one account; repeated_cancellations - pattern of
last-minute cancellations; poor_ratings - sustained sub-threshold score;
legal_request - government or law-enforcement order; security_breach - compromised
account; other - catch-all pending reclassification.';

CREATE TABLE IF NOT EXISTS auth.account_suspensions (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES auth.users (id) ON DELETE CASCADE,
  reason auth.suspension_reason NOT NULL,
  details TEXT,
  suspended_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  suspended_until TIMESTAMPTZ,
  is_permanent BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot path: look up suspensions for a user, ordered by expiry.
CREATE INDEX IF NOT EXISTS idx_account_suspensions_user_id ON auth.account_suspensions (user_id, suspended_until);

-- Partial index: enumerate permanent bans without scanning expired ones.
CREATE INDEX IF NOT EXISTS idx_account_suspensions_permanent ON auth.account_suspensions (user_id)
WHERE is_permanent = TRUE;

-- Partial index: drive the "is this user currently suspended?" check
-- by ordering temporary suspensions by their end time.
CREATE INDEX IF NOT EXISTS idx_account_suspensions_temporary ON auth.account_suspensions (user_id, suspended_until)
WHERE is_permanent = FALSE;

-- Most-recent-suspension lookup: WHERE user_id = ? ORDER BY suspended_at
-- DESC LIMIT 1. Complements the existing user_id partial indexes which
-- cover suspended_until range checks.
CREATE INDEX IF NOT EXISTS idx_account_suspensions_user_suspended_at
ON auth.account_suspensions (user_id, suspended_at DESC);

DROP TRIGGER IF EXISTS update_account_suspensions_updated_at ON auth.account_suspensions;

CREATE TRIGGER update_account_suspensions_updated_at BEFORE
UPDATE ON auth.account_suspensions FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS prevent_account_suspensions_created_at_update ON auth.account_suspensions;

CREATE TRIGGER prevent_account_suspensions_created_at_update BEFORE
UPDATE ON auth.account_suspensions FOR EACH ROW
EXECUTE FUNCTION prevent_update_created_at();

COMMENT ON TABLE auth.account_suspensions IS
'User account suspension records. One row per suspension event; a user may accumulate multiple rows over time. Active suspension lookup uses is_permanent OR suspended_until > now().';

COMMENT ON COLUMN auth.account_suspensions.user_id IS
'User whose account is suspended. ON DELETE CASCADE so suspensions disappear with the user account.';

COMMENT ON COLUMN auth.account_suspensions.reason IS
'Standardized suspension reason code (auth.suspension_reason enum).';

COMMENT ON COLUMN auth.account_suspensions.details IS
'Free-form admin note explaining the suspension in detail.';

COMMENT ON COLUMN auth.account_suspensions.suspended_at IS
'Timestamp when the suspension started. May predate created_at if the suspension is back-dated by the backoffice.';

COMMENT ON COLUMN auth.account_suspensions.suspended_until IS
'End timestamp for temporary suspensions. NULL when is_permanent = TRUE.';

COMMENT ON COLUMN auth.account_suspensions.is_permanent IS
'TRUE for permanent bans. When TRUE, suspended_until is ignored by the active-suspension check.';

-- RLS: suspensions are admin-written but visible to the suspended
-- user so the mobile client can render a clear "your account is
-- suspended" banner. Mutations are reserved for app_worker
-- (workers calling internal admin paths or the backoffice).
ALTER TABLE auth.account_suspensions ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth.account_suspensions FORCE ROW LEVEL SECURITY;

CREATE POLICY account_suspensions_self_read ON auth.account_suspensions
FOR SELECT TO app_api
USING (TRUE);

CREATE POLICY account_suspensions_admin_all ON auth.account_suspensions
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY account_suspensions_ro_select ON auth.account_suspensions
FOR SELECT TO app_readonly USING (TRUE);

COMMENT ON POLICY account_suspensions_self_read ON auth.account_suspensions IS
'A user can read their own suspension state. Only app_worker (backoffice) may insert or update.';

COMMENT ON POLICY account_suspensions_admin_all ON auth.account_suspensions IS
'Trust-and-safety backoffice and admin workers have full access to insert, amend, or lift suspensions.';

COMMENT ON POLICY account_suspensions_ro_select ON auth.account_suspensions IS
'Read-only role for analytics dashboards (Grafana / abuse-rate reports).';
