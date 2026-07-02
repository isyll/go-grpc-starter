-- Append-only audit log of authentication attempts. Rows are never
-- updated or deleted.
CREATE TABLE IF NOT EXISTS auth.login_attempts (
  id BIGSERIAL PRIMARY KEY,
  email CITEXT NOT NULL,
  user_id BIGINT,
  channel VARCHAR(20) NOT NULL
    CHECK (channel IN ('login', 'refresh', 'password_reset')),
  outcome VARCHAR(20) NOT NULL
    CHECK (outcome IN ('success', 'wrong_password', 'not_found', 'rate_limited', 'blocked')),
  remaining INTEGER CHECK (remaining IS NULL OR remaining >= 0),
  ip_address TEXT,
  user_agent TEXT,
  device_id TEXT,
  request_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_login_attempts_email_created
ON auth.login_attempts (email, created_at DESC)
WHERE outcome IN ('wrong_password', 'blocked');

CREATE INDEX IF NOT EXISTS idx_login_attempts_ip_created
ON auth.login_attempts (ip_address, created_at DESC)
WHERE ip_address IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_login_attempts_created
ON auth.login_attempts (created_at DESC);

-- Append-only enforcement.
CREATE OR REPLACE FUNCTION auth.prevent_login_attempts_change() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'auth.login_attempts is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS prevent_login_attempts_update ON auth.login_attempts;
CREATE TRIGGER prevent_login_attempts_update BEFORE UPDATE ON auth.login_attempts
FOR EACH ROW EXECUTE FUNCTION auth.prevent_login_attempts_change();

DROP TRIGGER IF EXISTS prevent_login_attempts_delete ON auth.login_attempts;
CREATE TRIGGER prevent_login_attempts_delete BEFORE DELETE ON auth.login_attempts
FOR EACH ROW EXECUTE FUNCTION auth.prevent_login_attempts_change();

ALTER TABLE auth.login_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth.login_attempts FORCE ROW LEVEL SECURITY;

CREATE POLICY login_attempts_app_insert ON auth.login_attempts
FOR INSERT TO app_api WITH CHECK (TRUE);

CREATE POLICY login_attempts_worker_all ON auth.login_attempts
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY login_attempts_ro_select ON auth.login_attempts
FOR SELECT TO app_readonly USING (TRUE);
