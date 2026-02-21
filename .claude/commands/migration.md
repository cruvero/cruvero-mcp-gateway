Create a new database migration pair following project conventions.

## Instructions

1. Scan `migrations/` to find the highest existing migration number (currently `0006`).
2. Compute the next number as `NNNN` (zero-padded to 4 digits).
3. Use the argument as the migration name: `$ARGUMENTS` (e.g., `tool_classifications`).
   - If no argument is given, ask the user for a descriptive snake_case name.
4. Create two files:
   - `migrations/NNNN_<name>.up.sql`
   - `migrations/NNNN_<name>.down.sql`
5. Ask the user what the migration should do, then write the SQL.

## SQL Conventions (from existing migrations)

- Use `UUID` primary keys with `gen_random_uuid()` default.
- Use `TIMESTAMPTZ` for all timestamps with `NOW()` default.
- Use `JSONB` for flexible/extensible columns.
- Include `created_at` and `updated_at` on new tables.
- Down migrations must be the exact inverse of up (DROP TABLE, DROP COLUMN, etc.).
- Add `IF NOT EXISTS` / `IF EXISTS` guards where appropriate.
- Use `TEXT` over `VARCHAR` (Postgres best practice).

## Example

For argument `tool_classifications`:

```
migrations/0007_tool_classifications.up.sql
migrations/0007_tool_classifications.down.sql
```

## Constraints

- Never modify existing migration files — migrations are immutable once committed.
- Never mention AI, Claude, or LLM in SQL comments.
