---
applyTo: "internal/admin/templates/**,internal/admin/static/**"
---

# Template and Static Asset Review

## Go Templates

- HTML templates must use `html/template`, never `text/template`.
- Templates must use the `{{define "content"}}` block pattern for page-specific content.
- Table partials must be self-contained for HTMX swap targets.
- Use `{{else}}` inside `{{range}}` blocks to render empty states.

## HTMX

- HTMX attributes must use valid verbs: `hx-get`, `hx-post`, `hx-put`, `hx-delete`.
- Every `hx-get`/`hx-post` must have a corresponding `hx-target`.
- Search inputs should use `hx-trigger` with debounce (e.g., `keyup changed delay:300ms`).
- Polling intervals must be at least 5 seconds to avoid server overload.

## Alpine.js

- `x-data` must define all state properties referenced by `x-model`, `x-show`, and `x-text`.
- Avoid inline JavaScript beyond Alpine.js directives.

## CSRF

- Every `<form method="POST">` (and PUT/DELETE) must include `<input type="hidden" name="_csrf" value="{{.CSRFToken}}">`.
- HTMX POST requests must include CSRF via `hx-include` or `hx-headers`.

## Confirm Dialogs

- Destructive actions (deregister, delete, revoke) must have a confirmation prompt before submission.

## CSS

- Custom properties must be defined in `:root`.
- Badge classes follow the `.badge-{status}` convention.
- Use `rem` units for spacing and font sizes.
- Layout should use CSS grid or flexbox, not floats or absolute positioning.
