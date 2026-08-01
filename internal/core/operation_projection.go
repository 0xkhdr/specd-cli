package core

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/0xkhdr/specd-cli/internal/core/persist"
)

// OperationFact is one public operation fact, keyed by its projection field
// name. Every consumer — terminal help, JSON, generated guidance, docs, and
// adapter exposure — renders these rows, so a field can never appear on one
// surface and not another.
type OperationFact struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// OperationFacts is the whole public projection in registry order.
type OperationFacts struct {
	SchemaVersion int         `json:"schemaVersion"`
	Operations    []Operation `json:"operations"`
}

// ProjectOperations is the one ordered projection every consumer reads.
func ProjectOperations() OperationFacts {
	return OperationFacts{SchemaVersion: OperationSchemaVersion, Operations: Operations()}
}

// ProjectAgentOperations is the agent-callable projection. It is the same
// projection filtered by declared visibility: it never widens an actor,
// effect, or scope, and human-only operations are absent.
func ProjectAgentOperations() OperationFacts {
	return OperationFacts{SchemaVersion: OperationSchemaVersion, Operations: AgentOperations()}
}

// OperationFactsJSON is the machine-readable schema surface. It is the same
// projection, marshalled; no consumer restates operation shape in JSON.
func OperationFactsJSON(facts OperationFacts) ([]byte, error) {
	return json.MarshalIndent(facts, "", "  ")
}

// Facts renders every projected field of one operation. The row order is the
// declaration order of Operation, so all surfaces agree on order too.
func (operation Operation) Facts() []OperationFact {
	return []OperationFact{
		{"id", operation.ID},
		{"summary", operation.Summary},
		{"usage", operation.Usage()},
		{"actor", string(operation.Actor)},
		{"effect", string(operation.Effect)},
		{"lifecycles", factLifecycles(operation)},
		{"requiresRoot", fmt.Sprint(operation.RequiresRoot)},
		{"requiresChange", fmt.Sprint(operation.RequiresChange)},
		{"requiresTask", fmt.Sprint(operation.RequiresTask)},
		{"authoritySource", operation.AuthoritySource},
		{"scopeSource", operation.ScopeSource},
		{"arguments", factArguments(operation)},
		{"flags", factFlags(operation)},
		{"exits", factExits(operation)},
		{"resultType", factOrNone(operation.ResultType)},
		{"agentVisible", fmt.Sprint(operation.AgentVisible)},
		{"executable", fmt.Sprint(operation.Executable)},
		{"example", operation.Example},
	}
}

const factNone = "none"

func factOrNone(value string) string {
	if value == "" {
		return factNone
	}
	return value
}

func factLifecycles(operation Operation) string {
	if len(operation.Lifecycles) == 0 {
		return "not lifecycle scoped"
	}
	names := make([]string, 0, len(operation.Lifecycles))
	for _, lifecycle := range operation.Lifecycles {
		names = append(names, string(lifecycle))
	}
	return strings.Join(names, ", ")
}

func factArguments(operation Operation) string {
	if len(operation.Arguments) == 0 {
		return factNone
	}
	parts := make([]string, 0, len(operation.Arguments))
	for _, argument := range operation.Arguments {
		text := "[" + argument.Name + "]"
		if argument.Required {
			text = "<" + argument.Name + ">"
		}
		parts = append(parts, text+" "+argument.Description)
	}
	return strings.Join(parts, "; ")
}

func factFlags(operation Operation) string {
	if len(operation.Flags) == 0 {
		return factNone
	}
	parts := make([]string, 0, len(operation.Flags))
	for _, flag := range operation.Flags {
		text := flag.Name + " (" + flag.Type
		if flag.Required {
			text += ", required"
		}
		if len(flag.Enum) > 0 {
			text += ", one of " + strings.Join(flag.Enum, "/")
		}
		if flag.Default != "" {
			text += ", default " + flag.Default
		}
		parts = append(parts, text+") "+flag.Description)
	}
	return strings.Join(parts, "; ")
}

func factExits(operation Operation) string {
	parts := make([]string, 0, len(operation.Exits))
	for _, exit := range operation.Exits {
		parts = append(parts, fmt.Sprintf("%d %s: %s", exit.Code, exit.Class, exit.Meaning))
	}
	return strings.Join(parts, "; ")
}

// RenderOperationHelp is the terminal help surface, built from the projection.
func RenderOperationHelp(facts OperationFacts) string {
	var out strings.Builder
	fmt.Fprintf(&out, "specd operations (schema %d)\n", facts.SchemaVersion)
	for _, operation := range facts.Operations {
		out.WriteString("\n")
		for _, fact := range operation.Facts() {
			fmt.Fprintf(&out, "  %-16s %s\n", fact.Field, fact.Value)
		}
	}
	return out.String()
}

// RenderOperationDocs is the documentation surface. docs/operations.md is
// generated from it, so no operation table is ever hand-written.
func RenderOperationDocs(facts OperationFacts) string {
	var out strings.Builder
	out.WriteString("# Operations\n\n")
	fmt.Fprintf(&out, "Generated from the operation registry (schema %d). Do not edit by hand:\n", facts.SchemaVersion)
	out.WriteString("run `go test ./internal/core -run TestOperationProjectionParity` to detect drift\n")
	out.WriteString("and `SPECD_WRITE_OPERATION_DOCS=1 go test ./internal/core -run TestOperationProjectionParity`\n")
	out.WriteString("to regenerate this file.\n\n")
	out.WriteString("Examples are display data. They are never parsed, expanded, or executed.\n")
	for _, operation := range facts.Operations {
		fmt.Fprintf(&out, "\n## %s\n\n| fact | value |\n|---|---|\n", operation.ID)
		for _, fact := range operation.Facts() {
			fmt.Fprintf(&out, "| %s | %s |\n", fact.Field, fact.Value)
		}
	}
	return out.String()
}

// WriteOperationDocs regenerates the documentation surface at path. It goes
// through the one atomic-replacement owner: a crash mid-regeneration leaves the
// previous generated file, never a torn one.
// Generated documentation is world-readable, unlike harness-owned state; the
// atomic owner writes 0600, so the published mode is restored after promotion.
func WriteOperationDocs(path string) error {
	if err := persist.AtomicReplace(path, []byte(RenderOperationDocs(ProjectOperations())), nil); err != nil {
		return err
	}
	return os.Chmod(path, 0o644)
}
