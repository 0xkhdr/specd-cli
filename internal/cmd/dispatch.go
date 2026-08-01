package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
)

// Route is how an invocation reached the harness. It is provenance, never
// proof: only a conformant host can attest that a human is at the keyboard.
type Route string

const (
	RouteHumanTerminal Route = "human_terminal"
	RouteAgent         Route = "agent_capable"
)

// Request is one invocation. Args is the full argv beginning with the
// operation id; Root is the fallback root when the invocation selects none.
type Request struct {
	Args  []string
	Root  string
	Actor string
	Route Route
}

// Result is one dispatched operation's outcome. Exit keeps the distinct exit
// classes the registry declares.
type Result struct {
	Operation string
	Value     any
	Exit      int
}

// UsageRefusal is a pre-handler refusal: unknown operation, unknown flag or
// enum, missing selector, lifecycle mismatch, or unauthorized actor. It maps
// to the fail-closed exit class and guarantees no handler ran.
type UsageRefusal struct{ *failure.Refusal }

func (refusal *UsageRefusal) Unwrap() error { return refusal.Refusal }

func refuse(code, reason, next string) error {
	return &UsageRefusal{Refusal: failure.New(code, "", "", reason, next)}
}

// ExitCode maps a dispatch outcome to its declared exit class: 0 success,
// 1 failure, 2 usage or fail-closed refusal.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var usage *UsageRefusal
	if errors.As(err, &usage) {
		return 2
	}
	return 1
}

type invocation struct {
	operation  core.Operation
	root       string
	actor      string
	positional []string
	values     map[string]any
}

// arg resolves a declared positional argument by name; an omitted optional
// argument is the empty string.
func (in invocation) arg(name string) string {
	for index, argument := range in.operation.Arguments {
		if argument.Name != name || index >= len(in.positional) {
			continue
		}
		return in.positional[index]
	}
	return ""
}

func value[T any](in invocation, flag string) T {
	typed, _ := in.values[flag].(T)
	return typed
}

// handlers binds every executable operation id to exactly one handler. The
// parity test proves the mapping is total in both directions.
var handlers = map[string]func(context.Context, invocation) (any, error){
	"init": func(_ context.Context, in invocation) (any, error) {
		return Init(in.root)
	},
	"new": func(_ context.Context, in invocation) (any, error) {
		capability := value[string](in, "--capability")
		if capability == "" {
			capability = in.arg("change")
		}
		return New(in.root, in.arg("change"), in.actor, capability)
	},
	"check": func(_ context.Context, in invocation) (any, error) {
		return Check(in.root, in.arg("change"))
	},
	"approve": func(_ context.Context, in invocation) (any, error) {
		return Approve(in.root, in.arg("change"), ApproveOptions{
			Approver: value[string](in, "--approver"),
			Reason:   value[string](in, "--reason"),
		})
	},
	"status": func(_ context.Context, in invocation) (any, error) {
		return Status(in.root, in.arg("change"))
	},
	"next": func(_ context.Context, in invocation) (any, error) {
		return Next(in.root, in.arg("change"), in.arg("task"))
	},
	"context": func(_ context.Context, in invocation) (any, error) {
		return Context(in.root, in.arg("change"), in.arg("task"), value[int](in, "--budget-bytes"))
	},
	"start": func(_ context.Context, in invocation) (any, error) {
		return Start(in.root, in.arg("change"), in.arg("task"),
			value[uint64](in, "--revision"), StartOptions{Actor: in.actor})
	},
	"verify": func(ctx context.Context, in invocation) (any, error) {
		return Verify(ctx, in.root, in.arg("change"), in.arg("task"), in.arg("attempt"), VerifyOptions{
			Actor:       in.actor,
			Timeout:     value[time.Duration](in, "--timeout"),
			OutputLimit: value[int](in, "--output-limit"),
		})
	},
	"complete": func(_ context.Context, in invocation) (any, error) {
		return Complete(in.root, in.arg("change"), in.arg("task"),
			value[uint64](in, "--revision"), CompleteOptions{Actor: in.actor})
	},
	"sync": func(_ context.Context, in invocation) (any, error) {
		return Sync(in.root, in.arg("change"), SyncOptions{
			Approver: value[string](in, "--approver"),
			Reason:   value[string](in, "--reason"),
		})
	},
	"archive": func(_ context.Context, in invocation) (any, error) {
		return Archive(in.root, in.arg("change"), ArchiveOptions{Actor: in.actor})
	},
}

// Dispatch resolves one invocation against the operation registry and runs its
// handler only after every declared check passes. Nothing here restates
// operation semantics: every rule is read from metadata.
func Dispatch(ctx context.Context, request Request) (Result, error) {
	in, err := resolve(core.Operations(), request)
	if err != nil {
		return Result{}, err
	}
	if err := checkLifecycle(in); err != nil {
		return Result{}, err
	}
	// An interrupted sync or archive is resolved before any other mutation, so
	// a crashed transaction cannot be stepped over by an unrelated command.
	if in.operation.Effect == core.EffectStateWrite {
		if err := core.RecoverTransactions(in.root, time.Now()); err != nil {
			return Result{}, err
		}
	}
	handler, bound := handlers[in.operation.ID]
	if !bound {
		// Unreachable while parity holds; a build that breaks parity refuses
		// rather than silently exposing an unbound id.
		return Result{}, refuse("operation_unbound",
			fmt.Sprintf("operation %q has no handler", in.operation.ID),
			"rebuild specd from a consistent operation registry")
	}
	result := Result{Operation: in.operation.ID}
	result.Value, err = handler(ctx, in)
	if err != nil {
		return Result{}, err
	}
	// The only operation whose declared failure class is reachable without an
	// error: gate findings are a reported outcome, not a broken run.
	if checked, ok := result.Value.(core.CheckResult); ok && !checked.Success {
		result.Exit = 1
	}
	return result, nil
}

// resolve turns argv into a validated invocation. It takes the registry as an
// argument so validation can be exercised against synthetic metadata.
func resolve(registry []core.Operation, request Request) (invocation, error) {
	if len(request.Args) == 0 {
		return invocation{}, refuse("operation_missing", "no operation given", usage(registry))
	}
	name := request.Args[0]
	var operation core.Operation
	found := false
	for _, candidate := range registry {
		if candidate.ID == name {
			operation, found = candidate, true
			break
		}
	}
	if !found {
		return invocation{}, refuse("operation_unknown",
			fmt.Sprintf("unknown operation %q", name), usage(registry))
	}
	if !operation.Executable {
		return invocation{}, refuse("operation_reserved",
			fmt.Sprintf("operation %q is reserved and not callable in this build", name),
			"run "+operation.Usage()+" after the stage that implements it ships")
	}
	if operation.Actor == core.ActorHuman && request.Route == RouteAgent {
		return invocation{}, humanBoundary(operation)
	}
	in := invocation{operation: operation, actor: strings.TrimSpace(request.Actor)}
	raw, err := parseFlags(operation, request.Args[1:], &in)
	if err != nil {
		return invocation{}, err
	}
	if err := typeFlags(operation, raw, &in); err != nil {
		return invocation{}, err
	}
	if err := checkArguments(operation, &in); err != nil {
		return invocation{}, err
	}
	if operation.AuthoritySource == core.AuthorityActor && in.actor == "" {
		return invocation{}, refuse("authority_missing",
			fmt.Sprintf("%s acts under a resolved actor identity", operation.ID),
			"retry through an authorized harness operation")
	}
	in.root, err = selectRoot(operation, in, raw["--root"], request.Root)
	if err != nil {
		return invocation{}, err
	}
	return in, nil
}

// humanBoundary refuses an agent route into a human-only operation with one
// human instruction. Approval reuses the canonical approval boundary rather
// than restating it.
func humanBoundary(operation core.Operation) error {
	if operation.ID == "approve" {
		err := core.ApprovalHumanOnlyFacts().AuthorizeAgentCapableRoute()
		var refusal *failure.Refusal
		if errors.As(err, &refusal) {
			return &UsageRefusal{Refusal: refusal}
		}
		return err
	}
	return refuse("human_operation_required",
		fmt.Sprintf("%s is human-only and cannot use an agent-capable route", operation.ID),
		"hand off "+operation.ID+" to a human terminal")
}

func parseFlags(operation core.Operation, args []string, in *invocation) (map[string]string, error) {
	raw := map[string]string{}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if !strings.HasPrefix(argument, "-") {
			in.positional = append(in.positional, argument)
			continue
		}
		flag, declared := operation.Flag(argument)
		if !declared {
			return nil, refuse("flag_unknown",
				fmt.Sprintf("unknown flag %q for %s", argument, operation.ID),
				"run "+operation.Usage())
		}
		if _, repeated := raw[argument]; repeated {
			return nil, refuse("flag_repeated",
				fmt.Sprintf("%s accepts one %s", operation.ID, argument),
				"run "+operation.Usage())
		}
		if flag.Type == "bool" {
			raw[argument] = "true"
			continue
		}
		index++
		if index >= len(args) {
			return nil, refuse("flag_value_missing",
				fmt.Sprintf("%s requires one value", argument), "run "+operation.Usage())
		}
		raw[argument] = args[index]
	}
	return raw, nil
}

func typeFlags(operation core.Operation, raw map[string]string, in *invocation) error {
	in.values = make(map[string]any, len(raw))
	for _, flag := range operation.Flags {
		text, supplied := raw[flag.Name]
		if !supplied {
			if flag.Required {
				return refuse("flag_required",
					fmt.Sprintf("%s requires %s", operation.ID, flag.Name),
					"run "+operation.Usage())
			}
			text = flag.Default
			if text == "" {
				continue
			}
		}
		if len(flag.Enum) > 0 {
			known := false
			for _, allowed := range flag.Enum {
				known = known || allowed == text
			}
			if !known {
				return refuse("flag_enum_unknown",
					fmt.Sprintf("%s value %q is not one of %s",
						flag.Name, text, strings.Join(flag.Enum, "|")),
					"run "+operation.Usage())
			}
		}
		typed, err := typeValue(flag, text)
		if err != nil {
			return refuse("flag_value_invalid",
				fmt.Sprintf("%s value %q is invalid", flag.Name, text), "run "+operation.Usage())
		}
		in.values[flag.Name] = typed
	}
	return nil
}

func typeValue(flag core.OperationFlag, text string) (any, error) {
	switch flag.Type {
	case "bool":
		return text == "true", nil
	case "string":
		return text, nil
	case "int":
		return strconv.Atoi(text)
	case "uint":
		return strconv.ParseUint(text, 10, 64)
	case "duration":
		return time.ParseDuration(text)
	}
	return nil, fmt.Errorf("unknown flag type %q", flag.Type)
}

func checkArguments(operation core.Operation, in *invocation) error {
	required := operation.RequiredArguments()
	if len(in.positional) < required || len(in.positional) > len(operation.Arguments) {
		return refuse("argument_count",
			fmt.Sprintf("wrong argument count for %s", operation.ID), "run "+operation.Usage())
	}
	for index, supplied := range in.positional {
		if strings.TrimSpace(supplied) != "" {
			continue
		}
		return refuse("selector_missing",
			fmt.Sprintf("%s selector for %s is empty", operation.Arguments[index].Name, operation.ID),
			"run "+operation.Usage())
	}
	return nil
}

// selectRoot keeps root selection explicit: an operation that accepts a
// positional root may not also carry --root.
func selectRoot(operation core.Operation, in invocation, flagRoot, fallback string) (string, error) {
	positional := in.arg("root")
	if positional != "" && flagRoot != "" {
		return "", refuse("root_selection",
			"positional root and --root are ambiguous", "run "+operation.Usage()+" with one root")
	}
	if positional != "" {
		return positional, nil
	}
	if flagRoot != "" {
		return flagRoot, nil
	}
	if strings.TrimSpace(fallback) == "" {
		return "", refuse("root_selection", "no root selected",
			"run "+operation.Usage()+" with an explicit --root")
	}
	return fallback, nil
}

// checkLifecycle refuses an operation outside its declared lifecycle stages
// before any handler side effect.
//
// ponytail: this reads the readiness snapshot the handler will read again;
// give core a lifecycle-only reader if the double read ever shows up in a
// profile.
func checkLifecycle(in invocation) error {
	operation := in.operation
	if !operation.RequiresChange || len(operation.Lifecycles) == len(core.Lifecycles()) {
		return nil
	}
	snapshot, err := core.LoadReadinessSnapshot(in.root, in.arg("change"))
	if err != nil {
		return err
	}
	stage := snapshot.Projection().Stage
	if operation.AppliesTo(stage) {
		return nil
	}
	return refuse("lifecycle_unsupported",
		fmt.Sprintf("%s applies to lifecycle %s, not %q",
			operation.ID, lifecycleList(operation), stage),
		"run specd status "+in.arg("change"))
}

func lifecycleList(operation core.Operation) string {
	names := make([]string, 0, len(operation.Lifecycles))
	for _, lifecycle := range operation.Lifecycles {
		names = append(names, string(lifecycle))
	}
	return strings.Join(names, "|")
}

func usage(registry []core.Operation) string {
	ids := make([]string, 0, len(registry))
	for _, operation := range registry {
		if operation.Executable {
			ids = append(ids, operation.ID)
		}
	}
	return "specd <" + strings.Join(ids, "|") + ">"
}
