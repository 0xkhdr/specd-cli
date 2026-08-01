# specd documentation

specd is a spec-driven coding harness. You plan a change in Markdown, a human
approves it, and then an agent executes it one task at a time against recorded
evidence — with the process enforced by a local binary instead of by the agent's
memory.

> The agent reasons. The harness enforces.

New here? Read [Getting started](getting-started.md) — one change from `init` to
`archive`, every command run for real. Then read
[Approval and evidence](approval-and-evidence.md), because it covers the one
thing everybody gets wrong at first.

## What trips people up

**A passing `verify` does not complete a task.** Verification records an
observation; `complete` is a separate transition that decides whether the
evidence applies. Nearly every "why won't it let me…" question is a version of
this one. [Approval and evidence](approval-and-evidence.md) explains it and the
second half of the same rule: an agent cannot approve its own plan.

## Pick your path

**I want to see it work.** [Getting started](getting-started.md). Bring a Git
repository with one commit; you'll have an archived change in twenty minutes.

**I want to understand the model before I commit to it.**
[Concepts](concepts.md) — root, spec, change, the four artifacts, the lifecycle,
and why activity, readiness, evidence, and approval are four separate things.

**I'm implementing tasks day to day.** [The loop](the-loop.md) — the frontier
and waves, bounded context, declared scope, the attempt, what each refusal is
protecting.

**I'm wiring up an AI agent.** [Driving specd from an agent](agent-setup.md) —
the JSON envelope, `next.kind` as control flow, the generated `AGENTS.md`, and
what `advisory` assurance honestly means.

**Something refused me.** [Troubleshooting](troubleshooting.md) — refusal codes
by family, what each means, and the one legal next action.

**I'm changing specd itself.** [Contributing](contributing.md) — the checks, the
non-negotiable rules, and the recipe for adding an operation without creating
drift.

**I need a flag or an exit code.** [Operations](operations.md), generated from
the operation registry and byte-checked against it.

**I want to know what's on disk.** [Layout](layout.md) — the `.specd/` tree,
who owns each file, and what never to hand-edit.

## Every page

| page | what it gives you |
| --- | --- |
| [Getting started](getting-started.md) | one change end to end, with real output |
| [Concepts](concepts.md) | the model and its vocabulary |
| [The loop](the-loop.md) | `next` → `context` → `start` → `verify` → `complete`, in depth |
| [Approval and evidence](approval-and-evidence.md) | the two guarantees with no bypass |
| [Driving specd from an agent](agent-setup.md) | JSON envelope, palette, guidance file, host assurance |
| [Layout](layout.md) | the `.specd/` on-disk format |
| [Troubleshooting](troubleshooting.md) | refusal codes and recoveries |
| [Operations](operations.md) | generated command reference |
| [Contributing](contributing.md) | build, test, the release gates, how to add an operation |

## The thirty-second version

```bash
specd init                              # adopt the project
specd new add-dark-mode                 # scaffold the change
#   author proposal.md, design.md, tasks.md, specs/<capability>/spec.md
specd check add-dark-mode               # gates must be green
specd approve add-dark-mode             # ← human, real terminal
specd next add-dark-mode                # what's ready
specd context add-dark-mode T1          # bounded read input
specd start add-dark-mode T1 --revision 2
#   edit only the files the task declared
specd verify add-dark-mode T1 <attempt> # record evidence
specd complete add-dark-mode T1 --revision 3
specd sync add-dark-mode                # ← human, accepts the behavior
specd archive add-dark-mode
```

Two of those steps are human and cannot be run by an agent. That is the design,
not a limitation to work around.

## Outside these docs

- [`../README.md`](../README.md) — what specd is, how to build it, project status
- [`../AGENTS.md`](../AGENTS.md) — the contributor and agent guide for this repo
- [`../release/release-decision.md`](../release/release-decision.md) — what has
  been proven, what has not, and the current release call
- [`../release/surface-inventory.md`](../release/surface-inventory.md) — every
  surface mapped to the journey or invariant that owns it

## A note on status

specd is released and young — `0.x` means the surface may break on any minor
bump. The loop is proven end to end by fourteen replayed journeys, and by one
real change in one root on linux/amd64.
[`release-decision.md`](../release/release-decision.md) states exactly what that
does and does not support. These docs describe what the code does today, not a
roadmap.

Found something here that's wrong, stale, or confusing? That's a bug. The
generated pages are checked mechanically; the hand-written ones are only as
accurate as the last person who read them carefully.
