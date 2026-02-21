---
applyTo: "migrations/**"
---

# SQL Migration Review

## Naming

- Files must follow the pattern `NNNN_descriptive_name.{up,down}.sql` with zero-padded 4-digit sequence numbers.
- Current highest migration number is `0009`. New migrations must use the next sequential number.
- Every up migration must have a corresponding down migration that is the exact inverse.

## Idempotency

- `CREATE TABLE` / `CREATE INDEX` must use `IF NOT EXISTS`.
- `DROP TABLE` / `DROP INDEX` must use `IF EXISTS`.
- `ALTER TABLE ADD COLUMN` should be guarded with `IF NOT EXISTS` where supported.

## Data Types and Conventions

- Use `TIMESTAMPTZ` for all timestamp columns, never `TIMESTAMP`.
- Use `gen_random_uuid()` for UUID default values.
- Use `CHECK` constraints for columns with enumerated values (e.g., status fields, role types).
- Index names should be descriptive: `idx_{table}_{column(s)}`.

## Anti-Patterns

- Flag any down migration that is missing or empty.
- Flag `DROP` statements without `IF EXISTS` guards.
- Flag data modification (`UPDATE`, `DELETE`) without a `WHERE` clause.
- Flag `TIMESTAMP` without timezone (`TIMESTAMPTZ` is required).
- Flag hardcoded UUIDs or secrets in migration data.
