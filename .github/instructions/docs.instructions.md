---
applyTo: "**/*.go"
---

# Documentation Coverage Review

## Godoc Requirements

- Every exported symbol (type, func, const, var) must have a godoc comment starting with the symbol name.
- Godoc comments are full sentences ending with a period.
- Package-level doc comments in a `doc.go` or at the top of the primary file.
- Flag any new exported symbol missing documentation.

## Comment Quality

- Comments should explain "why", not "what" — the code itself shows "what".
- Inline comments only where logic is non-obvious.
- No TODO comments without an associated issue number: `// TODO(#123): ...`
