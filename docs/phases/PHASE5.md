# Phase 5: Policy & Rate Limiting

## Goal

Implement per-client rate limiting using token buckets, tool safety guardrails with allowlist/denylist enforcement, and comprehensive audit logging for all policy decisions.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [5A](PHASE5A.md) | Rate Limiting Engine | [3 prompts](PHASE5A-PROMPT.md) | ratelimit |
| [5B](PHASE5B.md) | Tool Safety Guardrails & Audit Logging | [3 prompts](PHASE5B-PROMPT.md) | policy |

## Dependencies

- Phase 1 (Core Foundation) -- types, config, store (audit_log table)
- Phase 2 (Identity & Auth) -- identity context for per-client keying

## Deliverables

- Token bucket rate limiter per (client_identity, route)
- Configurable rates per policy profile (default, premium, admin)
- Policy profiles sourced from config/Cruvero sync in this baseline (no dedicated `policy_profiles` table in these phases)
- Rate limit headers in responses (X-RateLimit-*)
- Background cleanup of expired limiters
- Tool allowlist/denylist per policy profile
- Dangerous command pattern detection
- Argument validation against tool input schemas
- Audit logging for all policy decisions
- Configurable enforcement mode: enforce vs audit

## Packages Created

- `internal/ratelimit` -- Token bucket rate limiting, per-identity tracking
- `internal/policy` -- Tool safety evaluation, allowlist/denylist, audit logging

## Success Criteria

- Rate limiting enforced per client identity with correct token bucket behavior
- Rate limit headers present in all responses
- Tool safety blocks dangerous patterns in enforce mode
- Tool safety logs but allows in audit mode
- All policy decisions recorded in audit_log
- Expired limiters cleaned up without memory leaks
- >=80% test coverage
