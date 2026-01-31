# Review uncommitted changes

## Workflow

1. Run `git status` to see the full picture (staged, unstaged, untracked).
2. Run `git diff` for staged changes and `git diff` (no `--staged`) for unstaged changes. Review both.
3. For each changed file, evaluate using the checklists below.
4. Run the project's existing tests (e.g. `go test ./...` or project equivalent) and fix any failures.
5. Deliver the output in the format specified at the end.

## Per-file evaluation

### Code quality and best practices

- **Naming**: Variables, functions, and types are clear and self-documenting.
- **Design**: Functions have a single responsibility; length and inputs/outputs are appropriate.
- **Error handling**: Errors are handled gracefully; edge cases are covered.
- **Duplication**: No unnecessary duplication; DRY where it makes sense.
- **Abstraction**: Code is at the right level of abstraction.
- **Idioms**: Follows language and project conventions.

### Readability and maintainability

- **Understandability**: A new team member could follow the code without heavy context.
- **Comments**: Comments explain *why*, not *what*.
- **Control flow**: Easy to follow; no overly clever logic that should be simplified.

### LLM-generated code patterns

Flag and fix common signs of AI-generated "slop":

- **Excessive comments**: Comments that restate what the code already shows (e.g., `// Create a new user` above `user := NewUser()`).
- **Unnecessary blank lines**: Extra vertical whitespace that does not improve readability.
- **Verbose phrasing**: Comments starting with "This function...", "Here we...", "Now we...", or "The following code...".
- **Redundant documentation**: Doc comments that repeat the function signature in prose.
- **Over-explained obvious code**: Inline comments on trivial operations like incrementing counters or nil checks.
- **Placeholder patterns**: TODO comments without actionable context, or boilerplate explanations left in.
- **Defensive over-logging**: Excessive debug/info logs that add noise without operational value.
- **Unnecessary helper variables**: Variables used once immediately after assignment where inlining is clearer.

Every line of code must earn its place. Remove anything that does not directly contribute to functionality or genuine clarity.

### Testing and verification

- Run existing tests; ensure nothing is broken.
- Identify new tests needed for the changes.
- Call out edge cases to test: empty inputs, boundaries, error paths, concurrency if relevant.
- If tests are missing for changed behavior, write them.

### Potential bugs and issues

- Race conditions; nil/null handling; off-by-one errors.
- Resource leaks (unclosed connections, file handles).
- Security: injection, auth, sensitive data exposure.

## Output format

Provide three sections:

### 1. Summary

- **Blocking**: Issues that must be fixed before merge/commit (correctness, security, test failures, egregious LLM slop).
- **Nice-to-have**: Improvements that are recommended but not blocking (minor cleanup, style).

### 2. Code suggestions

For each suggestion, give a short rationale and **before** / **after** code snippets (or file:line references plus the change).

### 3. Tests

- List tests you added (with file/name).
- List tests you recommend adding, with a brief description of what they should cover.
