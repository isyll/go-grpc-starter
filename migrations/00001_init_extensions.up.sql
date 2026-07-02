-- Preflight: fail fast if the database was not initialized with the
-- required schemas and extensions. These are provisioned once by
-- infra/postgres/init before migrations run.
DO $$
DECLARE
    missing TEXT[] := ARRAY[]::TEXT[];
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'auth') THEN
        missing := array_append(missing, 'schema auth');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'notifications') THEN
        missing := array_append(missing, 'schema notifications');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'audit') THEN
        missing := array_append(missing, 'schema audit');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'events') THEN
        missing := array_append(missing, 'schema events');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgcrypto') THEN
        missing := array_append(missing, 'extension pgcrypto');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'citext') THEN
        missing := array_append(missing, 'extension citext');
    END IF;

    IF array_length(missing, 1) > 0 THEN
        RAISE EXCEPTION 'Database not initialized. Missing: %', array_to_string(missing, ', ');
    END IF;
END $$;
