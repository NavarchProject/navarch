# Update documentation (Navarch)

Navarch is an open-source multi-cloud GPU fleet management system written in Go. Use this skill when updating or creating documentation after code changes or for a new feature.

## Documentation structure

**User-facing docs** (`docs/`):
- Audience: People using Navarch to manage GPU fleets.
- Focus: Concepts, getting started, configuration, CLI reference, troubleshooting.
- Tone: Helpful, practical, task-oriented.

**Developer docs** (package READMEs, code comments, CONTRIBUTING.md):
- Audience: People contributing to Navarch.
- Focus: Architecture, design decisions, how subsystems work, how to extend.
- Tone: Technical, precise, explains the "why".

---

## Phase 1: Discover relevant docs

1. Run `find docs/ -name "*.md"` and review the doc structure.
2. Check for READMEs in relevant packages: `find . -name "README.md"` (root, cmd/*, pkg/*).
3. Review existing doc patterns: how topics are organized, heading style, list format, cross-links.
4. Identify:
   - Docs that need updating based on the changes.
   - Gaps where new docs are needed.
   - Cross-references in `docs/index.md` or other docs that might need updating.

---

## Phase 2: Research style guidelines

Before writing, fetch and review:
- Google Developer Documentation Style Guide: https://developers.google.com/style
- Anthropic docs for voice/tone: https://docs.anthropic.com

### Principles to follow

**From Google's style guide:**
- Be conversational but professional; write like you are explaining to a colleague.
- Use second person ("you") and active voice.
- Use present tense: "the server sends" not "the server will send".
- Use simple words: "use" not "utilize", "let" not "enable".
- Front-load important information; put the key point first.
- One idea per sentence; keep sentences short and scannable.
- Use consistent terminology; pick one term and stick with it.
- Document the 80% case first; common tasks before edge cases.

**From Anthropic's voice:**
- Clear and direct; no fluff or marketing speak.
- Confident but not arrogant; state things plainly.
- Technically precise; do not hand-wave.
- Respect the reader's time; get to the point.
- Show, do not just tell; concrete examples over abstract explanations.

**Navarch-specific (AGENTS.md):**
- Use sentence case for headings (not title case).
- Use active voice and present tense.
- Avoid contractions in documentation.
- Use "To [do something]:" for instructional steps.
- Use complete sentences with periods in lists.
- Avoid bold formatting for labels like "Usage:" or "Examples:".
- Avoid marketing language and future tense (e.g., "Coming soon").

### Anti-patterns to avoid

- "Simply" / "just" / "easy" (dismissive of complexity).
- "Please note that" / "It should be noted" (filler).
- Passive voice when active is clearer.
- Burying the lede in paragraphs of context.
- Walls of text without structure (use headings, lists, short paragraphs).
- Documenting implementation details in user docs.

---

## Phase 3: Write or update documentation

### User docs (`docs/`)

For each doc:

1. **Purpose**: One clear purpose per doc; match the topic to the filename (e.g., `simulator.md` for simulator usage).
2. **Structure**:
   - Short intro (what this doc covers and who it is for).
   - Main content under sentence-case headings.
   - Ordered by user workflow where applicable (e.g., get started → configure → run).
3. **Format**:
   - Sentence case for all headings.
   - "To [do X]:" for step-wise instructions.
   - Complete sentences with periods in lists.
   - Code blocks for commands, config snippets, and examples.
4. **Links**: Update `docs/index.md` if you add, rename, or remove a doc. Use relative links between docs (e.g., `[configuration](configuration.md)`).
5. **Scope**: User-facing behavior and options only; no internal APIs or implementation details unless they are necessary for troubleshooting or extending.

### Developer docs (package READMEs, code comments)

1. **Package READMEs** (e.g., `pkg/pool/README.md`):
   - What this package does and when to use it.
   - Key types and interfaces; how the package fits into the system.
   - Design decisions or non-obvious behavior (the "why").
   - Pointers to tests or examples if they illuminate usage.
2. **Code comments**:
   - Keep comments that explain why something is done a certain way.
   - Keep field/struct comments for non-obvious data structures or relationships.
   - Remove comments that only restate what the code does.
3. **CONTRIBUTING.md** (if present): Update build, test, and submission steps when relevant; keep consistent with the repo.

### Cross-references and index

- After adding or renaming a doc, add or update its entry in `docs/index.md` under the appropriate section.
- Fix broken links (e.g., moved or renamed files).
- Ensure linked anchor text is accurate (e.g., "getting started guide" → `getting-started.md`).

### Verification

- Read the doc in context: does it match the current behavior and UI?
- Confirm headings are sentence case and instructional steps use "To [do something]:".
- Confirm no contractions, no marketing language, no "simply/just/easy".
- Run any doc build or link check the project uses (if applicable).

---

## Summary checklist

- [ ] Phase 1: Discovered all relevant docs and READMEs; identified updates and gaps.
- [ ] Phase 2: Applied Google style, Anthropic voice, and AGENTS.md doc rules; avoided anti-patterns.
- [ ] Phase 3: User docs are task-oriented and correctly formatted; developer docs explain the "why"; index and cross-references are updated.
- [ ] Verification: Doc matches current behavior; style and links are consistent.
