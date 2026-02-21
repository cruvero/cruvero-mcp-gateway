Review the current git diff against project conventions and optionally draft a PR description.

## Instructions

1. Get the diff to review:
   - Default: `git diff` (unstaged) + `git diff --cached` (staged)
   - If `$ARGUMENTS` contains a branch name: `git diff <branch>...HEAD`

2. Review the diff against these criteria:

   ### Code Quality
   - [ ] All exported functions/types have doc comments
   - [ ] Error messages use lowercase, no trailing punctuation (Go convention)
   - [ ] Errors are wrapped with context: `fmt.Errorf("verb noun: %w", err)`
   - [ ] No hardcoded secrets, URLs, or credentials
   - [ ] No `TODO` or `FIXME` without an issue number

   ### Security
   - [ ] No SQL injection (parameterized queries only)
   - [ ] No command injection
   - [ ] Input validation at system boundaries
   - [ ] TLS/mTLS paths handled correctly

   ### Project Conventions
   - [ ] **No mentions of AI, Claude, LLM, Copilot, or any AI tool** in code, comments, or commit messages
   - [ ] Conventional commit style in any new commits
   - [ ] Table-driven tests for new test functions
   - [ ] `t.Setenv()` instead of `os.Setenv()` in tests
   - [ ] Build tags for integration/security/load tests

   ### Migrations
   - [ ] Up and down migrations are inverse operations
   - [ ] Migration number follows sequence (no gaps, no conflicts)
   - [ ] Down migration is safe (won't destroy production data carelessly)

3. Output a summary with pass/fail per category and specific line references for issues.

## PR Mode

If `$ARGUMENTS` contains `pr`:

1. Run the full review above.
2. If no blocking issues, draft a PR description:

```markdown
## Summary
- <bullet points describing the change>

## Changes
- `path/to/file.go`: <what changed and why>

## Test Plan
- [ ] Unit tests pass (`go test -race ./...`)
- [ ] Quality gates pass (`make quality`)
- [ ] <any additional testing specific to this change>

Closes #<issue-number>
```

3. Present the draft for user approval before creating the PR.

## Constraints

- Flag any AI/Claude/LLM mentions as **blocking** — these must be removed before PR.
- Be specific: reference file:line for every issue found.
