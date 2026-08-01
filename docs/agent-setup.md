# Driving specd from an agent

specd is built to be driven by an AI coding agent, with a human at two gates.
This page is the contract between them: the JSON envelope, the generated
guidance file, the operation palette, and what the host is and is not trusted to
do.

Read [approval-and-evidence.md](approval-and-evidence.md) first if you haven't.
The rules there are the ones an agent most often tries to route around.

## The JSON envelope

Every operation accepts `--json` and emits one envelope, schema
`specd.agent/v1`:

```json
{
  "schema": "specd.agent/v1",
  "ok": true,
  "operation": "new",
  "root": { "path": "/home/you/demo" },
  "subject": { "change": "sample-change" },
  "state": { "revision": 1 },
  "data": { "condition": "active", "stage": "planning" },
  "next": {
    "kind": "operation",
    "operation": "check",
    "arguments": { "change": "sample-change" },
    "instruction": "run specd check sample-change"
  },
  "exit": { "code": 0, "class": "success" }
}
```

Fields:

| field | always present | what it carries |
| --- | --- | --- |
| `schema` | yes | envelope version; branch on this, not on shape |
| `ok` | yes | whether the operation succeeded |
| `operation` | yes | the operation id you invoked |
| `root` | when a root was resolved | the selected project path |
| `subject` | when scoped | `change`, and `task` when task-scoped |
| `state` | when state was read | the revision you observed — pass it back to `start`/`complete` |
| `data` | varies | operation-specific facts, flat, snake_case keys |
| `diagnostics` | on findings/refusals | `code`, `severity`, `message`, `path`, `line`, `fix` |
| `next` | **yes** | what to do next |
| `exit` | yes | `code` (0/1/2) and `class` (`success`/`failure`/`refusal`) |

### `next` is the API

`next.kind` is one of four values, and it is the entire control flow of an
agent driving specd:

| kind | meaning | correct agent behavior |
| --- | --- | --- |
| `operation` | there is a next operation, named in `operation` + `arguments` | run it |
| `human_handoff` | a human gate | **stop.** Report `instruction` verbatim |
| `terminal` | nothing further to do | stop, report done |
| `blocked` | work is stopped; `owner` says who must act | fix it if `owner` is `author`; hand off if `human` |

A refusal envelope:

```json
{
  "schema": "specd.agent/v1",
  "ok": false,
  "operation": "status",
  "diagnostics": [{
    "code": "change_not_found",
    "severity": "error",
    "message": "change does not exist",
    "path": "…/.specd/changes/add-farewell",
    "fix": "choose an existing change"
  }],
  "next": { "kind": "blocked", "owner": "author", "instruction": "choose an existing change" },
  "exit": { "code": 1, "class": "failure" }
}
```

There is always exactly one legal next action. An agent that retries the same
call unchanged, or tries a different flag to get past a refusal, is doing the
one thing the design is built to prevent.

### Bounded output

Process output in the envelope is sampled, not streamed: excerpts are capped at
2048 bytes, invalid UTF-8 and control characters are scrubbed, and
credential-shaped text (`Authorization:`, `api_key=`, `token:`, `password=`,
`secret:`) is redacted. Every excerpt is paired with a digest and an evidence
reference, so the authoritative record is always reachable.

Don't parse excerpts for correctness. Read the exit code and the evidence
record.

## The generated guidance file

`AGENTS.md`, written into the project root, is the instruction set a cold agent
resumes from. It is generated from the operation registry, so the palette it
describes cannot drift from what the binary actually does.

Its managed region is delimited:

```html
<!-- specd:begin schema=1 hash=… -->
…harness-owned bytes…
<!-- specd:end -->
```

Bytes inside the markers are harness-owned; editing them makes a refresh refuse
as drift. Anything you write **outside** the markers is preserved verbatim, so
project-specific agent notes live there safely.

The generated content covers root and change selection, who owns what, the loop,
declared scope, verification versus completion, the human gate, refusal
handling, host assurance, review, reports, and then one section per
agent-visible executable operation with its usage and example.

`specd init` installs it, and re-running `init` on an adopted root refreshes it.
The refresh is idempotent and rewrites only the managed region, so it is safe to
run against a project that already has an `AGENTS.md` of its own: the managed
block is appended and your bytes are left alone.

Note that `init` therefore leaves an untracked file in your project root. Commit
it along with `.specd/` — `start` requires a clean worktree, so an uncommitted
guidance file will stop the loop at the first attempt.

The same install is reachable programmatically through the host adapter
(`internal/host`, `Adapter.Install` with the `skill` surface) for integrations
that embed specd as a Go package.

## The operation palette

The callable set is the registry's own agent projection, filtered to operations
that are executable and agent-visible. The adapter adds nothing and hides
nothing else.

Thirteen operations are agent-callable. **`approve` and `sync` are not**, and no
flag reveals them. This is not access control an agent can negotiate with; the
operations have no agent-callable form.

Every flag, exit code, and lifecycle constraint is in
[operations.md](operations.md), generated from the same registry and
byte-checked against it by the release gate. Do not hardcode a second copy of
the palette — read the registry projection or that file.

## Host capabilities and assurance

specd's adapter declares five independent capabilities, none inferred from the
adapter merely existing:

| capability | the local host declares |
| --- | --- |
| `canInstallSkill` | **true** — it can write a guidance file |
| `canInstallCommandWrapper` | false |
| `canHideHumanOperations` | false |
| `canEnforceToolPathRestrictions` | false |
| `canAttestActorClass` | false |

A host earns the `host_enforced` assurance label only by declaring the last
three. The local host declares none of them, so its label is **`advisory`**, and
that is what appears in `context` and `start` output.

Advisory means: the harness *checks* scope, actor class, and approval. It does
not *contain* a process. A shell-capable agent can write outside its declared
files — specd will refuse the attempt afterwards. Detection, not prevention.

Report this honestly. An agent that tells a user its file scope was *enforced*
when the label says `advisory` is overstating a security property.

## Rules for an agent integration

1. **Branch on `next.kind`.** Don't parse human-readable text.
2. **Stop at `human_handoff`.** Report the instruction verbatim. Don't retry,
   don't look for another route, don't claim the gate was passed.
3. **Round-trip `state.revision`** into `start --revision` and
   `complete --revision`. It is a staleness guard.
4. **Never write under `.specd/`.** State, history, evidence, and accepted specs
   are harness-owned. Writing there is not a repair.
5. **Never edit planning artifacts during an attempt.** It invalidates both the
   approval and the attempt.
6. **Treat a passing `verify` as an observation.** Call `complete`.
7. **Don't invent operations.** If the palette lacks something you need, say so
   — and if it actually blocked work, record it with `friction`.

## Testing an integration cold

The bar specd sets for itself (journey 14, `TestFreshAgentResume`): with no
conversation history, only the repository and the generated guidance, an agent
must be able to identify the selected root, the current change and lifecycle,
accepted specs versus proposed deltas, approval freshness, the full ready
frontier, one task's context and declared files, the verification command, the
next legal action or human handoff, and the source of truth for state, evidence,
and accepted behavior.

If your integration can't answer those from a cold start, that is a contract or
guidance problem, not a prompt problem.
