#!/bin/bash
set -eu

# Create the test database and Keycloak schema used by the local standalone stack.
# This script runs automatically via /docker-entrypoint-initdb.d/ on first postgres start.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    CREATE SCHEMA IF NOT EXISTS keycloak AUTHORIZATION $POSTGRES_USER;
EOSQL

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<-EOSQL
    CREATE DATABASE mcpgw_test OWNER $POSTGRES_USER;
EOSQL

echo "Created keycloak schema and mcpgw_test database."
