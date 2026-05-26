\set ON_ERROR_STOP on

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'db_user', :'db_password')
WHERE NOT EXISTS (
    SELECT 1
    FROM pg_roles
    WHERE rolname = :'db_user'
)
\gexec

SELECT format('CREATE DATABASE %I OWNER %I', :'db_name', :'db_user')
WHERE NOT EXISTS (
    SELECT 1
    FROM pg_database
    WHERE datname = :'db_name'
)
\gexec
