// Package host is the one concrete adapter between specd and the local coding
// host. It transforms destinations and invocation argv and declares what the
// host can actually do. It owns no workflow prose and no operation semantics:
// guidance comes from internal/generate, verbs come from the operation registry.
package host

import (
	"fmt"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core"
	"github.com/0xkhdr/specd-cli/internal/core/failure"
	corepath "github.com/0xkhdr/specd-cli/internal/core/path"
	"github.com/0xkhdr/specd-cli/internal/exec"
	"github.com/0xkhdr/specd-cli/internal/generate"
)

// Assurance labels. There is no middle rung and no configuration path to the
// upper one: only a host that hides human operations, restricts tool paths, and
// attests actor class can claim enforcement.
const (
	AssuranceAdvisory     = core.AttemptAssurance
	AssuranceHostEnforced = "host_enforced"
)

// Surface is one installable managed product.
type Surface string

const (
	SurfaceSkill          Surface = "skill"
	SurfaceCommandWrapper Surface = "command_wrapper"
)

// Capabilities are five independent declared facts about the host. They are
// never inferred from the adapter existing, and declaring one does not make it
// true — a host that cannot prove a capability must not declare it.
type Capabilities struct {
	CanInstallSkill                bool `json:"canInstallSkill"`
	CanInstallCommandWrapper       bool `json:"canInstallCommandWrapper"`
	CanHideHumanOperations         bool `json:"canHideHumanOperations"`
	CanEnforceToolPathRestrictions bool `json:"canEnforceToolPathRestrictions"`
	CanAttestActorClass            bool `json:"canAttestActorClass"`
}

// LocalCapabilities is the honest capability record of the current local host: a
// coding agent with shell access to the developer's machine. It can write a
// guidance file. It cannot keep a shell-capable agent away from a human verb,
// restrict the paths a tool touches, or attest who is at the keyboard, so it
// declares none of those.
func LocalCapabilities() Capabilities {
	return Capabilities{CanInstallSkill: true}
}

// Assurance is the label this capability set earns.
func (capabilities Capabilities) Assurance() string {
	if capabilities.CanHideHumanOperations &&
		capabilities.CanEnforceToolPathRestrictions &&
		capabilities.CanAttestActorClass {
		return AssuranceHostEnforced
	}
	return AssuranceAdvisory
}

// Adapter is the local host binding for one project.
type Adapter struct {
	root         string
	capabilities Capabilities
}

// Local binds the adapter to one validated project root. The capability record
// is supplied by whoever knows the host; the adapter never guesses it.
func Local(root string, capabilities Capabilities) (*Adapter, error) {
	resolved, err := corepath.ResolveRoot(root, "")
	if err != nil {
		return nil, err
	}
	return &Adapter{root: resolved, capabilities: capabilities}, nil
}

func (adapter *Adapter) Root() string               { return adapter.root }
func (adapter *Adapter) Capabilities() Capabilities { return adapter.capabilities }
func (adapter *Adapter) Assurance() string          { return adapter.capabilities.Assurance() }

// Callable is the operation palette the adapter exposes. It is the registry's
// own agent projection filtered by executability; the adapter adds nothing and
// hides nothing else.
func (adapter *Adapter) Callable() []string {
	facts := core.ProjectAgentOperations()
	ids := make([]string, 0, len(facts.Operations))
	for _, operation := range facts.Operations {
		if operation.Executable {
			ids = append(ids, operation.ID)
		}
	}
	return ids
}

// Install writes the supported managed surfaces. A requested surface the host
// declares it cannot deliver is a refusal with one recovery, never a quiet
// fallback to a different surface.
func (adapter *Adapter) Install(surfaces ...Surface) ([]string, error) {
	written := make([]string, 0, len(surfaces))
	for _, surface := range surfaces {
		switch surface {
		case SurfaceSkill:
			if !adapter.capabilities.CanInstallSkill {
				return nil, adapter.missing(surface, "canInstallSkill")
			}
			result, err := generate.Refresh(adapter.root)
			if err != nil {
				return nil, err
			}
			written = append(written, result.Path)
		case SurfaceCommandWrapper:
			if !adapter.capabilities.CanInstallCommandWrapper {
				return nil, adapter.missing(surface, "canInstallCommandWrapper")
			}
			// Reachable only for a host that declares the capability; this
			// adapter's host does not, so there is no wrapper product to ship.
			return nil, failure.New("adapter_surface_unimplemented", adapter.root, "",
				"this adapter ships no command wrapper product",
				"install the "+string(SurfaceSkill)+" surface instead")
		default:
			return nil, failure.New("adapter_surface_unknown", adapter.root, "",
				fmt.Sprintf("unknown managed surface %q", surface),
				"install the "+string(SurfaceSkill)+" surface")
		}
	}
	return written, nil
}

func (adapter *Adapter) missing(surface Surface, capability string) error {
	return failure.New("adapter_capability_absent", adapter.root, "",
		fmt.Sprintf("host declares %s false, so the %s surface cannot be installed",
			capability, surface),
		"ask the host owner to enable "+capability+" or drop the surface")
}

// Invoke transforms one callable operation into bounded argv for this project.
// Every argument reaches the process literally: the adapter interprets no shell
// and reuses no display string as execution input.
func (adapter *Adapter) Invoke(operation string, arguments ...string) (exec.Request, error) {
	registered, known := core.OperationByID(operation)
	switch {
	case !known:
		return exec.Request{}, failure.New("adapter_operation_unknown", adapter.root, "",
			fmt.Sprintf("unknown operation %q", operation),
			"call one of "+strings.Join(adapter.Callable(), ", "))
	case !registered.Executable:
		return exec.Request{}, failure.New("adapter_operation_reserved", adapter.root, "",
			fmt.Sprintf("operation %q is reserved and not callable in this build", operation),
			"call one of "+strings.Join(adapter.Callable(), ", "))
	case !registered.AgentVisible:
		// The human boundary is metadata. The adapter hands back the canonical
		// refusal instead of restating one, and never a wider surface.
		return exec.Request{}, adapter.humanBoundary(registered)
	}
	argv := append([]string{"specd", registered.ID}, arguments...)
	for _, argument := range arguments {
		if strings.TrimSpace(argument) == "" || strings.IndexByte(argument, 0) >= 0 {
			return exec.Request{}, failure.New("adapter_argument_unsafe", adapter.root, "",
				"argument is empty or contains a NUL byte",
				"pass one literal value per declared argument")
		}
	}
	// Root transformation is the whole of what this adapter owns besides argv.
	argv = append(argv, "--root", adapter.root)
	return exec.Request{Dir: adapter.root, Argv: argv}, nil
}

func (adapter *Adapter) humanBoundary(operation core.Operation) error {
	if operation.ID == "approve" {
		return core.ApprovalHumanOnlyFacts().AuthorizeAgentCapableRoute()
	}
	return failure.New("human_operation_required", adapter.root, "",
		fmt.Sprintf("%s is human-only and has no agent-callable form", operation.ID),
		"hand off "+operation.ID+" to a human terminal")
}
