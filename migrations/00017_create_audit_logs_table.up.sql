-- Append-only admin audit log. Written by the async AuditLogWritten
-- event handler in the event-dispatcher worker.
CREATE TABLE IF NOT EXISTS audit.audit_logs (
  id BIGSERIAL PRIMARY KEY,
  admin_id BIGINT NOT NULL,
  action VARCHAR(100) NOT NULL,
  resource VARCHAR(100) NOT NULL,
  resource_id TEXT,
  details JSONB,
  status VARCHAR(20) NOT NULL DEFAULT 'success',
  ip_address TEXT,
  user_agent TEXT,
  request_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_admin_created
ON audit.audit_logs (admin_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_logs_action_created
ON audit.audit_logs (action, created_at DESC);

-- Append-only enforcement.
CREATE OR REPLACE FUNCTION audit.prevent_audit_logs_change() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'audit.audit_logs is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS prevent_audit_logs_update ON audit.audit_logs;
CREATE TRIGGER prevent_audit_logs_update BEFORE UPDATE ON audit.audit_logs
FOR EACH ROW EXECUTE FUNCTION audit.prevent_audit_logs_change();

DROP TRIGGER IF EXISTS prevent_audit_logs_delete ON audit.audit_logs;
CREATE TRIGGER prevent_audit_logs_delete BEFORE DELETE ON audit.audit_logs
FOR EACH ROW EXECUTE FUNCTION audit.prevent_audit_logs_change();

ALTER TABLE audit.audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.audit_logs FORCE ROW LEVEL SECURITY;

-- The worker (app_worker) writes rows; app_api and app_readonly may read.
CREATE POLICY audit_logs_worker_all ON audit.audit_logs
FOR ALL TO app_worker USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY audit_logs_app_select ON audit.audit_logs
FOR SELECT TO app_api USING (TRUE);

CREATE POLICY audit_logs_ro_select ON audit.audit_logs
FOR SELECT TO app_readonly USING (TRUE);
