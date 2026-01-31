# Feature design (Navarch)

Use this skill when designing a new Navarch feature **before writing code**. The user supplies a feature description (below "Feature Description:" or in the message). Execute each phase in order; use web search and codebase context as needed.

## Feature description

Locate the feature description in the user message (look for "Feature Description:" or equivalent). If missing, ask for a short description of the feature and its goals before continuing.

---

## Phase 1: Understand & clarify

- Restate the feature in your own words and confirm understanding.
- Ask clarifying questions: requirements, constraints, scope, ambiguities, unstated assumptions.
- Define success criteria: how we know the feature works correctly.

Do not proceed to Phase 2 until scope and success criteria are clear. If the user has already specified them, summarize and move on.

---

## Phase 2: Research prior art

Search and analyze how comparable systems implement similar functionality. Prioritize:

- **Kubernetes** (k8s.io, kubernetes/kubernetes)
- **Nomad** (HashiCorp)
- **Ray** (distributed AI/ML)
- **Slurm** (HPC scheduler)
- **Borg/Omega** (Google papers)
- Other systems relevant to the specific feature

For each relevant system:

- How they solved the problem; abstractions/APIs.
- Tradeoffs and reasons.
- Documented lessons and pitfalls.

Synthesize: canonical/idiomatic approach; where systems diverge and why.

---

## Phase 3: Design space exploration

- Enumerate implementation options.
- For each: complexity (impl + ops), consistency/correctness, performance, failure modes, extensibility.
- Compare "simplest thing that could work" vs "right" long-term solution.
- **Recommendation**: chosen approach with rationale.

---

## Phase 4: API & interface design

- User-facing API: CLI, config, gRPC, etc.
- Design for the 80% use case; keep the happy path simple and obvious.
- Compose with existing Navarch concepts; match codebase and tool conventions.
- Error cases: how they are surfaced to users.

---

## Phase 5: System design

- **Data model**: state to store, where, consistency requirements.
- **Component interactions**: what touches what; outline the flow.
- **Distributed concerns**: node failures, partitions, leader election; concurrency and coordination; idempotency, retries, recovery.
- **Dependencies**: what this feature depends on; what depends on it.

---

## Phase 6: Incremental delivery plan

- Break into mergeable chunks (each delivers value or is behind a flag).
- MVP that proves the core concept.
- What to defer to fast-follow.
- Testing strategy per phase.

---

## Phase 7: Test plan

- Unit tests needed.
- Integration scenarios.
- Failure injection (node crash, timeout, partial failure).
- Edge cases and boundaries.
- Load testing at scale (if applicable).

---

## Phase 8: Observability & operations

- Metrics to emit.
- Logs needed for debugging.
- How operators detect health vs failure.
- Runbook sketch: expected failure modes and remediation.

---

## Deliverables

After the design phase, provide:

### 1. Design doc summary

A shareable summary (markdown) suitable for review: problem, approach, key decisions, alternatives considered, open questions.

### 2. Recommended file/package structure

Where new code lives under the Navarch repo (e.g. `pkg/...`, `cmd/...`, `proto/...`).

### 3. Key interfaces and types (Go)

Concrete Go interfaces and main types (structs) that define the feature's contract. Idiomatic Go; align with existing `pkg/` patterns.

### 4. Implementation order with milestones

Ordered list of work items with clear milestones (e.g. "Milestone 1: core types + store", "Milestone 2: API layer", "Milestone 3: integration").

---

## Conventions

- Follow [AGENTS.md](AGENTS.md) and Google Developer Documentation Style for docs.
- Prefer idiomatic Go and production-ready choices; every line should earn its place.
- When searching prior art, cite sources (docs, repo paths, papers) so the user can dig deeper.
