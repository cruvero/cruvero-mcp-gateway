Load and execute a phase specification from the project roadmap.

## Instructions

1. Read `docs/phases/INDEX-UPDATED.md` to get the full phase list and status.
2. If the user provided a phase number (e.g., `10A`), read `docs/phases/PHASE$ARGUMENTS.md` for the spec and `docs/phases/PHASE$ARGUMENTS-PROMPT.md` for the prompts.
3. If no argument was given, display the phase index table and ask which phase/sub-phase to work on.
4. Summarize the phase scope in 3-5 bullet points:
   - What it adds/changes
   - Key files/packages affected
   - Dependencies on prior phases
   - Testing requirements
5. List all prompt sections from the `-PROMPT.md` file as a numbered menu.
6. Ask the user which prompt section to execute.
7. Execute the selected prompt, following all project conventions from `CLAUDE.md`.

## Constraints

- Always read the spec before executing any prompt — context matters.
- If the phase depends on incomplete prior phases, warn the user.
- Never mention AI, Claude, or LLM in any generated code, commits, or comments.
