# Navarch Blog Voice Guide

This guide defines the writing style for Navarch blog posts, combining principles from the Google Developer Writing Style Guide with Anthropic's communication style.

## Core Principles

### 1. Clarity Above All

Write to be understood on first reading. Every sentence should serve a clear purpose.

- **Be direct.** State your main point early. Don't bury the lead.
- **Use simple words.** Prefer "use" over "utilize," "start" over "initialize."
- **Keep sentences short.** Aim for 20-25 words maximum. Break complex ideas into multiple sentences.

### 2. Active Voice and Present Tense

Write as if describing actions happening now.

| Avoid | Prefer |
|-------|--------|
| "The nodes were provisioned by the system" | "The system provisions nodes" |
| "Errors will be detected" | "The agent detects errors" |
| "The test was run" | "We run the test" |

### 3. Address the Reader Directly

Use "you" to speak to readers and "we" when describing our work or decisions.

- **You** when giving instructions: "You can run the simulator with..."
- **We** when describing our process: "We designed the chaos engine to..."
- **Avoid** passive constructions like "it is recommended" — instead say "we recommend" or "you should"

### 4. Honesty and Precision

Following Anthropic's commitment to truthfulness:

- **Acknowledge limitations.** If something doesn't work perfectly, say so.
- **Be specific about uncertainty.** Use "approximately," "in our experience," or "typically" rather than absolute claims.
- **Avoid hype.** Let the work speak for itself. Don't oversell outcomes.
- **Show evidence.** Back up claims with data, examples, or concrete results.

### 5. Technical Depth Without Gatekeeping

Make complex topics accessible without dumbing them down.

- **Define terms on first use.** "XID errors (GPU hardware fault codes logged by the NVIDIA driver)..."
- **Use examples liberally.** Show, don't just tell.
- **Layer complexity.** Start simple, then add nuance. Readers can stop when they have enough.
- **Respect the reader.** Assume intelligence but not specific domain knowledge.

## Structure Guidelines

### Opening

Start with context and stakes. Why should the reader care? What problem does this solve?

```markdown
Good: "Running 5,000 GPU nodes across three cloud providers means something will fail
every few minutes. We needed a way to test our system's resilience without spending
$50,000 on cloud compute."

Avoid: "In this blog post, we will discuss our approach to testing distributed systems."
```

### Body

- Use headers to create scannable structure
- Include code examples with context
- Add diagrams for complex architectures
- Break up walls of text with lists when appropriate

### Code Examples

- Keep examples minimal but complete
- Include enough context to understand the example
- Explain what the code does, not line-by-line

```yaml
# Good: focused example with context
chaos:
  failure_rate_per_minute_per_1000_nodes: 10
  cascading:
    enabled: true
    probability: 0.15  # 15% of failures trigger cascades
```

### Closing

End with practical takeaways. What can the reader do with this information?

## Tone

### What We Are

- **Confident but humble.** We're proud of our work but acknowledge we're still learning.
- **Technical but approachable.** We write for engineers, not marketing.
- **Direct but not terse.** Clear doesn't mean cold.
- **Curious.** We share what we learned, including surprises.

### What We Avoid

- **Marketing speak.** No "revolutionary," "game-changing," or "best-in-class."
- **Unnecessary hedging.** Don't say "we believe" when you can state a fact.
- **Jargon without explanation.** If you must use specialized terms, define them.
- **Excessive self-congratulation.** Share results; let readers draw conclusions.

## AI-Assisted Development Context

When writing about using AI tools like Claude Code:

- **Be specific about what the AI did.** "Claude generated the initial chaos engine structure" rather than vague claims.
- **Acknowledge human judgment.** The human decides what to build; the AI helps build it.
- **Share the collaboration pattern.** How did human and AI work together?
- **Include both successes and iterations.** What worked well? What needed refinement?

## Formatting Conventions

### Headers

Use sentence case: "How we built the simulator" not "How We Built The Simulator"

### Lists

- Use bullets for unordered items
- Use numbers only for sequential steps or ranked items
- Keep list items parallel in structure

### Emphasis

- **Bold** for key terms on first definition
- *Italics* sparingly for emphasis
- `Code formatting` for commands, file names, and technical terms

### Numbers

- Spell out one through nine
- Use numerals for 10 and above
- Always use numerals with units: "8 GPUs," "5 minutes"

## Example Transformations

### Before (vague, passive, hypey)

> Our revolutionary testing framework was developed to enable unprecedented levels of chaos engineering at scale. It is believed that this approach will transform how distributed systems are validated.

### After (specific, active, honest)

> We built a chaos testing framework that simulates failures across 5,000 nodes. In our tests, it detected three edge cases we'd missed in smaller-scale testing—including a cascading failure pattern that only appeared under specific timing conditions.

---

## Checklist Before Publishing

- [ ] Does the opening explain why this matters?
- [ ] Are all technical terms defined on first use?
- [ ] Is every claim backed by evidence or qualified with uncertainty?
- [ ] Can a reader scan the headers and understand the structure?
- [ ] Are code examples minimal and explained?
- [ ] Does the closing give the reader something actionable?
- [ ] Have we avoided marketing language?
- [ ] Is the human-AI collaboration described honestly?
