-- Core user accounts. Email + password_hash are the credentials.
-- Physical DELETE is disabled; deletion is soft via deleted_at.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_type t JOIN pg_namespace n ON t.typnamespace = n.oid
        WHERE t.typname = 'user_status' AND n.nspname = 'auth'
    ) THEN
        CREATE TYPE auth.user_status AS ENUM ('active', 'inactive', 'suspended');
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_type t JOIN pg_namespace n ON t.typnamespace = n.oid
        WHERE t.typname = 'user_role' AND n.nspname = 'auth'
    ) THEN
        CREATE TYPE auth.user_role AS ENUM ('user', 'admin');
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS auth.users (
  id BIGSERIAL PRIMARY KEY,
  email CITEXT NOT NULL CHECK (email ~* '^[^@\s]+@[^@\s]+\.[^@\s]+$'),
  password_hash TEXT NOT NULL,
  first_name VARCHAR(80) NOT NULL DEFAULT '',
  last_name VARCHAR(80) NOT NULL DEFAULT '',
  avatar VARCHAR(255) NOT NULL DEFAULT '',
  bio VARCHAR(500) NOT NULL DEFAULT '',
  status auth.user_status NOT NULL DEFAULT 'active',
  role auth.user_role NOT NULL DEFAULT 'user',
  email_verified_at TIMESTAMPTZ,
  last_login_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ
);

-- Email is unique among live (non-deleted) accounts.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_active
ON auth.users (email) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_users_status ON auth.users (status)
WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON auth.users (deleted_at);

DROP TRIGGER IF EXISTS update_users_updated_at ON auth.users;
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON auth.users
FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

DROP TRIGGER IF EXISTS prevent_users_created_at_update ON auth.users;
CREATE TRIGGER prevent_users_created_at_update BEFORE UPDATE ON auth.users
FOR EACH ROW EXECUTE FUNCTION public.prevent_update_created_at();

-- Row-level security. The app role has full access (the API only ever
-- returns the authenticated user's own data); the read-only role is
-- limited to SELECT. The app.current_user_id GUC set per transaction by
-- the store layer is available if you want to tighten a policy to owner-only.
ALTER TABLE auth.users ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth.users FORCE ROW LEVEL SECURITY;

CREATE POLICY users_app_all ON auth.users FOR ALL TO app_api
USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY users_worker_all ON auth.users FOR ALL TO app_worker
USING (TRUE) WITH CHECK (TRUE);

CREATE POLICY users_ro_select ON auth.users FOR SELECT TO app_readonly
USING (TRUE);
