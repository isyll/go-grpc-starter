DROP POLICY IF EXISTS users_app_all ON auth.users;
DROP POLICY IF EXISTS users_worker_all ON auth.users;
DROP POLICY IF EXISTS users_ro_select ON auth.users;
DROP TABLE IF EXISTS auth.users;
DROP TYPE IF EXISTS auth.user_status CASCADE;
DROP TYPE IF EXISTS auth.user_role CASCADE;
