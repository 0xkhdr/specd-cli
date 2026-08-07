# Getting started

This guide takes one small change from `init` through `archive`. The commands
were run against the current binary; output is shortened only where omitted
fields do not affect the next action.

## Before you start

You need Go 1.26 or newer, the `specd` binary from
[Install](../README.md#install), and a Git repository with at least one commit.

The repository's working tree must be **clean**: `start` refuses tracked changes
and untracked files, because scope enforcement would otherwise attribute them to
your attempt. Files your `.gitignore` excludes are exempt at both `start` and
`verify`, so dependency and build directories can stay where they are.

For the model behind any of this, read [concepts.md](concepts.md). For flags and
exit codes, read [operations.md](operations.md).

## Example project

A tiny Go module with one package that can greet:

```go
// greet/greet.go
package greet

func Greet(name string) string { return "Hello, " + name }
```

Committed, clean tree. The change we want: add a farewell.

## 1. Adopt the root

```console
$ specd init
operation: init
ok: true
root: /home/you/demo
guidance: /home/you/demo/AGENTS.md
next: blocked owner=author; run specd new <change> to plan the first change
exit: 0 success
```

That created `.specd/` and installed `AGENTS.md`, the instruction file an AI
agent resumes from. Both are meant to be committed — and `start` later requires
a clean worktree, so commit them before you begin implementing.

Note the `next:` line — every operation ends with one,
and it is the whole navigation system. When you don't know what to do, do what
`next` says.

## 2. Create the change

```console
$ specd new add-farewell --capability greeting
operation: new
ok: true
change: add-farewell
revision: 1
stage: planning
next: operation check; run specd check add-farewell
```

`--capability greeting` names the capability the delta spec is authored under.
Omit it and the change name is used. You now have four scaffolded artifacts:

```text
.specd/changes/add-farewell/
  proposal.md
  design.md
  tasks.md
  specs/greeting/spec.md
  state.json          ← harness-owned, not yours
```

## 3. Inspect the scaffold

```console
$ specd check add-farewell
ok: false
finding_count: 16
error proposal: …/proposal.md:4 Problem section contains scaffold placeholder text; fix: replace placeholder with concrete content
error deltas: …/specs/greeting/spec.md:6 ADDED requirement contains scaffold placeholder text; fix: replace placeholders with concrete behavior
error tasks: …/tasks.md:3 task verification command contains placeholder text; fix: replace it with one exact verification command
next: blocked owner=author; repair the reported findings and run specd check add-farewell
exit: 1 failure
```

The scaffold intentionally fails validation until every placeholder is
replaced. A template is not an approved plan.

Every finding is `file:line`, what is wrong, and `fix:` what to do.

## 4. Author the four artifacts

**`proposal.md`** — five required sections:

```markdown
# Proposal

## Problem
The greet package can open a conversation but cannot close one.

## Outcome
Callers can produce a farewell for a name, in the same shape as the greeting.

## Scope
The greeting capability in package greet.

## Non-goals
Localization, punctuation options, and templating.

## Affected capabilities
greeting
```

**`specs/greeting/spec.md`** — the delta. Behavior only, at least one scenario:

```markdown
## Purpose
Produce fixed conversational phrases for a named person.

## ADDED Requirements

### Requirement: Farewell phrase
The system MUST return a farewell addressed to the supplied name.

#### Scenario: Named farewell
- **WHEN** a caller requests a farewell for the name world
- **THEN** the result is the string Goodbye, world
```

**`design.md`** — seven required sections. The first one cites the requirement,
which is how the design is checked against the delta instead of trusted:

```markdown
# Design

## Boundaries
greeting/Requirement: Farewell phrase
One exported function in package greet; no other package changes.

## Interfaces
Farewell(name string) string, mirroring Greet.

## Invariants
The returned string always begins with Goodbye and always ends with the supplied name.

## Failure behavior
An empty name returns the prefix with an empty suffix; the function never panics.

## Integration
Package greet is the existing owner of conversational phrases.

## Alternatives
A phrase table was rejected because two phrases do not need indirection.

## Owner
greet
```

**`tasks.md`** — one table, seven columns:

```markdown
| id | role | files | depends-on | refs | verify | acceptance |
|---|---|---|---|---|---|---|
| T1 | builder | greet/greet.go; greet/greet_test.go | | greeting/Requirement: Farewell phrase | `go test ./greet` | Farewell returns Goodbye, world for the name world and the package tests pass |
```

> **Lists are semicolon-separated.** `files` and `depends-on` split on `;`, not
> on commas. Write `a.go, b.go` and you have declared one file literally named
> `a.go, b.go` — `check` and `start` both accept it, and `verify` then refuses
> your two real files as out of scope. Costs ten minutes the first time.

Now check passes:

```console
$ specd check add-farewell
ok: true
finding_count: 0
next: human_handoff owner=human reason=approval is human-only; ask a human to run specd approve add-farewell in a human terminal
exit: 0 success
```

## 5. The first human gate

```console
$ specd approve add-farewell --reason "plan reviewed"
approve add-farewell in this terminal? [y/N]: y
ok: true
```

Approval requires a non-empty `--reason`. Two other details matter.

**This must be a real terminal.** The human route is derived from a termios
ioctl on stdin. An agent, a pipe, a CI job, or an editor shell without a tty all
derive `agent`, and get:

```text
error human_approval_required: approve is human-only and cannot use an agent-capable route
next: human_handoff owner=human reason=approval is human-only; hand off approval to a human terminal
exit: 2 refusal
```

That is the design, not an obstacle to route around. There is no bypass flag.

**`--approver` must match your trusted identity**, which is git `user.email` (or
`SPECD_APPROVER`). Claiming anything else:

```text
error approval_identity: claimed approver differs from trusted identity; fix: remove or correct the approver claim
```

Simplest fix: omit `--approver`. It is derived either way.

Approval hashes the artifacts as they are right now. Edit one byte afterwards
and the approval goes stale — you will be sent back here.

## 6. Find the work

```console
$ specd status add-farewell
revision: 2
approval_current: true
stage: approved
frontier: T1
pending: 1

$ specd next add-farewell
classification: frontier
frontier: T1
next: operation context; run specd next add-farewell <task> to select one, then specd context add-farewell <task>
```

`frontier` is every task whose dependencies are satisfied — what could be worked
now, in parallel, not a single "current" task.

## 7. Bounded context

```console
$ specd context add-farewell T1
allowed_write_paths: greet/greet.go,greet/greet_test.go
approval_hash: 2b99e813…
item_count: 5
omission_count: 0
required_bytes: 1017
role: builder
next: operation start; run specd start add-farewell T1 --revision 2
```

This is the read input for exactly one task — the requirement it implements, its
design boundary, its declared files. Not the whole repository. `--budget-bytes`
caps it; optional items are dropped first and counted in `omission_count`, and
if the *required* inputs alone bust the budget, it refuses rather than silently
truncating what you were supposed to read.

## 8. Open the attempt

```console
$ specd start add-farewell T1 --revision 2
revision: 3
attempt: 569afad4…
baseline_head: 4def2ffb…
declared_files: greet/greet.go,greet/greet_test.go
next: operation verify; run specd verify add-farewell T1 569afad4…
```

`--revision 2` is the revision you just observed in `status`. If the change had
moved underneath you, this refuses instead of acting on a stale view.

`start` pins a Git baseline. From here, your diff against that baseline must
stay inside `declared_files`.

## 9. Write the code

```go
func Farewell(name string) string { return "Goodbye, " + name }
```

plus a test, in the two declared files. Nothing else.

## 10. Verify

```console
$ specd verify add-farewell T1 569afad4…
ok: true
exit_code: 0
head: 4def2ffb…
non_vacuous: true
passed: true
zero_match: false
record_id: f02039e9…
next: operation complete; run specd complete add-farewell T1 --revision 3
```

`go test ./greet` ran in a bounded process — no shell, a timeout, an output
limit — and the result was appended to `.specd/evidence.jsonl`, pinned to the
HEAD it observed.

`non_vacuous: true` and `zero_match: false` matter: a test command that matched
zero tests exits 0 while proving nothing, and that does not complete anything.

This did **not** complete the task. Verification is an observation.

## 11. Complete

```console
$ specd complete add-farewell T1 --revision 3
revision: 4
evidence: f02039e9…
next: operation next; run specd next add-farewell

$ specd status add-farewell
revision: 4
all_tasks_complete: true
completed: 1
next: terminal; all task work is complete
```

`complete` re-checked HEAD, found evidence applicable to this task contract and
this command at this commit, and closed the task inside one guarded transaction.

With more tasks you would loop back to step 6 here.

## 12. The second human gate

Accepting the plan and accepting the resulting behavior are two different
authorizations. Back to a real terminal:

```console
$ specd sync add-farewell --reason "behavior accepted"
```

An agent gets refused exactly like before:

```text
error human_operation_required: sync is human-only and cannot use an agent-capable route
```

`sync` applies the delta into accepted truth as one transaction, and
`.specd/specs/greeting/spec.md` now exists:

```markdown
# Greeting

## Purpose

Produce fixed conversational phrases for a named person.

### Requirement: Farewell phrase
The system MUST return a farewell addressed to the supplied name.

#### Scenario: Named farewell
- **WHEN** a caller requests a farewell for the name world
- **THEN** the result is the string Goodbye, world
```

That file is now what the greeting capability *does*. The next change to it
ships a delta against this.

## 13. Archive

```console
$ specd archive add-farewell
revision: 6
accepted: greeting
approver: d@e.f
source: changes/add-farewell
target: archive/2026-07-31-add-farewell
next: terminal; the change is archived at archive/2026-07-31-add-farewell; no further action is required
```

Final state:

```text
AGENTS.md                                     generated agent guidance
.specd/
  specs/greeting/spec.md                      accepted truth
  archive/2026-07-31-add-farewell/            the whole change, frozen
  history.jsonl                               every transition
  evidence.jsonl                              every verification
```

`.specd/changes/` is empty — the change moved wholesale into `archive/`.

Commit all of it with your code. The plan, its approval, the evidence, and the
accepted spec review in the same diff as the implementation.

## The loop, condensed

```text
init → new → author → check → [human] approve
→ next → context → start → edit → verify → complete   (repeat per task)
→ [human] sync → archive
```

Read `next:` after every command. It is never wrong about what comes next, and
when it refuses it gives you exactly one legal action — not a list of options.

## Common first-run problems

Common mistakes from this walkthrough:

1. **Commas in `files`.** Semicolons. See step 4.
2. **`--approver` with a name instead of the git email.** Omit the flag.
3. **Trying to approve from a non-terminal.** Both human gates need a real tty.
   If your agent is stuck, it is supposed to be — hand off.
4. **A dirty working tree.** `start` refuses tracked changes and untracked
   files that are not yours to attribute. Commit or clean before `start`.
   Git-ignored paths are exempt at both `start` and `verify`.

Next: read [The execution loop](the-loop.md) for task-level detail, or
[Troubleshooting](troubleshooting.md) when a refusal differs from this example.
