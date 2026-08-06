package core

import (
	"fmt"
	"slices"
	"strings"
)

// OperationSchemaVersion versions the operation metadata contract. Every
// consumer (terminal help, agent JSON, generated guidance, docs, adapters)
// pins against it, so an incompatible shape change is visible instead of
// silently reinterpreted.
const OperationSchemaVersion = 1

// OperationActor is who may invoke an operation. There is no fallback value:
// an undeclared actor fails registry validation rather than defaulting to a
// wider class.
type OperationActor string

const (
	ActorAgent  OperationActor = "agent"
	ActorHuman  OperationActor = "human"
	ActorEither OperationActor = "either"
)

// OperationEffect is what an operation may write. `read` touches no managed
// truth, `project_write` writes project files outside change state, and
// `state_write` mutates harness-owned state, history, or evidence.
type OperationEffect string

const (
	EffectRead         OperationEffect = "read"
	EffectProjectWrite OperationEffect = "project_write"
	EffectStateWrite   OperationEffect = "state_write"
)

// Authority provenance: where the identity an operation acts under comes from.
const (
	AuthorityNone     = "none"
	AuthorityActor    = "actor_identity"
	AuthorityHumanID  = "human_identity"
	ScopeNone         = "none"
	ScopeRoot         = "root"
	ScopeChange       = "change"
	ScopeDeclaredFile = "task_declared_files"
)

// Exit classes. They stay distinct so a refusal is never mistaken for a
// failed gate and a failed gate is never mistaken for success.
const (
	ExitClassSuccess = "success"
	ExitClassFailure = "failure"
	ExitClassRefusal = "refusal"
)

type OperationArgument struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type OperationFlag struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     string   `json:"default,omitempty"`
	Description string   `json:"description"`
}

type OperationExit struct {
	Code    int    `json:"code"`
	Class   string `json:"class"`
	Meaning string `json:"meaning"`
}

// Operation is the sole owner of operation metadata. Handlers own behavior and
// restate nothing from this record.
type Operation struct {
	ID      string          `json:"id"`
	Summary string          `json:"summary"`
	Actor   OperationActor  `json:"actor"`
	Effect  OperationEffect `json:"effect"`
	// Lifecycles is the set of change lifecycle stages the operation applies
	// to. It is empty exactly when the operation resolves no existing change.
	Lifecycles []Lifecycle `json:"lifecycles"`
	// RequiresChange means the operation resolves an existing change and its
	// harness-owned state; `new` names a change but creates it, so it is false.
	RequiresRoot    bool                `json:"requiresRoot"`
	RequiresChange  bool                `json:"requiresChange"`
	RequiresTask    bool                `json:"requiresTask"`
	AuthoritySource string              `json:"authoritySource"`
	ScopeSource     string              `json:"scopeSource"`
	Arguments       []OperationArgument `json:"arguments"`
	Flags           []OperationFlag     `json:"flags"`
	Exits           []OperationExit     `json:"exits"`
	ResultType      string              `json:"resultType,omitempty"`
	AgentVisible    bool                `json:"agentVisible"`
	// Executable is false for a registered contract whose handler does not
	// exist yet. A non-executable entry can never be dispatched or advertised
	// as callable.
	Executable bool `json:"executable"`
	// Example is display data only. It is never parsed, expanded, or executed.
	Example string `json:"example"`
}

func globalFlags() []OperationFlag {
	return []OperationFlag{
		{Name: "--root", Type: "string", Description: "Select the project root holding .specd."},
		{Name: "--json", Type: "bool", Description: "Emit the machine-readable result document."},
	}
}

func flags(extra ...OperationFlag) []OperationFlag {
	return append(globalFlags(), extra...)
}

// Lifecycles is every declared change lifecycle stage, in order. An operation
// applicable to all of them is unrestricted.
func Lifecycles() []Lifecycle {
	return []Lifecycle{
		LifecyclePlanning, LifecycleApproved, LifecycleExecuting,
		LifecycleReconciling, LifecycleArchived,
	}
}

func allLifecycles() []Lifecycle { return Lifecycles() }

func exits(failureMeaning string) []OperationExit {
	return []OperationExit{
		{Code: 0, Class: ExitClassSuccess, Meaning: "operation succeeded"},
		{Code: 1, Class: ExitClassFailure, Meaning: failureMeaning},
		{Code: 2, Class: ExitClassRefusal, Meaning: "usage error or fail-closed refusal"},
	}
}

// reservedExits is the only exit table a non-executable contract may declare:
// it cannot succeed, so it can only refuse.
func reservedExits() []OperationExit {
	return []OperationExit{
		{Code: 2, Class: ExitClassRefusal, Meaning: "reserved operation is not callable in this build"},
	}
}

// operations is the canonical registry in base-loop order. Order is the public
// projection order for help, docs, JSON, guidance, and adapters.
var operations = []Operation{
	{
		ID: "init", Summary: "Create or adopt the managed .specd root and install the agent guidance file.",
		Actor: ActorEither, Effect: EffectProjectWrite,
		RequiresRoot: true, AuthoritySource: AuthorityNone, ScopeSource: ScopeRoot,
		Arguments:  []OperationArgument{{Name: "root", Description: "Project directory to manage."}},
		Flags:      flags(),
		Exits:      exits("the root could not be created or adopted"),
		ResultType: "init_result", AgentVisible: true, Executable: true,
		Example: "specd init",
	},
	{
		ID: "new", Summary: "Create a change with its planning artifacts and state.",
		Actor: ActorEither, Effect: EffectStateWrite,
		// Creation records a best-effort actor label; it checks no authority,
		// so nothing here may claim one.
		RequiresRoot: true, AuthoritySource: AuthorityNone, ScopeSource: ScopeChange,
		Arguments: []OperationArgument{{Name: "change", Required: true, Description: "Change identifier to create."}},
		Flags: flags(OperationFlag{
			Name: "--capability", Type: "string",
			Description: "Capability the delta spec is authored under; the change name when omitted.",
		}),
		Exits:      exits("the change could not be created"),
		ResultType: "new_result", AgentVisible: true, Executable: true,
		Example: "specd new safe-create --capability safety",
	},
	{
		ID: "check", Summary: "Run planning gates over the change and report findings.",
		Actor: ActorEither, Effect: EffectRead, Lifecycles: allLifecycles(),
		RequiresRoot: true, RequiresChange: true,
		AuthoritySource: AuthorityNone, ScopeSource: ScopeChange,
		Arguments:  []OperationArgument{{Name: "change", Required: true, Description: "Change to validate."}},
		Flags:      flags(),
		Exits:      exits("blocking gate findings were reported"),
		ResultType: "check_result", AgentVisible: true, Executable: true,
		Example: "specd check safe-create",
	},
	{
		ID: "approve", Summary: "Record human approval of the current planning artifacts.",
		Actor: ActorHuman, Effect: EffectStateWrite,
		Lifecycles:   []Lifecycle{LifecyclePlanning},
		RequiresRoot: true, RequiresChange: true,
		AuthoritySource: AuthorityHumanID, ScopeSource: ScopeChange,
		Arguments: []OperationArgument{{Name: "change", Required: true, Description: "Change to approve."}},
		Flags: flags(
			OperationFlag{Name: "--approver", Type: "string", Description: "Approver identity for non-interactive use."},
			OperationFlag{Name: "--reason", Type: "string", Description: "Reason recorded with the approval."},
		),
		Exits:      exits("approval was refused or could not be recorded"),
		ResultType: "approval_record", AgentVisible: false, Executable: true,
		Example: "specd approve safe-create --approver me@example.com --reason reviewed",
	},
	{
		ID: "reopen", Summary: "Revoke execution authority and return an approved change to planning.",
		Actor: ActorEither, Effect: EffectStateWrite,
		Lifecycles:   []Lifecycle{LifecycleApproved},
		RequiresRoot: true, RequiresChange: true,
		AuthoritySource: AuthorityActor, ScopeSource: ScopeChange,
		Arguments: []OperationArgument{{Name: "change", Required: true, Description: "Approved change to return to planning."}},
		Flags: flags(
			OperationFlag{
				Name: "--revision", Type: "uint", Required: true,
				Description: "State revision observed before reopening.",
			},
			OperationFlag{
				Name: "--reason", Type: "string", Required: true,
				Description: "Why execution authority is being revoked.",
			},
		),
		Exits:      exits("reopening was refused or could not be recorded"),
		ResultType: "reopen_result", AgentVisible: true, Executable: true,
		Example: "specd reopen safe-create --revision 4 --reason repair-plan",
	},
	{
		ID: "status", Summary: "Report lifecycle, approval, readiness, and next action for a change.",
		Actor: ActorEither, Effect: EffectRead, Lifecycles: allLifecycles(),
		RequiresRoot: true, RequiresChange: true,
		AuthoritySource: AuthorityNone, ScopeSource: ScopeChange,
		Arguments:  []OperationArgument{{Name: "change", Required: true, Description: "Change to inspect."}},
		Flags:      flags(),
		Exits:      exits("the change status could not be projected"),
		ResultType: "status_result", AgentVisible: true, Executable: true,
		Example: "specd status safe-create",
	},
	{
		ID: "next", Summary: "Project the ready task frontier or the single blocking action.",
		Actor: ActorEither, Effect: EffectRead, Lifecycles: allLifecycles(),
		RequiresRoot: true, RequiresChange: true,
		AuthoritySource: AuthorityNone, ScopeSource: ScopeChange,
		Arguments: []OperationArgument{
			{Name: "change", Required: true, Description: "Change to project."},
			{Name: "task", Description: "Validate one task's frontier membership."},
		},
		Flags:      flags(),
		Exits:      exits("readiness could not be projected"),
		ResultType: "next_result", AgentVisible: true, Executable: true,
		Example: "specd next safe-create",
	},
	{
		ID: "context", Summary: "Assemble the bounded read context for exactly one task.",
		Actor: ActorEither, Effect: EffectRead,
		Lifecycles:   []Lifecycle{LifecycleApproved},
		RequiresRoot: true, RequiresChange: true, RequiresTask: true,
		AuthoritySource: AuthorityNone, ScopeSource: ScopeDeclaredFile,
		Arguments: []OperationArgument{
			{Name: "change", Required: true, Description: "Change holding the task."},
			{Name: "task", Required: true, Description: "Task to bound context for."},
		},
		Flags: flags(OperationFlag{
			Name: "--budget-bytes", Type: "int", Default: "0",
			Description: "Byte budget for assembled context; 0 is unbounded.",
		}),
		Exits:      exits("context could not be assembled within its bounds"),
		ResultType: "context_manifest", AgentVisible: true, Executable: true,
		Example: "specd context safe-create S1-01 --budget-bytes 65536",
	},
	{
		ID: "start", Summary: "Bind a clean Git baseline and open one task attempt.",
		Actor: ActorEither, Effect: EffectStateWrite,
		Lifecycles:   []Lifecycle{LifecycleApproved},
		RequiresRoot: true, RequiresChange: true, RequiresTask: true,
		AuthoritySource: AuthorityActor, ScopeSource: ScopeDeclaredFile,
		Arguments: []OperationArgument{
			{Name: "change", Required: true, Description: "Change holding the task."},
			{Name: "task", Required: true, Description: "Frontier task to start."},
		},
		Flags: flags(OperationFlag{
			Name: "--revision", Type: "uint", Required: true,
			Description: "State revision observed before the attempt.",
		}),
		Exits:      exits("the attempt could not be opened"),
		ResultType: "start_result", AgentVisible: true, Executable: true,
		Example: "specd start safe-create S1-01 --revision 4",
	},
	{
		ID: "verify", Summary: "Run the task's declared verification and record evidence at current HEAD.",
		Actor: ActorEither, Effect: EffectStateWrite,
		Lifecycles:   []Lifecycle{LifecycleApproved},
		RequiresRoot: true, RequiresChange: true, RequiresTask: true,
		AuthoritySource: AuthorityActor, ScopeSource: ScopeDeclaredFile,
		Arguments: []OperationArgument{
			{Name: "change", Required: true, Description: "Change holding the task."},
			{Name: "task", Required: true, Description: "Task to verify."},
			{Name: "attempt", Required: true, Description: "Attempt identifier returned by start."},
		},
		Flags: flags(
			OperationFlag{
				Name: "--timeout", Type: "duration", Default: "2m0s",
				Description: "Wall-clock bound on the verification command.",
			},
			OperationFlag{
				Name: "--output-limit", Type: "int", Default: "32768",
				Description: "Byte bound on recorded command output.",
			},
		),
		Exits:      exits("evidence could not be recorded"),
		ResultType: "verify_result", AgentVisible: true, Executable: true,
		Example: "specd verify safe-create S1-01 A1 --timeout 2m",
	},
	{
		ID: "complete", Summary: "Consume applicable passing evidence and close the task.",
		Actor: ActorEither, Effect: EffectStateWrite,
		Lifecycles:   []Lifecycle{LifecycleApproved},
		RequiresRoot: true, RequiresChange: true, RequiresTask: true,
		AuthoritySource: AuthorityActor, ScopeSource: ScopeDeclaredFile,
		Arguments: []OperationArgument{
			{Name: "change", Required: true, Description: "Change holding the task."},
			{Name: "task", Required: true, Description: "Task to complete."},
		},
		Flags: flags(OperationFlag{
			Name: "--revision", Type: "uint", Required: true,
			Description: "State revision observed before completion.",
		}),
		Exits:      exits("completion was refused or could not be recorded"),
		ResultType: "completion", AgentVisible: true, Executable: true,
		Example: "specd complete safe-create S1-01 --revision 6",
	},
	{
		ID: "review", Summary: "Record or project one separate reviewer verdict for a task.",
		// Review proves nothing runnable and authorizes nothing: it records a
		// human reviewer's verdict, who is never the approver of the change nor
		// the actor that implemented the task.
		Actor: ActorEither, Effect: EffectStateWrite,
		Lifecycles:   []Lifecycle{LifecycleApproved},
		RequiresRoot: true, RequiresChange: true, RequiresTask: true,
		AuthoritySource: AuthorityActor, ScopeSource: ScopeChange,
		Arguments: []OperationArgument{
			{Name: "change", Required: true, Description: "Change holding the reviewed task."},
			{Name: "task", Required: true, Description: "Task the verdict is bound to."},
			{Name: "attempt", Required: true, Description: "Attempt identifier returned by start."},
		},
		Flags: flags(
			OperationFlag{
				Name: "--reviewer", Type: "string",
				Description: "Reviewer identity claim; it must match the trusted reviewer identity.",
			},
			OperationFlag{
				Name: "--verdict", Type: "string", Enum: []string{"approve", "reject"},
				Description: "Verdict to record; omitted projects the current review state without writing.",
			},
			OperationFlag{
				Name: "--findings", Type: "string",
				Description: "Bounded reviewer findings; required to reject.",
			},
		),
		Exits:      exits("the verdict was refused or could not be recorded"),
		ResultType: "review_result", AgentVisible: true, Executable: true,
		// The example projects the current verdict rather than recording one:
		// the agent-facing guide renders it, and recording a human's verdict is
		// never the shape an agent should copy first.
		Example: "specd review safe-create S1-01 A1 --reviewer reviewer@example.com",
	},
	{
		ID: "sync", Summary: "Reconcile approved deltas into accepted specs.",
		// Accepted truth is the project's behavioral contract, so making a
		// proposal into truth is a human semantic authorization, not an agent
		// step. It is a second content-hash-bound gate, distinct from the
		// approval that authorized the plan.
		Actor: ActorHuman, Effect: EffectStateWrite,
		Lifecycles:   []Lifecycle{LifecycleApproved, LifecycleReconciling},
		RequiresRoot: true, RequiresChange: true,
		AuthoritySource: AuthorityHumanID, ScopeSource: ScopeChange,
		Arguments: []OperationArgument{{Name: "change", Required: true, Description: "Change to reconcile."}},
		Flags: flags(
			OperationFlag{Name: "--approver", Type: "string", Description: "Approver identity for non-interactive use."},
			OperationFlag{Name: "--reason", Type: "string", Description: "Reason recorded with the sync authorization."},
		),
		Exits:      exits("sync was refused or could not be committed"),
		ResultType: "sync_result", AgentVisible: false, Executable: true,
		Example: "specd sync safe-create --approver me@example.com --reason reviewed",
	},
	{
		ID: "archive", Summary: "Validate and move a reconciled change into the archive.",
		Actor: ActorEither, Effect: EffectStateWrite,
		Lifecycles:   []Lifecycle{LifecycleReconciling},
		RequiresRoot: true, RequiresChange: true,
		AuthoritySource: AuthorityActor, ScopeSource: ScopeChange,
		Arguments:  []OperationArgument{{Name: "change", Required: true, Description: "Change to archive."}},
		Flags:      flags(),
		Exits:      exits("archive was refused or could not be committed"),
		ResultType: "archive_result", AgentVisible: true, Executable: true,
		Example: "specd archive safe-create",
	},
	{
		ID: "report", Summary: "Project one of the four canonical read-only reports.",
		// A report is a projection of canonical truth and nothing else: it
		// writes no state, records no evidence, and authorizes nothing. Only
		// four kinds exist, so no consumer can invent a fifth report.
		Actor: ActorEither, Effect: EffectRead, Lifecycles: allLifecycles(),
		RequiresRoot: true, RequiresChange: true,
		AuthoritySource: AuthorityNone, ScopeSource: ScopeChange,
		Arguments: []OperationArgument{
			{Name: "change", Required: true, Description: "Change to project."},
		},
		Flags: flags(
			OperationFlag{
				Name: "--kind", Type: "string", Required: true,
				Enum:        []string{"status", "proof", "history", "review"},
				Description: "Report to project; only these four kinds exist.",
			},
			OperationFlag{
				Name: "--profile", Type: "string", Enum: []string{"default", "production"},
				Description: "Policy profile the report is projected under.",
			},
		),
		Exits:      exits("the report could not be projected"),
		ResultType: "report_result", AgentVisible: true, Executable: true,
		Example: "specd report safe-create --kind status",
	},
	{
		ID: "friction", Summary: "Record one observation that a deferred domain blocked real work.",
		// Friction is evidence for D14 and never authority: it appends one
		// history record, carries no revision transition, and unblocks nothing.
		// The observed revision is still required, so a stale observer refuses
		// rather than recording friction against truth it never read.
		Actor: ActorEither, Effect: EffectStateWrite, Lifecycles: allLifecycles(),
		RequiresRoot: true, RequiresChange: true, RequiresTask: true,
		AuthoritySource: AuthorityActor, ScopeSource: ScopeChange,
		Arguments: []OperationArgument{
			{Name: "change", Required: true, Description: "Change holding the blocked task."},
			{Name: "task", Required: true, Description: "Task status reports as blocked."},
		},
		Flags: flags(
			OperationFlag{
				Name: "--domain", Type: "string", Required: true, Enum: DeferredDomains(),
				Description: "Deferred domain whose absence caused the block; only these exist.",
			},
			OperationFlag{
				Name: "--blocked-operation", Type: "string", Required: true,
				Description: "Operation that was blocked when the observation was made.",
			},
			OperationFlag{
				Name: "--consequence", Type: "string", Required: true,
				Description: "What the missing capability prevented, observed and not predicted.",
			},
			OperationFlag{
				Name: "--revision", Type: "uint", Required: true,
				Description: "State revision observed before recording.",
			},
		),
		Exits:      exits("friction was refused or could not be recorded"),
		ResultType: "friction_result", AgentVisible: true, Executable: true,
		Example: "specd friction safe-create S1-01 --domain orchestration " +
			"--blocked-operation next --consequence no-route-exists --revision 6",
	},
}

// The registry is a compile-time table, so an invalid entry is a broken build.
// Failing here keeps a malformed operation from ever reaching a projection.
func init() {
	if err := ValidateOperations(); err != nil {
		panic("operation registry invalid: " + err.Error())
	}
}

func (operation Operation) clone() Operation {
	operation.Lifecycles = slices.Clone(operation.Lifecycles)
	operation.Arguments = slices.Clone(operation.Arguments)
	operation.Exits = slices.Clone(operation.Exits)
	operation.Flags = slices.Clone(operation.Flags)
	for index := range operation.Flags {
		operation.Flags[index].Enum = slices.Clone(operation.Flags[index].Enum)
	}
	return operation
}

// Operations returns every registered operation in deterministic order.
func Operations() []Operation {
	out := make([]Operation, 0, len(operations))
	for _, operation := range operations {
		out = append(out, operation.clone())
	}
	return out
}

// AgentOperations is the agent-callable palette. Human-only and reserved
// operations are absent; nothing widens the palette back.
func AgentOperations() []Operation {
	out := make([]Operation, 0, len(operations))
	for _, operation := range operations {
		if operation.AgentVisible {
			out = append(out, operation.clone())
		}
	}
	return out
}

// OperationByID resolves one operation. A miss is a miss: callers refuse
// instead of guessing a neighbour.
func OperationByID(id string) (Operation, bool) {
	for _, operation := range operations {
		if operation.ID == id {
			return operation.clone(), true
		}
	}
	return Operation{}, false
}

// Flag resolves one declared flag of this operation.
func (operation Operation) Flag(name string) (OperationFlag, bool) {
	for _, flag := range operation.Flags {
		if flag.Name == name {
			return flag, true
		}
	}
	return OperationFlag{}, false
}

// RequiredArguments is the count of leading positional arguments that must be
// supplied; len(Arguments) is the maximum.
func (operation Operation) RequiredArguments() int {
	count := 0
	for _, argument := range operation.Arguments {
		if argument.Required {
			count++
		}
	}
	return count
}

// Usage renders the invocation shape from metadata so no surface hand-writes
// a usage line that can drift from the registry.
func (operation Operation) Usage() string {
	parts := []string{"specd", operation.ID}
	for _, argument := range operation.Arguments {
		if argument.Required {
			parts = append(parts, "<"+argument.Name+">")
			continue
		}
		parts = append(parts, "["+argument.Name+"]")
	}
	for _, flag := range operation.Flags {
		text := flag.Name
		if flag.Type != "bool" {
			text += " <" + strings.TrimPrefix(flag.Name, "--") + ">"
		}
		if !flag.Required {
			text = "[" + text + "]"
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " ")
}

// AppliesTo reports whether the operation is legal in a lifecycle stage. An
// operation that resolves no change applies everywhere.
func (operation Operation) AppliesTo(lifecycle string) bool {
	if len(operation.Lifecycles) == 0 {
		return true
	}
	return slices.Contains(operation.Lifecycles, Lifecycle(lifecycle))
}

// ValidateOperations fails closed on incomplete, duplicate, or contradictory
// metadata.
func ValidateOperations() error { return validateOperations(operations) }

var (
	validActors     = []OperationActor{ActorAgent, ActorHuman, ActorEither}
	validEffects    = []OperationEffect{EffectRead, EffectProjectWrite, EffectStateWrite}
	validAuthority  = []string{AuthorityNone, AuthorityActor, AuthorityHumanID}
	validScope      = []string{ScopeNone, ScopeRoot, ScopeChange, ScopeDeclaredFile}
	validFlagTypes  = []string{"bool", "string", "int", "uint", "duration"}
	validExitClass  = []string{ExitClassSuccess, ExitClassFailure, ExitClassRefusal}
	unsafeInExample = ";|&$`<>(){}[]*?!\\\"'\n\r\t"
)

func validateOperations(registry []Operation) error {
	if len(registry) == 0 {
		return fmt.Errorf("operation registry is empty")
	}
	seen := make(map[string]bool, len(registry))
	seenResult := make(map[string]string, len(registry))
	for _, operation := range registry {
		if err := validateOperation(operation); err != nil {
			return err
		}
		if seen[operation.ID] {
			return fmt.Errorf("%s: duplicate operation id", operation.ID)
		}
		seen[operation.ID] = true
		if operation.ResultType == "" {
			continue
		}
		if owner, taken := seenResult[operation.ResultType]; taken {
			return fmt.Errorf("%s: result type %q already owned by %s",
				operation.ID, operation.ResultType, owner)
		}
		seenResult[operation.ResultType] = operation.ID
	}
	return nil
}

func validateOperation(operation Operation) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%s: %s", operation.ID, fmt.Sprintf(format, args...))
	}
	switch {
	case strings.TrimSpace(operation.ID) == "":
		return fmt.Errorf("operation id is required")
	case strings.TrimSpace(operation.Summary) == "":
		return fail("summary is required")
	case !slices.Contains(validActors, operation.Actor):
		return fail("unknown actor %q", operation.Actor)
	case !slices.Contains(validEffects, operation.Effect):
		return fail("unknown effect %q", operation.Effect)
	case !slices.Contains(validAuthority, operation.AuthoritySource):
		return fail("unknown authority source %q", operation.AuthoritySource)
	case !slices.Contains(validScope, operation.ScopeSource):
		return fail("unknown scope source %q", operation.ScopeSource)
	case !operation.RequiresRoot:
		return fail("every operation resolves an explicit root")
	case operation.RequiresTask && !operation.RequiresChange:
		return fail("a task selector requires a change selector")
	case operation.RequiresChange == (len(operation.Lifecycles) == 0):
		return fail("lifecycle applicability is declared exactly when a change is resolved")
	}
	seenLifecycle := make(map[Lifecycle]bool, len(operation.Lifecycles))
	for _, lifecycle := range operation.Lifecycles {
		if !slices.Contains(allLifecycles(), lifecycle) {
			return fail("unknown lifecycle %q", lifecycle)
		}
		if seenLifecycle[lifecycle] {
			return fail("duplicate lifecycle %q", lifecycle)
		}
		seenLifecycle[lifecycle] = true
	}
	if err := validateArguments(operation, fail); err != nil {
		return err
	}
	if err := validateFlags(operation, fail); err != nil {
		return err
	}
	if err := validateExits(operation, fail); err != nil {
		return err
	}
	// The human boundary is metadata, not prose: a human-only operation can
	// never be projected into an agent palette, and a reserved contract can
	// never be advertised as callable.
	if operation.AgentVisible && operation.Actor == ActorHuman {
		return fail("human-only operations are never agent visible")
	}
	if operation.Executable && operation.ResultType == "" {
		return fail("executable operations declare a JSON result type")
	}
	if !operation.Executable {
		if operation.AgentVisible || operation.ResultType != "" {
			return fail("reserved operations are neither callable nor agent visible")
		}
	}
	return validateExample(operation, fail)
}

func validateArguments(operation Operation, fail func(string, ...any) error) error {
	optionalSeen := false
	names := make(map[string]bool, len(operation.Arguments))
	for _, argument := range operation.Arguments {
		if strings.TrimSpace(argument.Name) == "" || strings.TrimSpace(argument.Description) == "" {
			return fail("every argument declares a name and description")
		}
		if names[argument.Name] {
			return fail("duplicate argument %q", argument.Name)
		}
		names[argument.Name] = true
		if !argument.Required {
			optionalSeen = true
			continue
		}
		if optionalSeen {
			return fail("required argument %q follows an optional one", argument.Name)
		}
	}
	if operation.RequiresChange && !names["change"] {
		return fail("a change selector requires a change argument")
	}
	if operation.RequiresTask && !names["task"] {
		return fail("a task selector requires a task argument")
	}
	return nil
}

func validateFlags(operation Operation, fail func(string, ...any) error) error {
	names := make(map[string]bool, len(operation.Flags))
	for _, flag := range operation.Flags {
		switch {
		case !strings.HasPrefix(flag.Name, "--"):
			return fail("flag %q must be long form", flag.Name)
		case names[flag.Name]:
			return fail("duplicate flag %q", flag.Name)
		case !slices.Contains(validFlagTypes, flag.Type):
			return fail("flag %q has unknown type %q", flag.Name, flag.Type)
		case strings.TrimSpace(flag.Description) == "":
			return fail("flag %q requires a description", flag.Name)
		}
		names[flag.Name] = true
		if flag.Type == "bool" && (flag.Required || flag.Default != "" || len(flag.Enum) > 0) {
			return fail("boolean flag %q takes no value, default, or enum", flag.Name)
		}
		if len(flag.Enum) > 0 && flag.Default != "" && !slices.Contains(flag.Enum, flag.Default) {
			return fail("flag %q default %q is outside its enum", flag.Name, flag.Default)
		}
		seenValue := make(map[string]bool, len(flag.Enum))
		for _, value := range flag.Enum {
			if strings.TrimSpace(value) == "" || seenValue[value] {
				return fail("flag %q has an empty or duplicate enum value", flag.Name)
			}
			seenValue[value] = true
		}
	}
	if !names["--root"] || !names["--json"] {
		return fail("every operation accepts --root and --json")
	}
	return nil
}

func validateExits(operation Operation, fail func(string, ...any) error) error {
	if len(operation.Exits) == 0 {
		return fail("exit classes are required")
	}
	codes := make(map[int]bool, len(operation.Exits))
	hasSuccess, hasRefusal := false, false
	for _, exit := range operation.Exits {
		if codes[exit.Code] {
			return fail("duplicate exit code %d", exit.Code)
		}
		codes[exit.Code] = true
		if !slices.Contains(validExitClass, exit.Class) {
			return fail("unknown exit class %q", exit.Class)
		}
		if strings.TrimSpace(exit.Meaning) == "" {
			return fail("exit %d requires a meaning", exit.Code)
		}
		hasSuccess = hasSuccess || exit.Class == ExitClassSuccess
		hasRefusal = hasRefusal || exit.Class == ExitClassRefusal
	}
	if !hasRefusal {
		return fail("every operation declares a refusal exit class")
	}
	if hasSuccess != operation.Executable {
		return fail("a success exit class is declared exactly when the operation is executable")
	}
	return nil
}

func validateExample(operation Operation, fail func(string, ...any) error) error {
	if !strings.HasPrefix(operation.Example, "specd "+operation.ID) {
		return fail("example must invoke the operation it documents")
	}
	if strings.ContainsAny(operation.Example, unsafeInExample) {
		return fail("example %q contains shell metacharacters", operation.Example)
	}
	return nil
}
