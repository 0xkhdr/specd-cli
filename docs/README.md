# Documentation

specd plans a change in Markdown, requires human approval, and then gives an
agent one bounded task at a time. Use this page to choose the shortest reading
path for your goal.

## Evaluate specd

Start with:

1. [Getting started](getting-started.md) — run one change from `init` through
   `archive`.
2. [Concepts](concepts.md) — understand root, spec, change, approval, evidence,
   and completion.
3. [Release decision](../release/release-decision.md),
   [gate limits](../release/gate-limits.md), and
   [Security](../SECURITY.md) — decide whether the current platform and
   assurance boundary fit your use.
4. [Architecture](../ARCHITECTURE.md) — see how the code enforces that boundary.

Current boundary: the base loop is released and proven end to end on
linux/amd64. The production profile remains experimental. Host scope assurance
is advisory unless the host supplies containment. These levels are projected
from the typed maturity registry by `report --kind status` and checked here by
release qualification.

## Use specd day to day

1. [The execution loop](the-loop.md) — `next`, `context`, `start`, `verify`,
   and `complete`.
2. [Approval and evidence](approval-and-evidence.md) — the two human gates and
   why passing evidence is not completion.
3. [Troubleshooting](troubleshooting.md) — refusal codes and legal recovery.
4. [Layout](layout.md) — what lives under `.specd/` and who may edit it.

For exact syntax, use the generated [operation reference](operations.md).

## Integrate an agent

Read [Driving specd from an agent](agent-setup.md). It defines the JSON
envelope, `next.kind` control flow, generated `AGENTS.md`, operation palette,
and host-assurance boundary.

An integration must stop at `human_handoff`, preserve revision guards, edit
only declared files, and treat `verify` as an observation rather than task
completion.

## Contribute to specd

Read the repository [agent guide](../AGENTS.md), then
[Architecture](../ARCHITECTURE.md), then [Contributing](contributing.md). They
cover the binding design rules, code shape, narrow and
full checks, generated files, operation registry, release gates, and surface
ownership.

Repository policy and history:

- [Security policy](../SECURITY.md)
- [Changelog](../CHANGELOG.md)
- [Release decision](../release/release-decision.md)
- [Surface inventory](../release/surface-inventory.md)

## Documentation ownership

| subject | owner |
| --- | --- |
| Product orientation and installation | [root README](../README.md) |
| Reading paths | this page |
| First complete workflow | [Getting started](getting-started.md) |
| Model and vocabulary | [Concepts](concepts.md) |
| Task execution | [The execution loop](the-loop.md) |
| Approval and evidence rules | [Approval and evidence](approval-and-evidence.md) |
| Agent contract | [Driving specd from an agent](agent-setup.md) |
| Managed files | [Layout](layout.md) |
| Refusal recovery | [Troubleshooting](troubleshooting.md) |
| Commands and flags | generated [Operations](operations.md) |
| Codebase changes | [Contributing](contributing.md) |
| Code shape and layering | [Architecture](../ARCHITECTURE.md) |

If documentation disagrees with source, source wins and the documentation is a
bug. Do not hand-edit `operations.md`; regenerate it from the operation
registry.
