---
applyTo: "README.md"
---

# README Freshness Review

## Update Requirements

Any PR adding new features, CLI subcommands, or environment variables must update README.md accordingly.

## Sections to Verify

- The "Key Features" section must reflect all major capabilities.
- The "Environment Variables" table must include any new `MCPGW_*` vars.
- The "Dependencies" table must include any new Go module dependencies.
- The "Repository Layout" tree must reflect new directories.
- The "Phase Roadmap" table should be updated when phases complete.
- The "CLI Subcommands" list (if present) must match `cmd/mcpgw/main.go` subcommands.
- Quick Start instructions must remain functional.
- Admin Dashboard section must document OIDC configuration requirements and available dashboard pages.
- Device Code Flow section must document the `auth` and `mcp-proxy` CLI commands and their usage.
- CLI Subcommands list must include `auth` and `mcp-proxy` commands.
