---
applyTo: "**/*.go"
---

# Go Code Review Standards

## Documentation

- All exported types, functions, and constants must have godoc comments starting with the symbol name.
- Godoc comments are full sentences ending with a period.

## Error Handling

- Error messages use lowercase, no trailing punctuation: `fmt.Errorf("open database: %w", err)`
- Wrap errors with `%w` for context; prefix with the function or operation name.
- No naked returns; explicit return values in all paths.

## Style

- Use `context.Context` as the first parameter where appropriate.
- Avoid global mutable state; use dependency injection.
- Prefer `log/slog` structured logging (no `log.Printf`).
- Keep functions focused; flag functions exceeding ~80 lines for review.
- Package-internal helpers are preferred over cross-package utility packages.
- No naked returns; explicit return values in all paths.

## Security

- No command injection, SQL injection, XSS, or other OWASP top-10 vulnerabilities.
- Flag any hardcoded secrets, credentials, or connection strings.
- Use `database/sql` with parameterized queries; never string-concatenate SQL.
- Validate at system boundaries (user input, external APIs); trust internal code.

## Admin Dashboard Patterns

- Chi router groups must apply middleware in the correct order: `AdminAuthMiddleware` before `CSRFMiddleware`.
- Handlers must detect `HX-Request` header to return partials instead of full pages.
- The `render()` helper must inject session data (user info, CSRF token) into every template context.
- Store dependencies must be nil-checked during handler initialization, not at request time.
- State-changing operations (deregister, revoke, policy changes) must create audit log entries and broadcast events.

## Embedded Filesystems

- `//go:embed` directives must be at package scope, not inside functions.
- Templates must be parsed with `template.Must(template.ParseFS(...))` at init time.
- Static file serving must use `fs.Sub` to strip the prefix, then `http.FileServer`.

## Dependency Injection

- Prefer deps structs over long parameter lists for handler constructors.
- Dependencies should be typed as interfaces for testability.
- Nil-safe defaults should be provided where a dependency is optional.
