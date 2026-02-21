#!/bin/bash
set -eu

# Create the test database for integration tests.
# This script runs automatically via /docker-entrypoint-initdb.d/ on first postgres start.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    CREATE DATABASE mcpgw_test OWNER $POSTGRES_USER;
EOSQL

echo "Created mcpgw_test database."
