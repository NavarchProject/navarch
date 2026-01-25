# Reframe Your Mental Model: What's Possible After Work

I built a production-grade distributed system for managing GPU fleets across three cloud providers. Multi-cloud provisioning. Health monitoring with NVML integration. Pluggable autoscaling. A 5,000-node chaos testing framework.

I did it in evenings and weekends over six weeks. Maybe 10-15 hours a week.

This isn't a brag. It's a calibration problem.

---

## The Old Mental Model

Before AI coding assistants, I'd estimate projects like this in months:

- **Week 1-2**: Scaffold the project, set up build system, CI
- **Week 3-4**: Define protos, implement basic RPC server
- **Week 5-8**: Build provider integrations (just GCP alone would be 2 weeks)
- **Week 9-10**: Node agent with health monitoring
- **Week 11-12**: Autoscaling logic
- **Week 13-16**: Testing infrastructure (if we even got to it)

Four months for a senior engineer working full-time. For a side project? A year, maybe never finished.

---

## What Actually Happened

| Phase | Old Estimate | Actual Time |
|-------|--------------|-------------|
| Project scaffold, protos, RPC server | 3-4 weeks | 3 evenings |
| Multi-cloud providers (GCP, AWS, Lambda) | 4-6 weeks | 1 week |
| Node agent with NVML, XID detection | 2-3 weeks | 4 days |
| Autoscaling system (5 strategies) | 2-3 weeks | 1 week |
| Chaos testing framework | 3-4 weeks | 1 week |
| HTML reporting with charts | 1 week | 1 evening |

I'm not 10x faster. The work is different.

---

## What Changed

**I stopped implementing. I started directing.**

My job became:
1. Define the critical user journeys
2. Design the data models
3. Write the proto definitions
4. Describe what each component should do
5. Review what Claude produced
6. Iterate on the parts that needed refinement

The architecture decisions—what to build, how data flows, what interfaces exist—still took real thought. That part wasn't faster.

But the translation from "I know what this should do" to "working code that does it"? That collapsed from days to hours.

---

## The Calibration Problem

Here's what I'm struggling with now: **my intuition about project scope is wrong.**

When someone asks "how long would it take to build X?" my brain still runs the old calculation. Weeks of implementation work. Boilerplate. Integration headaches. Testing infrastructure we'll probably skip.

But that's not the world I'm building in anymore.

The limiting factor isn't implementation time. It's:
- How clearly can I articulate what I want?
- How well do I understand the problem domain?
- How good is my judgment about architecture?

Those skills didn't get faster. But they're now the *only* bottleneck.

---

## What This Means

**Side projects are viable again.** That system you've been meaning to build for three years? It's a month of evenings now, not a sabbatical.

**Prototypes can be production-grade.** The excuse "it's just a prototype" made sense when real implementation was expensive. Now you can build it right the first time.

**Reliability engineering is affordable.** I built a 5,000-node chaos simulator because I had the bandwidth. Previously, I'd have shipped without it and hoped for the best.

**Your estimates are probably wrong.** If you're still planning projects with pre-AI timelines, you're either undercommitting or leaving value on the table.

---

## The Catch

This only works if you actually know what you're building.

AI didn't help me figure out which XID error codes matter. It didn't tell me that NVLink failures cascade across GPUs. It didn't design the autoscaler interface.

The "thinking" work is the same. The "typing" work largely disappeared.

---

## Rethinking Quality Assurance

Here's where I'll lose some of you: **stop reviewing the code line by line.**

Instead, deeply understand the system itself. The architecture. The critical user journeys. The data models. How information flows between components. What can fail and how.

If I've clearly specified what a component should do—its inputs, outputs, invariants, error cases—Claude can implement it correctly. I don't need to verify the syntax. I need to verify the behavior.

So instead of reading code, I ask: what tests or evaluations would give me confidence this works? Then I build those too.

**Here's the thing about tests**: unit tests only catch bugs you thought to write tests for. They verify the behavior you specified, not the behavior you forgot to specify.

That's why integration tests, load tests, and chaos tests matter. They find the bugs you didn't anticipate: the cascading failure that only happens under specific timing, the race condition that only appears at scale, the edge case in your cloud provider's SDK that nobody documented.

Before AI, comprehensive testing was hard to justify. Weeks of engineering effort for something that *might* find bugs. It was never in the budget. Always "nice to have" that got cut.

Now I can ask Claude to build the chaos testing framework too. The comprehensive testing that was economically impossible is suddenly viable.

**And here's the meta point**: it was never sustainable to rely on "cracked developers writing perfect PRs."

We've been telling ourselves a story that if we just review carefully enough, we'll catch the bugs. But we don't. We never did. There are always cases where we fundamentally don't understand how something works until it breaks.

You were never going to read every line of every dependency. You were never going to understand every edge case in your cloud provider's SDK. You were already trusting code you didn't write and didn't review.

AI-generated code isn't a new category of risk. It's the same risk you've always had, with a different author.

The question isn't "is this code bug-free?" It's "do I understand this system well enough to diagnose and fix issues when they appear?"

When something breaks, my understanding of the architecture tells me where to look. Then I either fix it myself or describe the problem to Claude. Either way, system knowledge matters more than having read every line.

---

## The Fun Part

Here's what I've realized: **the fun part of engineering isn't writing every line of code.**

It's not debugging why a goroutine isn't canceling properly. It's not wrestling with SDK authentication flows. It's not writing the fifteenth REST endpoint that looks like the last fourteen.

The fun part is:
- What are the data models?
- What are the interfaces?
- What are the components and what responsibilities do they own?
- How does state flow through the system?
- What are the failure modes and how do we handle them?

*That's* the interesting work. The architectural decisions. The system design. The "why" and "what," not the "how do I express this in Go."

For years, the interesting work was buried under implementation. You'd spend 20% of your time on the fun stuff and 80% translating it into code. Now that ratio is flipping.

I get to spend most of my time on data models, interfaces, component boundaries, and state flow. The parts I actually enjoy. The parts that actually matter.

AI didn't just make me faster. It gave me back the parts of engineering I became an engineer for.

---

## The Real Question

If you're a senior engineer who's been avoiding AI tools because "I can code faster than I can prompt"—you might be right for small tasks. You're definitely wrong for large systems.

The question isn't whether AI makes you faster at coding. It's whether you have time to build the things you actually want to build.

I didn't have time for a multi-cloud GPU fleet manager with chaos testing. Now I do.

Recalibrate accordingly.

---

*Building Navarch: a GPU fleet management system. More at [link to blog series].*
