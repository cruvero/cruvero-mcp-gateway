New Content Review
Phase 9 (GitOps Deployment) — Well Structured
Phase 9 is a clean addition. The devcontainer-first approach, Helm env overlays, Vault operator contract, and Argo ApplicationSet pattern are all sound. A few notes:

Phase 9A Prompt 2 references docs/phases/PHASE7B.md in Required Reading. Since files live at the project root (not docs/phases/), this should just be PHASE7B.md. Same pattern appears in several other prompts — see the remaining issue below.
Phase 9B Prompt 1 references ../cruvero/deploy/argocd/project.yaml as Required Reading. Claude Code won't have access to a sibling repo. Add a note that this is a reference pattern, or inline the key structural elements that should be mirrored.

MCP-SERVER-FLEET-GATEWAY-INTEGRATION.md — Excellent
This is a thorough fleet migration spec. The settings control flow (Cruvero → Gateway → heartbeat response → server-side apply with versioned last-known-good) is well thought out. The per-server checklist and test matrix are exactly what you need for 20-server rollout.
One observation: this doc is in the project knowledge but it's for a different concern (migrating existing MCP servers) than the gateway itself. Claude Code working on gateway phases shouldn't need to read it. Consider noting in LLM.md that this doc is reference material for fleet migration, not for gateway implementation phases 1-8.
Vault Operator Integration — Correctly Scoped
The Helm chart template list in Phase 7B now includes vault-auth.yaml, vault-secrets.yaml, validate-secrets.yaml, and runtime-secret.yaml. The OVERVIEW.md Section 13 describes the Vault operator contract. This is the right approach — no committed plaintext secrets, existingSecret references only.

Remaining Issues
1. File Path References Still Say docs/phases/ in Some Prompts
The original issue was that prompts reference docs/phases/PHASE1A.md but files live at the project root. You've partially fixed this — some prompts now just say PHASE1A.md or LLM.md. But several still reference docs/phases/:

Phase 1A Prompt 2: docs/phases/PHASE1A.md
Phase 1A Prompt 3: docs/phases/PHASE1A.md
Phase 1A Prompt 4: docs/phases/PHASE1A.md
Phase 9A Prompt 1: docs/phases/PHASE9A.md
Phase 9A Prompt 2: docs/phases/PHASE7B.md

When Claude Code actually runs against the repo, the files will live wherever you put them. If they're in docs/phases/, then these paths are correct. If they're at root (as they are here in the project knowledge), then they need updating. Decide the canonical location and make all references match. My recommendation: keep them in docs/phases/ as the README and LLM.md already say — just make sure they actually get placed there when you create the repo.
Severity: Medium. Claude Code will either find the file or ask — it won't silently produce wrong output. But it wastes a turn.
2. Phase 9B Prompt 1 References Sibling Repo
### Required Reading (read these files before writing code)
- ../cruvero/deploy/argocd/project.yaml
Claude Code can't read files outside the repo. Either inline the key structural patterns from the parent project's Argo manifests into the Phase 9B spec, or remove the reference and add a comment like "Mirror the AppProject structure used in the parent Cruvero platform."
Severity: Low. Claude Code will skip what it can't read and still produce a reasonable AppProject. But you'll get better results if the patterns are inline.
3. CLAUDE.md Could Be Stronger
The current CLAUDE.md is functional but minimal. Claude Code will find it and follow it, but you could get more mileage by adding:
markdown## Phase Implementation Workflow
1. Read the relevant PHASE*-PROMPT.md for your current task.
2. Read all files listed in "Required Reading" before writing any code.
3. Run verification commands after each prompt's implementation.
4. Do not proceed to the next prompt until verification passes.

## Critical Constraints
- Never set tls.Config.ClientAuth below RequireAndVerifyClientCert in non-test code.
- All config reads go through config.Load() — never call os.Getenv directly outside config package.
- All SQL queries must be parameterized — no string interpolation.
- All exported struct fields need json:"snake_case" tags.
- Middleware ordering: auth → rate limiting → policy → proxy. Never reorder.
This prevents the most common Claude Code mistakes for this specific project.
Severity: Low. Nice to have, not blocking.
4. Phase 7B Helm Values Missing Vault Keys in Template List
Phase 9A defines the vault values contract (vault.enabled, vault.createAuth, vault.authRef, vault.mount, vault.path, vault.refreshAfter, secrets.existingSecret). Phase 7B creates the Helm chart with Vault templates. But Phase 7B doesn't explicitly list these vault value keys in its spec — it just says "Vault operator templates" generically.
Claude Code implementing Phase 7B won't know the exact values schema unless it reads Phase 9A forward (which it shouldn't — phases are sequential). Consider adding the vault values contract to Phase 7B's scope since that's where the templates are created.
Severity: Medium. Claude Code will invent a vault values schema that may not match what Phase 9A expects.
5. nats_connected Metric vs Server Settings Metrics Mismatch
OVERVIEW.md Section 12 lists mcpgw_nats_connected as a gauge, but Phase 7A's metrics list doesn't include it. Phase 7A does list mcpgw_circuit_breaker_state but with different labels (backend, state) than OVERVIEW.md (server). The new settings metrics (mcpgw_server_settings_applied_total, mcpgw_server_settings_rejected_total, mcpgw_server_settings_version) from OVERVIEW.md don't appear in Phase 7A either.
Make Phase 7A's metric list the exact superset of everything in OVERVIEW.md Section 12.
Severity: Low. Missing metrics are easy to add later, but it's cleaner to get them right in the spec.

Architecture Observations on New Content
Server Settings Flow Is Well-Designed
The Cruvero → NATS → Gateway → heartbeat response → server-side apply pipeline with versioned configs and last-known-good fallback is a solid pattern. The separation of hot-reload-safe keys vs restart-required keys is the right call. The "reject invalid, keep last-known-good, report status on heartbeat" behavior is exactly what you want for operational safety.
Devcontainer-First Is the Right Call
Having Phase 9 start with .devcontainer/ before any cluster work ensures reproducibility. The validation matrix (lint + template render per env) catches issues before they hit Argo CD.
Fleet Migration Doc Scope
The MCP-SERVER-FLEET-GATEWAY-INTEGRATION.md is comprehensive but it's effectively a separate project spec (shared core module + 20 server migrations). It should probably live in a separate repo or at minimum be clearly marked as out-of-scope for the gateway implementation phases. Right now it's in the same knowledge base, which means Claude Code might pull patterns from it when implementing gateway code.
